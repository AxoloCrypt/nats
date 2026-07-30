package ble

import (
	"context"
	"errors"
	"testing"
	"time"
)

type fakeScanner struct {
	probeOK        bool
	probeReason    string
	advertisements []Advertisement
	scanErr        error
}

func (f *fakeScanner) Probe() (bool, string) {
	return f.probeOK, f.probeReason
}

func (f *fakeScanner) Scan(ctx context.Context, window time.Duration) (<-chan Advertisement, error) {
	if f.scanErr != nil {
		return nil, f.scanErr
	}
	ch := make(chan Advertisement, len(f.advertisements))
	for _, a := range f.advertisements {
		ch <- a
	}
	close(ch)
	return ch, nil
}

func TestRun_ProbeFailPath(t *testing.T) {
	orig := scanner
	defer func() { scanner = orig }()
	RegisterScanner(&fakeScanner{probeOK: false, probeReason: "bluetooth permission denied"})

	events, err := Run(context.Background(), Options{Window: time.Second})
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	var got []Event
	for evt := range events {
		got = append(got, evt)
	}

	if len(got) != 1 {
		t.Fatalf("expected exactly one event (Done), got %d: %+v", len(got), got)
	}
	if got[0].Kind != EventKindDone {
		t.Fatalf("expected Done, got %v", got[0].Kind)
	}
	if len(got[0].Diagnostics) != 1 || got[0].Diagnostics[0].Severity != "warning" {
		t.Fatalf("expected one warning diagnostic, got %+v", got[0].Diagnostics)
	}
	if got[0].Diagnostics[0].Reason != "bluetooth permission denied" {
		t.Fatalf("expected probe reason to propagate, got %q", got[0].Diagnostics[0].Reason)
	}
}

func TestRun_ScanErrorPath(t *testing.T) {
	orig := scanner
	defer func() { scanner = orig }()
	RegisterScanner(&fakeScanner{probeOK: true, scanErr: errors.New("adapter reset mid-scan")})

	events, err := Run(context.Background(), Options{Window: time.Second})
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	var got []Event
	for evt := range events {
		got = append(got, evt)
	}

	if len(got) != 1 {
		t.Fatalf("expected exactly one event (Done), got %d: %+v", len(got), got)
	}
	if got[0].Kind != EventKindDone {
		t.Fatalf("expected Done, got %v", got[0].Kind)
	}
	if len(got[0].Diagnostics) != 1 || got[0].Diagnostics[0].Severity != "warning" {
		t.Fatalf("expected one warning diagnostic, got %+v", got[0].Diagnostics)
	}
	if got[0].Diagnostics[0].Reason != "adapter reset mid-scan" {
		t.Fatalf("expected scan error to propagate as the diagnostic reason, got %q", got[0].Diagnostics[0].Reason)
	}
	if len(got[0].Report.Devices) != 0 {
		t.Fatalf("expected an empty Report on the scan-error path, got %+v", got[0].Report)
	}
}

func TestRun_ProbeOKPath(t *testing.T) {
	orig := scanner
	defer func() { scanner = orig }()
	RegisterScanner(&fakeScanner{
		probeOK: true,
		advertisements: []Advertisement{
			{Address: "aa:bb:cc:dd:ee:ff", RSSI: -50},
			{Address: "11:22:33:44:55:66", RSSI: -70},
		},
	})

	events, err := Run(context.Background(), Options{Window: time.Second})
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	var got []Event
	for evt := range events {
		got = append(got, evt)
	}

	// Two DeviceFound events (one per Advertisement) followed by exactly one
	// Done, with the channel closing immediately after — no event follows
	// Done (mirrors base AD-3's invariant).
	if len(got) != 3 {
		t.Fatalf("expected 2 DeviceFound events + 1 Done, got %d: %+v", len(got), got)
	}
	for i := 0; i < 2; i++ {
		if got[i].Kind != EventKindDeviceFound {
			t.Fatalf("expected event %d to be DeviceFound, got %v", i, got[i].Kind)
		}
	}
	last := got[len(got)-1]
	if last.Kind != EventKindDone {
		t.Fatalf("expected the last event to be Done, got %v", last.Kind)
	}
	if len(last.Diagnostics) != 0 {
		t.Fatalf("expected no diagnostics on the probe-ok path, got %+v", last.Diagnostics)
	}

	// events channel must already be closed with no further sends after Done.
	if _, ok := <-events; ok {
		t.Fatal("expected channel to be closed with no event following Done")
	}
}

func TestRun_CompilesBLEDeviceProfilesInObservedOrder(t *testing.T) {
	orig := scanner
	defer func() { scanner = orig }()

	appleID := uint16(76)   // Apple, Inc.
	googleID := uint16(224) // Google
	RegisterScanner(&fakeScanner{
		probeOK: true,
		advertisements: []Advertisement{
			{Address: "aa:bb:cc:dd:ee:ff", Name: "Pixel Buds", CompanyID: &googleID},
			{Address: "11:22:33:44:55:66", Name: "AirTag", CompanyID: &appleID},
			// No Name broadcast and no CompanyID — must compile to an empty
			// Name (never "unknown"; that substitution is a Writer's job,
			// Story 4.7) and Vendor "unknown".
			{Address: "77:88:99:00:11:22"},
		},
	})

	events, err := Run(context.Background(), Options{Window: time.Second})
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	var report Report
	var deviceFoundOrder []string
	for evt := range events {
		if evt.Kind == EventKindDeviceFound {
			deviceFoundOrder = append(deviceFoundOrder, evt.Device.Address)
		}
		if evt.Kind == EventKindDone {
			report = evt.Report
		}
	}

	wantOrder := []string{"aa:bb:cc:dd:ee:ff", "11:22:33:44:55:66", "77:88:99:00:11:22"}
	if len(deviceFoundOrder) != len(wantOrder) {
		t.Fatalf("expected %d DeviceFound events, got %d: %v", len(wantOrder), len(deviceFoundOrder), deviceFoundOrder)
	}
	for i := range wantOrder {
		if deviceFoundOrder[i] != wantOrder[i] {
			t.Fatalf("expected DeviceFound order %v, got %v", wantOrder, deviceFoundOrder)
		}
	}

	want := []BLEDeviceProfile{
		{Address: "aa:bb:cc:dd:ee:ff", Name: "Pixel Buds", Vendor: "Google"},
		{Address: "11:22:33:44:55:66", Name: "AirTag", Vendor: "Apple, Inc."},
		{Address: "77:88:99:00:11:22", Name: "", Vendor: "unknown"},
	}
	if len(report.Devices) != len(want) {
		t.Fatalf("expected %d devices in the final Report, got %d: %+v", len(want), len(report.Devices), report.Devices)
	}
	for i := range want {
		if report.Devices[i] != want[i] {
			t.Fatalf("device %d: expected %+v, got %+v", i, want[i], report.Devices[i])
		}
	}
}
