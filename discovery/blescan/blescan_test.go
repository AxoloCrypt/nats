package blescan

import (
	"context"
	"errors"
	"testing"
	"time"

	"nats/core/ble"

	"tinygo.org/x/bluetooth"
)

// fakePayload is a minimal bluetooth.AdvertisementPayload — ScanResult
// embeds this interface and a zero-value (nil) payload would panic when
// toAdvertisement calls its methods, so tests need a real implementation.
type fakePayload struct{}

func (fakePayload) LocalName() string                                     { return "" }
func (fakePayload) HasServiceUUID(bluetooth.UUID) bool                    { return false }
func (fakePayload) ServiceUUIDs() []bluetooth.UUID                        { return nil }
func (fakePayload) Bytes() []byte                                         { return nil }
func (fakePayload) ManufacturerData() []bluetooth.ManufacturerDataElement { return nil }
func (fakePayload) ServiceData() []bluetooth.ServiceDataElement           { return nil }

// multiPayload carries several manufacturer-data elements, in an order that
// puts the lowest CompanyID last so a mfg[0] implementation fails this test.
type multiPayload struct{ fakePayload }

func (multiPayload) ManufacturerData() []bluetooth.ManufacturerDataElement {
	return []bluetooth.ManufacturerDataElement{
		{CompanyID: 0x004C, Data: []byte{0xAA}},
		{CompanyID: 0x00E0, Data: []byte{0xBB}},
		{CompanyID: 0x0006, Data: []byte{0xCC}},
	}
}

// The real slice comes from ranging a Go map upstream, so its order is
// randomized per callback; toAdvertisement must resolve the same CompanyID
// regardless of which permutation it is handed.
func TestToAdvertisement_PicksLowestCompanyIDDeterministically(t *testing.T) {
	result := bluetooth.ScanResult{AdvertisementPayload: multiPayload{}}

	for i := 0; i < 50; i++ {
		adv := toAdvertisement(result)
		if adv.CompanyID == nil {
			t.Fatal("expected a CompanyID to be resolved")
		}
		if *adv.CompanyID != 0x0006 {
			t.Fatalf("expected the lowest CompanyID 0x0006, got 0x%04X", *adv.CompanyID)
		}
		if len(adv.ManufacturerData) != 1 || adv.ManufacturerData[0] != 0xCC {
			t.Fatalf("ManufacturerData must come from the same element as CompanyID, got %v", adv.ManufacturerData)
		}
	}
}

func TestProbe_Ok(t *testing.T) {
	orig := enableAdapter
	defer func() { enableAdapter = orig }()
	enableAdapter = func() error { return nil }

	ok, reason := (&scanner{}).Probe()
	if !ok {
		t.Fatalf("expected ok=true, got reason %q", reason)
	}
	if reason != "" {
		t.Fatalf("expected empty reason on success, got %q", reason)
	}
}

func TestProbe_Fail(t *testing.T) {
	orig := enableAdapter
	defer func() { enableAdapter = orig }()
	enableAdapter = func() error { return errors.New("bluetooth permission denied") }

	ok, reason := (&scanner{}).Probe()
	if ok {
		t.Fatal("expected ok=false")
	}
	if reason != "bluetooth permission denied" {
		t.Fatalf("expected the underlying error as reason, got %q", reason)
	}
}

func TestScan_DrainsAdvertisementsAndClosesAfterWindow(t *testing.T) {
	origScan := scanAdapter
	origStop := stopScanAdapter
	defer func() {
		scanAdapter = origScan
		stopScanAdapter = origStop
	}()

	stopped := make(chan struct{}, 1)
	scanAdapter = func(callback func(bluetooth.ScanResult)) error {
		callback(bluetooth.ScanResult{RSSI: -42, AdvertisementPayload: fakePayload{}})
		<-stopped
		return nil
	}
	stopScanAdapter = func() error {
		stopped <- struct{}{}
		return nil
	}

	s := &scanner{}
	ch, err := s.Scan(context.Background(), 10*time.Millisecond)
	if err != nil {
		t.Fatalf("Scan failed: %v", err)
	}

	var got []int
	for adv := range ch {
		got = append(got, adv.RSSI)
	}

	if len(got) != 1 || got[0] != -42 {
		t.Fatalf("expected one advertisement with RSSI -42, got %v", got)
	}
}

// TestScan_ReportsAStartFailureInsteadOfAnEmptyScan covers the gap Probe()
// structurally cannot: with tinygo v0.15.0, "Bluetooth is powered off" and
// "scanning denied by policy" are reported by Adapter.Scan, not by Enable().
// The error used to be discarded into scanDone, so the user got a
// zero-device "BLE scan complete." that looked exactly like an empty room.
// Scan must hand it back so core/ble.Run can name it in a warning
// (NL-FR-13: a skipped scan is never silently dropped from output).
func TestScan_ReportsAStartFailureInsteadOfAnEmptyScan(t *testing.T) {
	origScan := scanAdapter
	origStop := stopScanAdapter
	defer func() {
		scanAdapter = origScan
		stopScanAdapter = origStop
	}()

	// Both real refusals return without ever invoking the callback.
	for _, reason := range []string{
		"bluetooth: adapter is not powered on",
		"org.bluez.Error.NotAuthorized: permission denied",
	} {
		t.Run(reason, func(t *testing.T) {
			scanAdapter = func(callback func(bluetooth.ScanResult)) error {
				return errors.New(reason)
			}
			stopScanAdapter = func() error {
				t.Error("stopScanAdapter must not be called for a scan that never started")
				return nil
			}

			ch, err := (&scanner{}).Scan(context.Background(), time.Minute)
			if err == nil {
				t.Fatal("expected the scan-start failure to be returned, got nil — it was swallowed")
			}
			if err.Error() != reason {
				t.Fatalf("expected the adapter's own reason %q verbatim, got %q", reason, err.Error())
			}
			if ch != nil {
				t.Fatalf("expected a nil channel alongside the error, got %v", ch)
			}
		})
	}
}

// TestScan_StartFailureReachesTheUserAsAWarning is the composition proof: the
// error surfaced above has to travel through core/ble.Run's skip path and
// come out as a warning Diagnostic naming what was skipped and why, rather
// than as a bare empty report.
func TestScan_StartFailureReachesTheUserAsAWarning(t *testing.T) {
	origScan := scanAdapter
	origEnable := enableAdapter
	defer func() {
		scanAdapter = origScan
		enableAdapter = origEnable
	}()

	enableAdapter = func() error { return nil }
	scanAdapter = func(callback func(bluetooth.ScanResult)) error {
		return errors.New("bluetooth: adapter is not powered on")
	}

	origScanner, _ := ble.GetScanner()
	t.Cleanup(func() { ble.RegisterScanner(origScanner) })
	ble.RegisterScanner(&scanner{})

	events, err := ble.Run(context.Background(), ble.Options{Window: time.Minute})
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	var got []ble.Event
	for evt := range events {
		got = append(got, evt)
	}

	if len(got) != 1 || got[0].Kind != ble.EventKindDone {
		t.Fatalf("expected exactly one Done event, got %+v", got)
	}
	if len(got[0].Diagnostics) != 1 {
		t.Fatalf("expected exactly one Diagnostic, got %+v", got[0].Diagnostics)
	}
	diag := got[0].Diagnostics[0]
	if diag.Severity != "warning" {
		t.Fatalf("expected severity %q, got %q", "warning", diag.Severity)
	}
	if diag.Reason != "bluetooth: adapter is not powered on" {
		t.Fatalf("expected the adapter's own reason to reach the user, got %q", diag.Reason)
	}
}

func TestScan_StopsOnContextCancel(t *testing.T) {
	origScan := scanAdapter
	origStop := stopScanAdapter
	defer func() {
		scanAdapter = origScan
		stopScanAdapter = origStop
	}()

	stopped := make(chan struct{}, 1)
	scanAdapter = func(callback func(bluetooth.ScanResult)) error {
		<-stopped
		return nil
	}
	stopScanAdapter = func() error {
		stopped <- struct{}{}
		return nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	s := &scanner{}
	ch, err := s.Scan(ctx, time.Minute)
	if err != nil {
		t.Fatalf("Scan failed: %v", err)
	}
	cancel()

	select {
	case _, ok := <-ch:
		if ok {
			t.Fatal("expected no advertisements")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("expected channel to close after context cancellation")
	}
}
