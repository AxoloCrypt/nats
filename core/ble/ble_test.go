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
// the command must complete cleanly rather than crash or hang on a degraded
// path, so a Run that leaks its goroutine and never
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
// the rule: no second, generic diagnostic may be stacked on top of the one
// that already explains the empty result.
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
// core/engine's "no devices discovered" error diagnostic.
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
	// Done (mirrors the LAN vertical's event-stream invariant).
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
			// Name (never "unknown"; that substitution is a Writer's job)
			// and Vendor "unknown".
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
	// DistanceEstimate's exact numbers aren't pinned by any specification —
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

// TestRun_DedupesRepeatedAdvertisementsByAddress proves core/ble.Run merges
// repeated advertisements from the same physical device (same Address)
// into a single Report.Devices row instead of one row per raw packet.
// Exercises all three merge rules in one scan: DistanceEstimate always takes
// the latest reading; Name/Vendor/DeviceType resolve from the first packet
// that carries the signal and are never blanked back by a later,
// less-informative one; distinct Addresses are unaffected.
func TestRun_DedupesRepeatedAdvertisementsByAddress(t *testing.T) {
	orig := scanner
	defer func() { scanner = orig }()

	googleID := uint16(224)
	appleID := uint16(76)

	// aa: three sightings of the same device, RSSI improving each time —
	// proves DistanceEstimate always reflects the latest packet, not the
	// first.
	aaFirst := Advertisement{Address: "aa:11:00:00:00:01", Name: "Gadget", RSSI: -80, CompanyID: &googleID}
	aaLast := Advertisement{Address: "aa:11:00:00:00:01", Name: "Gadget", RSSI: -60, CompanyID: &googleID}

	// bb: identity signal absent on first sighting, resolves on the second,
	// then the third sighting reverts to carrying no signal at all — proves
	// Name/Vendor/DeviceType lock in once resolved and never regress.
	bbUnresolved := Advertisement{Address: "bb:22:00:00:00:01", RSSI: -90}
	bbResolved := Advertisement{Address: "bb:22:00:00:00:01", Name: "Beacon Widget", RSSI: -85, CompanyID: &appleID}

	RegisterScanner(&fakeScanner{
		probeOK: true,
		advertisements: []Advertisement{
			{Address: "cc:33:00:00:00:01", Name: "Distinct1", RSSI: -65, CompanyID: &appleID}, // distinct, single sighting
			aaFirst,
			bbUnresolved,
			{Address: "dd:44:00:00:00:01", Name: "Distinct2", RSSI: -70},                    // distinct, single sighting, interleaved
			{Address: "aa:11:00:00:00:01", Name: "Gadget", RSSI: -70, CompanyID: &googleID}, // aa: middle sighting
			bbResolved,
			aaLast,
			{Address: "bb:22:00:00:00:01", RSSI: -88}, // bb: reverts to no identity signal
		},
	})

	got := drainToDone(t, Options{Window: time.Second})

	var foundOrder []string
	var report Report
	for _, evt := range got {
		if evt.Kind == EventKindDeviceFound {
			foundOrder = append(foundOrder, evt.Device.Address)
		}
		if evt.Kind == EventKindDone {
			report = evt.Report
		}
	}

	wantOrder := []string{"cc:33:00:00:00:01", "aa:11:00:00:00:01", "bb:22:00:00:00:01", "dd:44:00:00:00:01"}
	if len(foundOrder) != len(wantOrder) {
		t.Fatalf("expected exactly %d DeviceFound events (one per unique Address, never one per packet), got %d: %v", len(wantOrder), len(foundOrder), foundOrder)
	}
	for i := range wantOrder {
		if foundOrder[i] != wantOrder[i] {
			t.Fatalf("expected DeviceFound order %v, got %v", wantOrder, foundOrder)
		}
	}

	if len(report.Devices) != len(wantOrder) {
		t.Fatalf("expected %d deduped rows in Report.Devices (first-sighting order), got %d: %+v", len(wantOrder), len(report.Devices), report.Devices)
	}
	for i, addr := range wantOrder {
		if report.Devices[i].Address != addr {
			t.Fatalf("expected Report.Devices[%d].Address == %q (first-sighting order preserved), got %q", i, addr, report.Devices[i].Address)
		}
	}

	byAddr := make(map[string]BLEDeviceProfile, len(report.Devices))
	for _, d := range report.Devices {
		byAddr[d.Address] = d
	}

	// aa: DistanceEstimate must reflect the last (strongest, -60) reading,
	// not the first (-80) one.
	aa := byAddr["aa:11:00:00:00:01"]
	wantAADistance := FormatDistance(EstimateDistance(aaLast.RSSI, aaLast.TXPower))
	staleAADistance := FormatDistance(EstimateDistance(aaFirst.RSSI, aaFirst.TXPower))
	if aa.DistanceEstimate != wantAADistance {
		t.Fatalf("aa: expected DistanceEstimate from the latest (3rd) packet %q, got %q", wantAADistance, aa.DistanceEstimate)
	}
	if aa.DistanceEstimate == staleAADistance {
		t.Fatalf("aa: DistanceEstimate matches the first packet's reading — it should have been overwritten by the latest one")
	}

	// bb: Name/Vendor/DeviceType must reflect the one packet (the 2nd) that
	// carried the signal, and must not have been blanked back to their
	// placeholder by the 1st (before) or 3rd (after) packet, which carried
	// none. DistanceEstimate must still reflect the 3rd (latest) packet's
	// RSSI even though that same packet carried no identity signal — proves
	// the two merge rules apply independently on the same row.
	bb := byAddr["bb:22:00:00:00:01"]
	if bb.Name != "Beacon Widget" {
		t.Fatalf("bb: expected Name %q to survive the later no-signal packet, got %q", "Beacon Widget", bb.Name)
	}
	if bb.Vendor != "Apple, Inc." {
		t.Fatalf("bb: expected Vendor %q to survive the later no-signal packet, got %q", "Apple, Inc.", bb.Vendor)
	}
	if bb.DeviceType != DeviceTypeSensorTag {
		t.Fatalf("bb: expected DeviceType %q (from the \"beacon\" keyword) to survive the later no-signal packet, got %q", DeviceTypeSensorTag, bb.DeviceType)
	}
	wantBBDistance := FormatDistance(EstimateDistance(-88, nil)) // the 3rd, latest packet
	if bb.DistanceEstimate != wantBBDistance {
		t.Fatalf("bb: expected DistanceEstimate from the latest (3rd) packet %q, got %q", wantBBDistance, bb.DistanceEstimate)
	}

	// cc/dd: distinct Addresses seen once each — every field, not just
	// Name/Vendor, must be untouched by dedup logic.
	cc := byAddr["cc:33:00:00:00:01"]
	wantCCDistance := FormatDistance(EstimateDistance(-65, nil))
	if cc.Name != "Distinct1" || cc.Vendor != "Apple, Inc." || cc.DeviceType != DeviceTypeUnknown || cc.DistanceEstimate != wantCCDistance {
		t.Fatalf("cc: expected an untouched single-sighting row, got %+v", cc)
	}
	dd := byAddr["dd:44:00:00:00:01"]
	wantDDDistance := FormatDistance(EstimateDistance(-70, nil))
	if dd.Name != "Distinct2" || dd.Vendor != "unknown" || dd.DeviceType != DeviceTypeUnknown || dd.DistanceEstimate != wantDDDistance {
		t.Fatalf("dd: expected an untouched single-sighting row, got %+v", dd)
	}
}

// TestRun_DedupeIsCaseSensitiveOnAddress pins spec-ble-advertisement-dedup.md's
// "Always: ... no normalization/case-folding" boundary as a checked
// invariant: two Addresses differing only in case must be treated as two
// distinct devices, never merged. Guards against a well-intentioned future
// "fix" (e.g. adding strings.ToLower for robustness) silently violating
// that boundary.
func TestRun_DedupeIsCaseSensitiveOnAddress(t *testing.T) {
	orig := scanner
	defer func() { scanner = orig }()

	RegisterScanner(&fakeScanner{
		probeOK: true,
		advertisements: []Advertisement{
			{Address: "AA:BB:CC:DD:EE:FF", RSSI: -60},
			{Address: "aa:bb:cc:dd:ee:ff", RSSI: -70},
		},
	})

	got := drainToDone(t, Options{Window: time.Second})

	var report Report
	var foundCount int
	for _, evt := range got {
		if evt.Kind == EventKindDeviceFound {
			foundCount++
		}
		if evt.Kind == EventKindDone {
			report = evt.Report
		}
	}

	if foundCount != 2 {
		t.Fatalf("expected 2 DeviceFound events (case-differing Addresses are distinct devices, never merged), got %d", foundCount)
	}
	if len(report.Devices) != 2 {
		t.Fatalf("expected 2 rows in Report.Devices, got %d: %+v", len(report.Devices), report.Devices)
	}
}

// controlledScanner is a BLEScanner whose Scan returns a channel the test
// sends into directly, letting a test synchronize advertisement delivery
// with context cancellation deterministically instead of racing on it.
type controlledScanner struct {
	ch chan Advertisement
}

func (c *controlledScanner) Probe() (bool, string) { return true, "" }

func (c *controlledScanner) Scan(ctx context.Context, window time.Duration) (<-chan Advertisement, error) {
	return c.ch, nil
}

// TestRun_MergePathStillObservesCtxCancellation guards the merge branch's
// ctx.Done() check: a repeat sighting of a known Address (the exact
// re-broadcast pattern the dedup merge exists to handle) must not delay
// observing cancellation until the next new Address or advCh's own close.
// Uses an unbuffered, test-controlled channel to make the ordering between
// "ctx canceled" and "repeat advertisement delivered" deterministic — a
// wall-clock race (e.g. cancel() then hope the goroutine hasn't already
// finished a burst) would be flaky in either direction.
func TestRun_MergePathStillObservesCtxCancellation(t *testing.T) {
	orig := scanner
	defer func() { scanner = orig }()

	advCh := make(chan Advertisement)
	RegisterScanner(&controlledScanner{ch: advCh})

	ctx, cancel := context.WithCancel(context.Background())
	events, err := Run(ctx, Options{Window: time.Second})
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	send := func(adv Advertisement) {
		t.Helper()
		select {
		case advCh <- adv:
		case <-time.After(2 * time.Second):
			t.Fatal("timed out sending advertisement — Run's drain loop is not reading advCh")
		}
	}

	// First sighting of "aa": send it and drain the resulting DeviceFound —
	// this deterministically proves Run's goroutine is back at the top of
	// the drain loop, blocked on advCh, before cancellation.
	send(Advertisement{Address: "aa:bb:cc:dd:ee:ff", RSSI: -60})
	select {
	case evt := <-events:
		if evt.Kind != EventKindDeviceFound {
			t.Fatalf("expected the first event to be DeviceFound, got %v", evt.Kind)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for the first DeviceFound event")
	}

	cancel()

	// Second sighting of the same Address: a repeat, so it goes through the
	// merge branch. If that branch doesn't check ctx.Done(), Run keeps
	// draining forever waiting for advCh to close — the exact hang the fix
	// guards against.
	send(Advertisement{Address: "aa:bb:cc:dd:ee:ff", RSSI: -70})

	select {
	case _, ok := <-events:
		if ok {
			t.Fatal("expected the channel to close (ctx canceled mid-merge), got an event instead")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run's event channel never closed — the merge path is not observing ctx cancellation")
	}
}

// TestRun_TwoConsecutiveCallsAreIndependent is the concrete, automatable
// proof that two runs produce two fully independent result sets:
// back-to-back Run() calls, with the registered BLEScanner swapped between
// them, must never let the second call's Report see a device from the
// first. This is the regression guard against someone later adding a
// well-intentioned in-memory cache/accumulator to core/ble.
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

// TestRun_NeverWritesAnyFileToDisk guards the vertical's "no on-disk cache,
// database, or history file" rule: a full Run() call must leave every
// location an accidental cache would plausibly land in exactly as empty as
// it found it. The working directory alone isn't enough — a well-intentioned
// cache is far more likely to reach for os.UserCacheDir() or $HOME — so HOME
// and XDG_CACHE_HOME are redirected at temp dirs and checked too. The
// explicit, user-requested --output-file write is additive to stdout and
// opt-in, driven from cmd/cli rather than from core/ble.Run, so it isn't
// what this test is guarding against.
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
// proof for the warning's "why" half: the Reason is whatever the adapter
// actually diagnosed, passed straight through. Table-driven across several
// unrelated reason strings so an implementation that substitutes one fixed,
// generic sentence — discarding the adapter's real diagnosis — can't pass by
// happening to match the single string a one-case test asserted.
func TestRun_ProbeFail_SurfacesEachScannerReasonVerbatim(t *testing.T) {
	reasons := []string{
		"Bluetooth permission denied",
		"Bluetooth adapter unavailable",
		// A reason discovery/blescan actually produced on a real dev host,
		// verbatim — a long, platform-specific string.
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
			// The "what" half of the warning: the message has to name the scan
			// that was skipped, not just report that something went wrong.
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
// a bare "warning: BLE scan skipped" — naming what was skipped but never
// why, when both halves are required. The fallback is only ever reached when
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

// TestRun_ProbeOKWithZeroDevices_IsNotAnError pins a deliberate divergence
// from the LAN vertical. core/engine.Run appends an error-severity "no
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
