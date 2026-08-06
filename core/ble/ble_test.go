package ble

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
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

// drainToDone runs Run and collects every event it emits, failing the test if
// the channel doesn't close within a bounded time. The timeout is the point:
// Story 4.6 AC #1 requires the command to "complete cleanly rather than crash
// or hang" on a degraded path, so a Run that leaks its goroutine and never
// closes the channel has to fail the test rather than block the suite until
// Go's package-level 10m panic.
func drainToDone(t *testing.T, opts Options) []Event {
	t.Helper()

	events, err := Run(context.Background(), opts)
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	var got []Event
	drained := make(chan struct{})
	go func() {
		defer close(drained)
		for evt := range events {
			got = append(got, evt)
		}
	}()

	select {
	case <-drained:
	case <-time.After(5 * time.Second):
		t.Fatal("Run's event channel never closed — the scan hung instead of completing")
	}
	return got
}

// assertDegradedDone asserts the shape every skipped-scan path must produce:
// exactly one event, a Done, carrying exactly one warning Diagnostic and an
// empty Report. "Exactly one Diagnostic" is the machine-checkable form of
// Task 2's rule — no second, generic diagnostic may be stacked on top of the
// one that already explains the empty result.
func assertDegradedDone(t *testing.T, got []Event) Diagnostic {
	t.Helper()

	if len(got) != 1 {
		t.Fatalf("expected exactly one event (Done), got %d: %+v", len(got), got)
	}
	if got[0].Kind != EventKindDone {
		t.Fatalf("expected Done, got %v", got[0].Kind)
	}
	if len(got[0].Diagnostics) != 1 {
		t.Fatalf("expected exactly one Diagnostic (no redundant second one stacked on it), got %+v", got[0].Diagnostics)
	}
	if got[0].Diagnostics[0].Severity != "warning" {
		t.Fatalf("expected severity %q — a permission gap is degraded-but-completed, never an error — got %q", "warning", got[0].Diagnostics[0].Severity)
	}
	if len(got[0].Report.Devices) != 0 {
		t.Fatalf("expected an empty Report on a skipped scan, got %+v", got[0].Report)
	}
	return got[0].Diagnostics[0]
}

// assertNoErrorSeverity is the direct guard against core/ble ever growing
// core/engine's "no devices discovered" error diagnostic (Task 2).
func assertNoErrorSeverity(t *testing.T, diags []Diagnostic) {
	t.Helper()

	for _, d := range diags {
		if d.Severity == "error" {
			t.Fatalf("core/ble must never emit an error-severity Diagnostic for an empty result set, got: %+v", d)
		}
	}
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
	txPower := -55
	RegisterScanner(&fakeScanner{
		probeOK: true,
		advertisements: []Advertisement{
			// TX Power present.
			{Address: "aa:bb:cc:dd:ee:ff", Name: "Pixel Buds", RSSI: -60, TXPower: &txPower, CompanyID: &googleID},
			// TX Power absent — falls back to EstimateDistance's assumed default.
			{Address: "11:22:33:44:55:66", Name: "AirTag", RSSI: -70, CompanyID: &appleID},
			// No Name broadcast and no CompanyID — must compile to an empty
			// Name (never "unknown"; that substitution is a Writer's job,
			// Story 4.7) and Vendor "unknown".
			{Address: "77:88:99:00:11:22", RSSI: -80},
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

	want := []struct {
		Address, Name, Vendor, DeviceType string
	}{
		// "Pixel Buds" has no Appearance/ServiceUUIDs, so classification
		// falls back to the Name keyword signal ("buds" -> audio device).
		{Address: "aa:bb:cc:dd:ee:ff", Name: "Pixel Buds", Vendor: "Google", DeviceType: DeviceTypeAudioDevice},
		// "AirTag" likewise falls back to the Name keyword signal ("tag" ->
		// sensor/tag).
		{Address: "11:22:33:44:55:66", Name: "AirTag", Vendor: "Apple, Inc.", DeviceType: DeviceTypeSensorTag},
		// No Name/Appearance/ServiceUUIDs at all -> no signal resolves.
		{Address: "77:88:99:00:11:22", Name: "", Vendor: "unknown", DeviceType: DeviceTypeUnknown},
	}
	if len(report.Devices) != len(want) {
		t.Fatalf("expected %d devices in the final Report, got %d: %+v", len(want), len(report.Devices), report.Devices)
	}
	// DistanceEstimate's exact numbers aren't pinned by spec (Story 4.3) —
	// assert its shape structurally rather than against a hardcoded string.
	// distancePattern is the package-level regex declared in distance_test.go.
	for i := range want {
		got := report.Devices[i]
		if got.Address != want[i].Address || got.Name != want[i].Name || got.Vendor != want[i].Vendor {
			t.Fatalf("device %d: expected Address/Name/Vendor %+v, got %+v", i, want[i], got)
		}
		if got.DeviceType != want[i].DeviceType {
			t.Fatalf("device %d: expected DeviceType %q, got %q", i, want[i].DeviceType, got.DeviceType)
		}
		if !distancePattern.MatchString(got.DistanceEstimate) {
			t.Fatalf("device %d: expected a well-formed DistanceEstimate, got %q", i, got.DistanceEstimate)
		}
	}
}

// TestRun_TwoConsecutiveCallsAreIndependent is the concrete, automatable
// proof of AC #1's "two fully independent result sets" requirement
// (NL-AD-12): back-to-back Run() calls, with the registered BLEScanner
// swapped between them, must never let the second call's Report see a
// device from the first. This is the regression guard against someone
// later adding a well-intentioned in-memory cache/accumulator to core/ble.
func TestRun_TwoConsecutiveCallsAreIndependent(t *testing.T) {
	orig := scanner
	defer func() { scanner = orig }()

	RegisterScanner(&fakeScanner{
		probeOK: true,
		advertisements: []Advertisement{
			{Address: "aa:aa:aa:aa:aa:aa", RSSI: -50},
		},
	})
	firstEvents, err := Run(context.Background(), Options{Window: time.Second})
	if err != nil {
		t.Fatalf("first Run failed: %v", err)
	}
	var firstReport Report
	for evt := range firstEvents {
		if evt.Kind == EventKindDone {
			firstReport = evt.Report
		}
	}
	if len(firstReport.Devices) != 1 || firstReport.Devices[0].Address != "aa:aa:aa:aa:aa:aa" {
		t.Fatalf("expected first call's report to contain only aa:aa:aa:aa:aa:aa, got %+v", firstReport.Devices)
	}

	// Swap in a scanner with a completely different device set and confirm
	// the second call's Report reflects only the second call's data — never
	// a union with the first call's devices.
	RegisterScanner(&fakeScanner{
		probeOK: true,
		advertisements: []Advertisement{
			{Address: "bb:bb:bb:bb:bb:bb", RSSI: -60},
			{Address: "cc:cc:cc:cc:cc:cc", RSSI: -70},
		},
	})
	secondEvents, err := Run(context.Background(), Options{Window: time.Second})
	if err != nil {
		t.Fatalf("second Run failed: %v", err)
	}
	var secondReport Report
	for evt := range secondEvents {
		if evt.Kind == EventKindDone {
			secondReport = evt.Report
		}
	}

	if len(secondReport.Devices) != 2 {
		t.Fatalf("expected the second call's report to contain exactly its own 2 devices, got %d: %+v", len(secondReport.Devices), secondReport.Devices)
	}
	for _, d := range secondReport.Devices {
		if d.Address == "aa:aa:aa:aa:aa:aa" {
			t.Fatalf("second call's report leaked a device from the first call: %+v", secondReport.Devices)
		}
	}
	wantAddrs := map[string]bool{"bb:bb:bb:bb:bb:bb": true, "cc:cc:cc:cc:cc:cc": true}
	for _, d := range secondReport.Devices {
		if !wantAddrs[d.Address] {
			t.Fatalf("unexpected device address in second call's report: %q", d.Address)
		}
	}
}

// TestRun_NeverWritesAnyFileToDisk guards NL-AD-12's "no on-disk cache,
// database, or history file" clause of AC #1: a full Run() call must leave
// every location an accidental cache would plausibly land in exactly as
// empty as it found it. The working directory alone isn't enough — a
// well-intentioned cache is far more likely to reach for os.UserCacheDir()
// or $HOME — so HOME and XDG_CACHE_HOME are redirected at temp dirs and
// checked too. Story 4.7 is what eventually adds an explicit,
// user-requested --output-file write — additive to stdout, opt-in — which
// doesn't exist yet at this point in the epic and isn't what this test is
// guarding against.
func TestRun_NeverWritesAnyFileToDisk(t *testing.T) {
	orig := scanner
	defer func() { scanner = orig }()
	RegisterScanner(&fakeScanner{
		probeOK: true,
		advertisements: []Advertisement{
			{Address: "aa:bb:cc:dd:ee:ff", RSSI: -50},
		},
	})

	// t.Chdir (Go 1.24+) restores the working directory automatically and
	// fails fast if this test is ever made parallel — process-global state
	// that a hand-rolled os.Chdir/defer pair silently gets wrong.
	workDir := t.TempDir()
	t.Chdir(workDir)

	homeDir := t.TempDir()
	cacheDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("XDG_CACHE_HOME", cacheDir)

	events, err := Run(context.Background(), Options{Window: time.Second})
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	for range events {
		// Drain to completion.
	}

	for _, dir := range []struct {
		label string
		path  string
	}{
		{"working directory", workDir},
		{"$HOME", homeDir},
		{"$XDG_CACHE_HOME", cacheDir},
	} {
		entries, err := os.ReadDir(dir.path)
		if err != nil {
			t.Fatalf("failed to read %s: %v", dir.label, err)
		}
		if len(entries) != 0 {
			names := make([]string, len(entries))
			for i, e := range entries {
				names[i] = filepath.Join(dir.path, e.Name())
			}
			t.Fatalf("expected no files created by Run() in %s, found: %v", dir.label, names)
		}
	}
}

// TestRun_ProbeFail_SurfacesEachScannerReasonVerbatim is the anti-hardcoding
// proof for AC #1's "why" half: the warning's Reason is whatever the adapter
// actually diagnosed, passed straight through. Table-driven across several
// unrelated reason strings so an implementation that substitutes one fixed,
// generic sentence — discarding the adapter's real diagnosis — can't pass by
// happening to match the single string a one-case test asserted.
func TestRun_ProbeFail_SurfacesEachScannerReasonVerbatim(t *testing.T) {
	reasons := []string{
		"Bluetooth permission denied",
		"Bluetooth adapter unavailable",
		// The reason discovery/blescan actually produced on the Story 4.1 dev
		// host, verbatim — a real, long, platform-specific string.
		"could not activate BlueZ adapter: The name org.bluez was not provided by any .service files",
	}

	for _, reason := range reasons {
		t.Run(reason, func(t *testing.T) {
			orig := scanner
			defer func() { scanner = orig }()
			RegisterScanner(&fakeScanner{probeOK: false, probeReason: reason})

			got := drainToDone(t, Options{Window: time.Second})

			diag := assertDegradedDone(t, got)
			if diag.Reason != reason {
				t.Fatalf("expected the scanner's own reason %q passed through verbatim, got %q", reason, diag.Reason)
			}
			// The "what" half of AC #1: the message has to name the scan that
			// was skipped, not just report that something went wrong.
			if !strings.Contains(diag.Message, "BLE scan") {
				t.Fatalf("expected the message to name what was skipped (the BLE scan), got %q", diag.Message)
			}
			assertNoErrorSeverity(t, got[0].Diagnostics)
		})
	}
}

// TestRun_ProbeFail_WithoutAReasonStillExplainsWhy covers the one genuine gap
// in the probe-fail path: BLEScanner.Probe's contract lets an implementation
// return ok=false with an empty reason, and cmd/cli's renderDiagnostic drops
// the "reason:" line entirely when Reason is "". That combination would print
// a bare "warning: BLE scan skipped" — naming what was skipped but never why,
// which AC #1 requires both halves of. The fallback is only ever reached when
// the scanner supplied nothing; a real reason is never replaced (the table
// test above is what pins that).
func TestRun_ProbeFail_WithoutAReasonStillExplainsWhy(t *testing.T) {
	orig := scanner
	defer func() { scanner = orig }()
	RegisterScanner(&fakeScanner{probeOK: false, probeReason: ""})

	got := drainToDone(t, Options{Window: time.Second})

	diag := assertDegradedDone(t, got)
	if strings.TrimSpace(diag.Reason) == "" {
		t.Fatal("expected a non-empty fallback Reason when the scanner reports no reason of its own, got an empty string")
	}
}

// TestRun_ProbeOKWithZeroDevices_IsNotAnError is the story's central
// judgement call (Task 2). core/engine.Run appends an error-severity "no
// devices discovered" Diagnostic when a scan finds nothing, because LAN
// scanning can reasonably assume at least a router is reachable. BLE cannot
// assume anything of the sort — standing somewhere with nothing broadcasting
// nearby is an ordinary, unremarkable outcome — so a completed BLE scan that
// observed zero advertisements must report no diagnostic at all. This test
// exists to fail loudly if someone later pattern-matches engine.go's block
// into core/ble.
func TestRun_ProbeOKWithZeroDevices_IsNotAnError(t *testing.T) {
	orig := scanner
	defer func() { scanner = orig }()
	RegisterScanner(&fakeScanner{probeOK: true, advertisements: nil})

	got := drainToDone(t, Options{Window: time.Second})

	if len(got) != 1 {
		t.Fatalf("expected exactly one event (Done) when nothing was observed, got %d: %+v", len(got), got)
	}
	if got[0].Kind != EventKindDone {
		t.Fatalf("expected Done, got %v", got[0].Kind)
	}
	assertNoErrorSeverity(t, got[0].Diagnostics)
	if len(got[0].Diagnostics) != 0 {
		t.Fatalf("a normally-completed scan that simply saw nothing nearby must report no diagnostic at all, got %+v", got[0].Diagnostics)
	}
	if len(got[0].Report.Devices) != 0 {
		t.Fatalf("expected an empty Report, got %+v", got[0].Report)
	}
}

// TestRun_NoScannerRegistered_DegradesTheSameWay pins the third skip path
// (no platform build registered a BLEScanner) to the identical shape as the
// probe-fail path, so the three skip paths can't drift into reporting the
// same class of condition at different severities.
func TestRun_NoScannerRegistered_DegradesTheSameWay(t *testing.T) {
	orig := scanner
	defer func() { scanner = orig }()
	scanner = nil

	got := drainToDone(t, Options{Window: time.Second})

	diag := assertDegradedDone(t, got)
	if strings.TrimSpace(diag.Reason) == "" {
		t.Fatal("expected a non-empty Reason explaining why no scan could run")
	}
	assertNoErrorSeverity(t, got[0].Diagnostics)
}
