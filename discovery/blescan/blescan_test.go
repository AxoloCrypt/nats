package blescan

import (
	"context"
	"errors"
	"testing"
	"time"

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
