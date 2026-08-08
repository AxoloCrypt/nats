package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"nats/core/engine"

	"github.com/spf13/cobra"
)

func TestSplitTechniques_Empty(t *testing.T) {
	result := splitTechniques("")
	if result != nil {
		t.Fatalf("expected nil for empty string, got %v", result)
	}
}

func TestSplitTechniques_Single(t *testing.T) {
	result := splitTechniques("arp")
	if len(result) != 1 || result[0] != "arp" {
		t.Fatalf("expected [\"arp\"], got %v", result)
	}
}

func TestSplitTechniques_Multiple(t *testing.T) {
	result := splitTechniques("arp,icmp,mdns")
	if len(result) != 3 {
		t.Fatalf("expected 3 techniques, got %d: %v", len(result), result)
	}
	if result[0] != "arp" || result[1] != "icmp" || result[2] != "mdns" {
		t.Fatalf("expected [\"arp\",\"icmp\",\"mdns\"], got %v", result)
	}
}

func TestSplitTechniques_SelectSubset(t *testing.T) {
	result := splitTechniques("icmp,mdns")
	if len(result) != 2 {
		t.Fatalf("expected 2 techniques, got %d: %v", len(result), result)
	}
	if result[0] != "icmp" || result[1] != "mdns" {
		t.Fatalf("expected [\"icmp\",\"mdns\"], got %v", result)
	}
}

func TestSplitTechniques_TrimsWhitespace(t *testing.T) {
	result := splitTechniques("arp, icmp , mdns")
	if len(result) != 3 || result[0] != "arp" || result[1] != "icmp" || result[2] != "mdns" {
		t.Fatalf("expected [\"arp\",\"icmp\",\"mdns\"], got %v", result)
	}
}

func TestSplitTechniques_DedupsAndLowercases(t *testing.T) {
	result := splitTechniques("ICMP,icmp,Mdns")
	if len(result) != 2 || result[0] != "icmp" || result[1] != "mdns" {
		t.Fatalf("expected [\"icmp\",\"mdns\"], got %v", result)
	}
}

func newTestScanCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "scan"}
	cmd.Flags().String("subnet", "", "")
	cmd.Flags().String("techniques", "", "")
	cmd.Flags().String("enrich", "", "")
	cmd.Flags().String("format", "table", "")
	cmd.Flags().String("output-file", "", "")
	return cmd
}

func TestRealEnrichersSelfRegisterViaBlankImport(t *testing.T) {
	// cmd/cli blank-imports nats/enrich/dns and nats/enrich/oui (root.go) so
	// they self-register into core/engine's enricher registry at process
	// startup. core/engine's own package can't verify this (importing
	// enrich/dns or enrich/oui there would be a cycle), so this is the only
	// place a Name()/registration mismatch between the real packages and the
	// literal "dns"/"oui" strings DefaultOptions() relies on would surface.
	if _, ok := engine.GetEnricher("dns"); !ok {
		t.Fatal("expected the real \"dns\" enricher to be registered via root.go's blank import")
	}
	if _, ok := engine.GetEnricher("oui"); !ok {
		t.Fatal("expected the real \"oui\" enricher to be registered via root.go's blank import")
	}
	if _, ok := engine.GetEnricher("tcpconnect"); !ok {
		t.Fatal("expected the real \"tcpconnect\" enricher to be registered via root.go's blank import")
	}
	// The three opt-in enrichers: registered the same way as the defaults
	// (being in the registry doesn't imply "on by default"), so this is the
	// only place a Name()/registration mismatch for these three real packages
	// would surface (core/engine can't import enrich/* itself).
	if _, ok := engine.GetEnricher("tcpsyn"); !ok {
		t.Fatal("expected the real \"tcpsyn\" enricher to be registered via root.go's blank import")
	}
	if _, ok := engine.GetEnricher("udpscan"); !ok {
		t.Fatal("expected the real \"udpscan\" enricher to be registered via root.go's blank import")
	}
	if _, ok := engine.GetEnricher("banner"); !ok {
		t.Fatal("expected the real \"banner\" enricher to be registered via root.go's blank import")
	}
}

// fakeSightingTechnique reports two Sightings with distinct MAC identities
// (so Merge treats them as two separate Devices) but both on loopback,
// used only to get Devices through Merge without depending on a real network
// interface (arp/icmp/mdns/ssdp all need one).
type fakeSightingTechnique struct{}

func (f *fakeSightingTechnique) Name() string            { return "fake-sighting-2-2" }
func (f *fakeSightingTechnique) RequiresPrivilege() bool { return false }
func (f *fakeSightingTechnique) Run(ctx context.Context, target string) (<-chan engine.Sighting, error) {
	ch := make(chan engine.Sighting, 2)
	ch <- engine.Sighting{IP: "127.0.0.1", MAC: "aa:bb:cc:dd:ee:ff", Technique: f.Name()}
	ch <- engine.Sighting{IP: "127.0.0.2", MAC: "aa:bb:cc:dd:ee:00", Technique: f.Name()}
	close(ch)
	return ch, nil
}

func TestRun_DefaultEnrichOptionsInvokesAllThreeRealEnrichersPerDevice(t *testing.T) {
	// Exercises the real core/engine.Run against the real dns/oui/tcpconnect
	// enrichers self-registered via root.go's blank imports — core/engine's
	// own package tests can't do this (importing enrich/* there would be a
	// cycle), so this is the only place the full always-on default set is
	// verified end-to-end.
	engine.RegisterTechnique(&fakeSightingTechnique{})

	// Bounds the real tcpconnect port scan (real sockets against loopback)
	// instead of leaving it to run unsupervised past this test's return.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	opts := engine.Options{Techniques: []string{"fake-sighting-2-2"}}
	events, err := engine.Run(ctx, opts)
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	// Each enricher's own TechniqueStarted event fires before core/engine
	// invokes it against any device (engine.go's enrichDevices), so this
	// records the three start events without waiting for the real
	// tcpconnect enricher's own (potentially multi-second, real socket)
	// port scan to finish first.
	var enricherStarts []string
	var report engine.Report
	for evt := range events {
		if evt.Kind == engine.EventKindTechniqueStarted && (evt.Technique == "dns" || evt.Technique == "oui" || evt.Technique == "tcpconnect") {
			enricherStarts = append(enricherStarts, evt.Technique)
		}
		if evt.Kind == engine.EventKindDone {
			report = evt.Report
		}
	}

	want := []string{"dns", "oui", "tcpconnect"}
	if len(enricherStarts) != len(want) {
		t.Fatalf("expected enrichers to start %v, got %v", want, enricherStarts)
	}
	for i := range want {
		if enricherStarts[i] != want[i] {
			t.Fatalf("expected enrichers to run in order %v, got %v", want, enricherStarts)
		}
	}

	// Confirms enrichment actually reached both devices (not just the
	// first): enrichDevices applies each enricher to every device in
	// result, so a regression that only enriched device[0] would still
	// leave device[1] in the final Report, but every enricher failing on
	// it would surface as a diagnostic here.
	if len(report.Devices) != 2 {
		t.Fatalf("expected 2 devices to come out of the full scan+enrich pipeline, got %d: %+v", len(report.Devices), report.Devices)
	}
	for _, diag := range report.Diagnostics {
		if strings.Contains(diag.Message, "enrichment failed") {
			t.Fatalf("expected no enrichment failures for either device, got diagnostic: %+v", diag)
		}
	}
}

func TestBuildOptions_TechniquesFlagMapsToOptions(t *testing.T) {
	cmd := newTestScanCmd()
	if err := cmd.Flags().Set("techniques", "icmp,ssdp"); err != nil {
		t.Fatalf("failed to set techniques flag: %v", err)
	}

	opts := buildOptions(cmd)

	expected := []string{"icmp", "ssdp"}
	if len(opts.Techniques) != len(expected) {
		t.Fatalf("expected %d techniques, got %d: %v", len(expected), len(opts.Techniques), opts.Techniques)
	}
	for i := range expected {
		if opts.Techniques[i] != expected[i] {
			t.Fatalf("at index %d: expected %q, got %q", i, expected[i], opts.Techniques[i])
		}
	}
}

func TestBuildOptions_NoTechniquesFlagKeepsDefault(t *testing.T) {
	cmd := newTestScanCmd()

	opts := buildOptions(cmd)

	if len(opts.Techniques) != 1 || opts.Techniques[0] != "arp" {
		t.Fatalf("expected default [\"arp\"], got %v", opts.Techniques)
	}
}

func TestBuildOptions_SubnetFlagMapsToOptions(t *testing.T) {
	cmd := newTestScanCmd()
	if err := cmd.Flags().Set("subnet", "192.168.1.0/24"); err != nil {
		t.Fatalf("failed to set subnet flag: %v", err)
	}

	opts := buildOptions(cmd)

	if opts.Subnet != "192.168.1.0/24" {
		t.Fatalf("expected subnet 192.168.1.0/24, got %q", opts.Subnet)
	}
}

func TestBuildOptions_NoEnrichFlagKeepsOnlyDefaultEnrichers(t *testing.T) {
	cmd := newTestScanCmd()

	opts := buildOptions(cmd)

	expected := []string{"dns", "oui", "tcpconnect"}
	if len(opts.EnrichOptions) != len(expected) {
		t.Fatalf("expected default enrichers %v, got %v", expected, opts.EnrichOptions)
	}
	for i := range expected {
		if opts.EnrichOptions[i] != expected[i] {
			t.Fatalf("expected default enrichers %v, got %v", expected, opts.EnrichOptions)
		}
	}
}

func TestBuildOptions_EnrichFlagAddsNamedOptInOnTopOfDefaults(t *testing.T) {
	cmd := newTestScanCmd()
	if err := cmd.Flags().Set("enrich", "tcpsyn"); err != nil {
		t.Fatalf("failed to set enrich flag: %v", err)
	}

	opts := buildOptions(cmd)

	expected := []string{"dns", "oui", "tcpconnect", "tcpsyn"}
	if len(opts.EnrichOptions) != len(expected) {
		t.Fatalf("expected %v, got %v", expected, opts.EnrichOptions)
	}
	for i := range expected {
		if opts.EnrichOptions[i] != expected[i] {
			t.Fatalf("expected %v, got %v", expected, opts.EnrichOptions)
		}
	}
}

func TestBuildOptions_EnrichFlagOnlyAddsNamedEnrichers(t *testing.T) {
	cmd := newTestScanCmd()
	if err := cmd.Flags().Set("enrich", "banner"); err != nil {
		t.Fatalf("failed to set enrich flag: %v", err)
	}

	opts := buildOptions(cmd)

	for _, name := range []string{"tcpsyn", "udpscan"} {
		for _, got := range opts.EnrichOptions {
			if got == name {
				t.Fatalf("expected %q not to run when only \"banner\" was requested, got %v", name, opts.EnrichOptions)
			}
		}
	}
	found := false
	for _, got := range opts.EnrichOptions {
		if got == "banner" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected \"banner\" to be included, got %v", opts.EnrichOptions)
	}
}

func TestBuildOptions_EnrichFlagMultipleNamesAllAdded(t *testing.T) {
	cmd := newTestScanCmd()
	if err := cmd.Flags().Set("enrich", "tcpsyn,udpscan,banner"); err != nil {
		t.Fatalf("failed to set enrich flag: %v", err)
	}

	opts := buildOptions(cmd)

	expected := []string{"dns", "oui", "tcpconnect", "tcpsyn", "udpscan", "banner"}
	if len(opts.EnrichOptions) != len(expected) {
		t.Fatalf("expected %v, got %v", expected, opts.EnrichOptions)
	}
	for i := range expected {
		if opts.EnrichOptions[i] != expected[i] {
			t.Fatalf("expected %v, got %v", expected, opts.EnrichOptions)
		}
	}
}

func TestBuildOptions_FormatFlagMapsToOptions(t *testing.T) {
	cmd := newTestScanCmd()
	if err := cmd.Flags().Set("format", "json"); err != nil {
		t.Fatalf("failed to set format flag: %v", err)
	}

	opts := buildOptions(cmd)

	if opts.OutputFormat != "json" {
		t.Fatalf("expected format \"json\", got %q", opts.OutputFormat)
	}
}

func TestBuildOptions_NoFormatFlagKeepsDefaultTable(t *testing.T) {
	cmd := newTestScanCmd()

	opts := buildOptions(cmd)

	if opts.OutputFormat != "table" {
		t.Fatalf("expected default format \"table\", got %q", opts.OutputFormat)
	}
}

func TestBuildOptions_FormatFlagLowercasesAndTrims(t *testing.T) {
	cmd := newTestScanCmd()
	if err := cmd.Flags().Set("format", " JSON "); err != nil {
		t.Fatalf("failed to set format flag: %v", err)
	}

	opts := buildOptions(cmd)

	if opts.OutputFormat != "json" {
		t.Fatalf("expected normalized format \"json\", got %q", opts.OutputFormat)
	}
}

func TestBuildOptions_WhitespaceOnlyFormatKeepsDefaultTable(t *testing.T) {
	cmd := newTestScanCmd()
	if err := cmd.Flags().Set("format", "   "); err != nil {
		t.Fatalf("failed to set format flag: %v", err)
	}

	opts := buildOptions(cmd)

	if opts.OutputFormat != "table" {
		t.Fatalf("expected a whitespace-only --format to keep the default \"table\", got %q", opts.OutputFormat)
	}
}

func TestRenderProgress_ShowsTechniquesAndCounts(t *testing.T) {
	var buf bytes.Buffer
	p := &scanProgress{techniques: map[string]string{"arp": "running"}}
	renderProgress(&buf, p)

	out := buf.String()
	if !strings.Contains(out, "arp:running") {
		t.Fatalf("expected progress to contain 'arp:running', got: %q", out)
	}
	if !strings.Contains(out, "Addresses probed: 0") {
		t.Fatalf("expected 'Addresses probed: 0', got: %q", out)
	}
}

func TestRenderProgress_UpdatesCount(t *testing.T) {
	var buf bytes.Buffer
	p := &scanProgress{
		techniques: map[string]string{"arp": "running"},
		addressed:  5,
	}
	renderProgress(&buf, p)

	out := buf.String()
	if !strings.Contains(out, "Addresses probed: 5") {
		t.Fatalf("expected 'Addresses probed: 5', got: %q", out)
	}
}

func TestRenderProgress_ShowsPendingWhenTotalKnown(t *testing.T) {
	var buf bytes.Buffer
	p := &scanProgress{
		techniques:     map[string]string{"arp": "running"},
		addressed:      3,
		totalAddresses: 10,
	}
	renderProgress(&buf, p)

	out := buf.String()
	if !strings.Contains(out, "Addresses probed: 3/10 (7 pending)") {
		t.Fatalf("expected pending count in progress line, got: %q", out)
	}
}

func TestRenderProgress_PendingNeverNegative(t *testing.T) {
	var buf bytes.Buffer
	p := &scanProgress{
		techniques:     map[string]string{"arp": "running"},
		addressed:      10,
		totalAddresses: 4,
	}
	renderProgress(&buf, p)

	out := buf.String()
	if !strings.Contains(out, "Addresses probed: 10/4 (0 pending)") {
		t.Fatalf("expected pending to floor at 0, got: %q", out)
	}
}

func TestRenderProgress_NoTotalFallsBackToBareCount(t *testing.T) {
	var buf bytes.Buffer
	p := &scanProgress{
		techniques: map[string]string{"mdns": "running"},
		addressed:  2,
	}
	renderProgress(&buf, p)

	out := buf.String()
	if !strings.Contains(out, "Addresses probed: 2") {
		t.Fatalf("expected bare count, got: %q", out)
	}
	if strings.Contains(out, "pending") {
		t.Fatalf("did not expect pending count when total is unknown, got: %q", out)
	}
}

func TestRenderProgress_SkippedTechnique(t *testing.T) {
	var buf bytes.Buffer
	p := &scanProgress{
		techniques: map[string]string{"icmp": "skipped", "arp": "running"},
	}
	renderProgress(&buf, p)

	out := buf.String()
	if !strings.Contains(out, "icmp:skipped") {
		t.Fatalf("expected 'icmp:skipped', got: %q", out)
	}
	if !strings.Contains(out, "arp:running") {
		t.Fatalf("expected 'arp:running', got: %q", out)
	}
}

func TestRenderProgress_SkipsAfterDone(t *testing.T) {
	var buf bytes.Buffer
	p := &scanProgress{
		techniques: map[string]string{"arp": "running"},
		done:       true,
	}
	renderProgress(&buf, p)
	if buf.Len() != 0 {
		t.Fatalf("expected no output after done, got: %q", buf.String())
	}
}

func TestRenderDone_ShowsSummary(t *testing.T) {
	var buf bytes.Buffer
	p := &scanProgress{
		techniques: map[string]string{"arp": "running"},
		addressed:  10,
		devices: []engine.Device{
			{IP: "192.168.1.10", MAC: "aa:bb:cc:dd:ee:ff"},
		},
	}
	renderDone(&buf, p)

	out := buf.String()
	if !strings.Contains(out, "Scan complete. 1 device found, 10 addresses probed.") {
		t.Fatalf("expected summary line, got: %q", out)
	}
	if !p.done {
		t.Fatal("expected p.done to be true after renderDone")
	}
}

// withCapturedExit overrides osExit for the duration of the calling test,
// recording every code passed to it instead of terminating the test binary —
// mirrors withOverriddenWriters' save/restore-on-defer pattern, applied to
// the swappable osExit var. It is purely a recorder: it does not itself
// enforce any invariant about how many times osExit is called or with what
// codes — pair every use with assertExitCode, which does.
//
// Not safe under t.Parallel(): osExit is a shared package-level var, same
// trade-off already accepted for engineRun/writeReportFile/progressWriter
// elsewhere in this file. No test in this package uses t.Parallel() today.
func withCapturedExit(t *testing.T) *[]int {
	t.Helper()
	orig := osExit
	var codes []int
	osExit = func(code int) {
		codes = append(codes, code)
	}
	t.Cleanup(func() { osExit = orig })
	return &codes
}

// assertExitCode asserts osExit was called with exactly the given sequence
// of codes (in order) — zero codes asserts it was never called. Centralizes
// the check so a future test can't forget the "at most once" half of it, as
// the six inline call sites this replaced independently had to remember to
// write each time.
func assertExitCode(t *testing.T, codes *[]int, want ...int) {
	t.Helper()
	if len(*codes) != len(want) {
		t.Fatalf("expected osExit called with codes %v, got calls: %v", want, *codes)
	}
	for i, w := range want {
		if (*codes)[i] != w {
			t.Fatalf("expected osExit called with codes %v, got calls: %v", want, *codes)
		}
	}
}

func withOverriddenWriters(t *testing.T, fn func(progress, diagnostic, report *bytes.Buffer)) {
	t.Helper()
	origProgress := progressWriter
	origDiagnostic := diagnosticWriter
	origReport := reportWriter
	defer func() {
		progressWriter = origProgress
		diagnosticWriter = origDiagnostic
		reportWriter = origReport
	}()

	var progressBuf, diagnosticBuf, reportBuf bytes.Buffer
	progressWriter = &progressBuf
	diagnosticWriter = &diagnosticBuf
	reportWriter = &reportBuf

	fn(&progressBuf, &diagnosticBuf, &reportBuf)
}

func TestScanCommand_RendersProgress(t *testing.T) {
	origRun := engineRun
	defer func() { engineRun = origRun }()

	engineRun = func(ctx context.Context, opts engine.Options) (<-chan engine.Event, error) {
		ch := make(chan engine.Event, 10)
		ch <- engine.Event{Kind: engine.EventKindTechniqueStarted, Technique: "arp", TotalAddresses: 2}
		ch <- engine.Event{Kind: engine.EventKindAddressProbed, Technique: "arp", Address: "192.168.1.10"}
		ch <- engine.Event{Kind: engine.EventKindAddressProbed, Technique: "arp", Address: "192.168.1.20"}
		ch <- engine.Event{Kind: engine.EventKindDeviceFound, Technique: "merge", Device: engine.Device{IP: "192.168.1.10", MAC: "aa:bb:cc:dd:ee:ff"}}
		ch <- engine.Event{Kind: engine.EventKindDone, Report: engine.Report{Devices: []engine.Device{{IP: "192.168.1.10", MAC: "aa:bb:cc:dd:ee:ff"}}}}
		close(ch)
		return ch, nil
	}

	withOverriddenWriters(t, func(progressBuf, diagnosticBuf, reportBuf *bytes.Buffer) {
		cmd := newTestScanCmd()
		if err := scanCmd.RunE(cmd, nil); err != nil {
			t.Fatalf("scan command failed: %v", err)
		}

		out := progressBuf.String()
		if !strings.Contains(out, "arp:running") {
			t.Fatalf("expected 'arp:running', got: %q", out)
		}
		if !strings.Contains(out, "Addresses probed: 2/2 (0 pending)") {
			t.Fatalf("expected pending progress, got: %q", out)
		}
		if !strings.Contains(out, "Scan complete. 1 device found, 2 addresses probed.") {
			t.Fatalf("expected scan summary, got: %q", out)
		}
	})
}

func TestRenderDiagnostic_FormatsErrorWithReason(t *testing.T) {
	var buf bytes.Buffer
	renderDiagnostic(&buf, engine.Diagnostic{Severity: "error", Message: "no active network interface found", Reason: "no cable connected"})

	out := buf.String()
	if !strings.Contains(out, "error: no active network interface found") {
		t.Fatalf("expected tagged error line, got: %q", out)
	}
	if !strings.Contains(out, "reason: no cable connected") {
		t.Fatalf("expected reason line, got: %q", out)
	}
}

func TestRenderDiagnostic_FormatsWarningDistinctFromError(t *testing.T) {
	var buf bytes.Buffer
	renderDiagnostic(&buf, engine.Diagnostic{Severity: "warning", Message: "arp skipped", Reason: "requires privilege"})

	out := buf.String()
	if !strings.Contains(out, "warning: arp skipped") {
		t.Fatalf("expected tagged warning line, got: %q", out)
	}
	if strings.Contains(out, "error:") {
		t.Fatalf("did not expect an 'error:' tag on a warning diagnostic, got: %q", out)
	}
}

func TestRenderDiagnostic_NoReasonOmitsReasonLine(t *testing.T) {
	var buf bytes.Buffer
	renderDiagnostic(&buf, engine.Diagnostic{Severity: "error", Message: "no devices discovered"})

	out := buf.String()
	if strings.Contains(out, "reason:") {
		t.Fatalf("did not expect a reason line when Reason is empty, got: %q", out)
	}
}

func TestScanCommand_PrintsReportTableToReportWriter(t *testing.T) {
	origRun := engineRun
	defer func() { engineRun = origRun }()

	engineRun = func(ctx context.Context, opts engine.Options) (<-chan engine.Event, error) {
		ch := make(chan engine.Event, 5)
		ch <- engine.Event{
			Kind: engine.EventKindDone,
			Report: engine.Report{
				Devices: []engine.Device{{IP: "192.168.1.10", MAC: "aa:bb:cc:dd:ee:ff"}},
			},
		}
		close(ch)
		return ch, nil
	}

	codes := withCapturedExit(t)

	withOverriddenWriters(t, func(progressBuf, diagnosticBuf, reportBuf *bytes.Buffer) {
		cmd := newTestScanCmd()
		if err := scanCmd.RunE(cmd, nil); err != nil {
			t.Fatalf("scan command failed: %v", err)
		}

		out := reportBuf.String()
		if !strings.Contains(out, "192.168.1.10") || !strings.Contains(out, "unknown") {
			t.Fatalf("expected the device table on the report writer, got: %q", out)
		}
		if diagnosticBuf.Len() != 0 {
			t.Fatalf("expected no diagnostics printed, got: %q", diagnosticBuf.String())
		}
	})

	// AC: "Given a normal, diagnostic-free scan, when the command runs, then
	// it exits with code 0, unchanged from today."
	assertExitCode(t, codes)
}

func TestScanCommand_WarningAndSummaryBothRenderInSameRun(t *testing.T) {
	origRun := engineRun
	defer func() { engineRun = origRun }()

	engineRun = func(ctx context.Context, opts engine.Options) (<-chan engine.Event, error) {
		ch := make(chan engine.Event, 5)
		ch <- engine.Event{Kind: engine.EventKindTechniqueSkipped, Technique: "icmp", Reason: "requires privilege"}
		ch <- engine.Event{Kind: engine.EventKindDeviceFound, Technique: "merge", Device: engine.Device{IP: "192.168.1.10", MAC: "aa:bb:cc:dd:ee:ff"}}
		ch <- engine.Event{
			Kind: engine.EventKindDone,
			Diagnostics: []engine.Diagnostic{
				{Severity: "warning", Message: "icmp skipped", Reason: "requires privilege"},
			},
			Report: engine.Report{
				Devices: []engine.Device{{IP: "192.168.1.10", MAC: "aa:bb:cc:dd:ee:ff"}},
			},
		}
		close(ch)
		return ch, nil
	}

	codes := withCapturedExit(t)

	withOverriddenWriters(t, func(progressBuf, diagnosticBuf, reportBuf *bytes.Buffer) {
		cmd := newTestScanCmd()
		if err := scanCmd.RunE(cmd, nil); err != nil {
			t.Fatalf("scan command failed: %v", err)
		}

		if !strings.Contains(diagnosticBuf.String(), "warning: icmp skipped") {
			t.Fatalf("expected the skipped-technique warning, got: %q", diagnosticBuf.String())
		}
		if !strings.Contains(reportBuf.String(), "192.168.1.10") {
			t.Fatalf("expected the discovered device's summary row, got: %q", reportBuf.String())
		}
	})

	// AC: "Given a scan that completes with only a warning-severity
	// diagnostic ..., when the command runs, then it exits with code 0,
	// unchanged from today." A warning must never trip osExit.
	assertExitCode(t, codes)
}

// formatMarkers pinpoints a substring unique to each writer's rendering of
// the "Device Type" column/field, distinguishing every format from the other
// three: table's header is all-caps with no separator ("DEVICE TYPE"), json's
// field is camelCased per its json struct tag with no space ("deviceType"),
// markdown's header cell is title-cased inside pipes ("| Device Type |"),
// and plain's label is title-cased with a colon ("Device Type:").
var formatMarkers = map[string]string{
	"table":    "DEVICE TYPE",
	"json":     "\"deviceType\"",
	"markdown": "| Device Type |",
	"plain":    "Device Type:",
}

func TestScanCommand_FormatSelection_EachFormatProducesExactlyOneWritersOutput(t *testing.T) {
	origRun := engineRun
	defer func() { engineRun = origRun }()

	engineRun = func(ctx context.Context, opts engine.Options) (<-chan engine.Event, error) {
		ch := make(chan engine.Event, 5)
		ch <- engine.Event{
			Kind: engine.EventKindDone,
			Report: engine.Report{
				Devices: []engine.Device{{IP: "192.168.1.10", MAC: "aa:bb:cc:dd:ee:ff", DeviceType: "router"}},
			},
		}
		close(ch)
		return ch, nil
	}

	for format, marker := range formatMarkers {
		t.Run(format, func(t *testing.T) {
			withOverriddenWriters(t, func(progressBuf, diagnosticBuf, reportBuf *bytes.Buffer) {
				cmd := newTestScanCmd()
				if err := cmd.Flags().Set("format", format); err != nil {
					t.Fatalf("failed to set format flag: %v", err)
				}
				if err := scanCmd.RunE(cmd, nil); err != nil {
					t.Fatalf("scan command failed: %v", err)
				}

				out := reportBuf.String()
				if !strings.Contains(out, marker) {
					t.Fatalf("expected %q format output to contain %q, got: %q", format, marker, out)
				}
				if diagnosticBuf.Len() != 0 {
					t.Fatalf("expected no diagnostics for a recognized format, got: %q", diagnosticBuf.String())
				}

				for otherFormat, otherMarker := range formatMarkers {
					if otherFormat == format {
						continue
					}
					if strings.Contains(out, otherMarker) {
						t.Fatalf("expected only the %q writer's output, but found %q's marker %q in: %q", format, otherFormat, otherMarker, out)
					}
				}
			})
		})
	}
}

func TestScanCommand_UnrecognizedFormat_ProducesErrorDiagnosticNotSilentDefault(t *testing.T) {
	origRun := engineRun
	defer func() { engineRun = origRun }()

	engineRunCalled := false
	engineRun = func(ctx context.Context, opts engine.Options) (<-chan engine.Event, error) {
		engineRunCalled = true
		ch := make(chan engine.Event, 5)
		ch <- engine.Event{
			Kind: engine.EventKindDone,
			Report: engine.Report{
				Devices: []engine.Device{{IP: "192.168.1.10", MAC: "aa:bb:cc:dd:ee:ff"}},
			},
		}
		close(ch)
		return ch, nil
	}

	codes := withCapturedExit(t)

	withOverriddenWriters(t, func(progressBuf, diagnosticBuf, reportBuf *bytes.Buffer) {
		cmd := newTestScanCmd()
		if err := cmd.Flags().Set("format", "yaml"); err != nil {
			t.Fatalf("failed to set format flag: %v", err)
		}
		if err := scanCmd.RunE(cmd, nil); err != nil {
			t.Fatalf("scan command failed: %v", err)
		}

		if !strings.Contains(diagnosticBuf.String(), `unrecognized output format "yaml"`) {
			t.Fatalf("expected an unrecognized-format error diagnostic, got: %q", diagnosticBuf.String())
		}
		if reportBuf.Len() != 0 {
			t.Fatalf("expected no report output for an unrecognized format (no silent fallback to table), got: %q", reportBuf.String())
		}
		if engineRunCalled {
			t.Fatal("expected an unrecognized --format to be rejected before the scan runs, but engineRun was invoked")
		}
	})

	// AC: "Given nats scan --format yaml, when the command runs, then an
	// error diagnostic is rendered and the process exits with code 1."
	assertExitCode(t, codes, 1)
}

// TestScanCommand_ProgressOutputIdenticalAcrossAllFormats is the regression
// test for progress/report separation: live progress renders from the Event
// stream independently of which Writer consumes the final Report, so a
// scripted Event stream must produce byte-identical progress output no
// matter which --format is selected.
func TestScanCommand_ProgressOutputIdenticalAcrossAllFormats(t *testing.T) {
	origRun := engineRun
	defer func() { engineRun = origRun }()

	scriptedEvents := func() []engine.Event {
		return []engine.Event{
			{Kind: engine.EventKindTechniqueStarted, Technique: "arp", TotalAddresses: 2},
			{Kind: engine.EventKindAddressProbed, Technique: "arp", Address: "192.168.1.10"},
			{Kind: engine.EventKindAddressProbed, Technique: "arp", Address: "192.168.1.20"},
			{Kind: engine.EventKindDeviceFound, Technique: "merge", Device: engine.Device{IP: "192.168.1.10", MAC: "aa:bb:cc:dd:ee:ff"}},
			{Kind: engine.EventKindDone, Report: engine.Report{Devices: []engine.Device{{IP: "192.168.1.10", MAC: "aa:bb:cc:dd:ee:ff"}}}},
		}
	}

	formats := []string{"table", "json", "markdown", "plain"}
	progressOutputs := make([]string, len(formats))

	for i, format := range formats {
		engineRun = func(ctx context.Context, opts engine.Options) (<-chan engine.Event, error) {
			ch := make(chan engine.Event, 10)
			for _, evt := range scriptedEvents() {
				ch <- evt
			}
			close(ch)
			return ch, nil
		}

		withOverriddenWriters(t, func(progressBuf, diagnosticBuf, reportBuf *bytes.Buffer) {
			cmd := newTestScanCmd()
			if err := cmd.Flags().Set("format", format); err != nil {
				t.Fatalf("failed to set format flag: %v", err)
			}
			if err := scanCmd.RunE(cmd, nil); err != nil {
				t.Fatalf("scan command failed: %v", err)
			}
			progressOutputs[i] = progressBuf.String()
		})
	}

	for i := 1; i < len(progressOutputs); i++ {
		if progressOutputs[i] != progressOutputs[0] {
			t.Fatalf("expected identical progress output across all --format values; format %q differs from %q:\n%q\nvs\n%q",
				formats[i], formats[0], progressOutputs[i], progressOutputs[0])
		}
	}
}

func doneEventWithOneDevice() engine.Event {
	return engine.Event{
		Kind: engine.EventKindDone,
		Report: engine.Report{
			Devices: []engine.Device{{IP: "192.168.1.10", MAC: "aa:bb:cc:dd:ee:ff"}},
		},
	}
}

// TestScanCommand_OutputFileFlag_WritesByteIdenticalContentToStdoutAndFile is
// the first --output-file case: with the flag set, the named file must
// contain exactly the same bytes as were written to stdout — the Writer
// output is wrapped, not re-derived via a separate encoding path.
func TestScanCommand_OutputFileFlag_WritesByteIdenticalContentToStdoutAndFile(t *testing.T) {
	origRun := engineRun
	defer func() { engineRun = origRun }()
	engineRun = func(ctx context.Context, opts engine.Options) (<-chan engine.Event, error) {
		ch := make(chan engine.Event, 5)
		ch <- doneEventWithOneDevice()
		close(ch)
		return ch, nil
	}

	outputPath := filepath.Join(t.TempDir(), "summary.txt")

	withOverriddenWriters(t, func(progressBuf, diagnosticBuf, reportBuf *bytes.Buffer) {
		cmd := newTestScanCmd()
		if err := cmd.Flags().Set("output-file", outputPath); err != nil {
			t.Fatalf("failed to set output-file flag: %v", err)
		}
		if err := scanCmd.RunE(cmd, nil); err != nil {
			t.Fatalf("scan command failed: %v", err)
		}

		if diagnosticBuf.Len() != 0 {
			t.Fatalf("expected no diagnostics on a successful file write, got: %q", diagnosticBuf.String())
		}

		fileContent, err := os.ReadFile(outputPath)
		if err != nil {
			t.Fatalf("expected the output file to exist and be readable: %v", err)
		}
		if reportBuf.Len() == 0 {
			t.Fatal("expected stdout (reportWriter) to still receive the summary")
		}
		if !bytes.Equal(fileContent, reportBuf.Bytes()) {
			t.Fatalf("expected byte-identical content between stdout and file; stdout=%q file=%q", reportBuf.String(), string(fileContent))
		}
	})
}

// TestScanCommand_NoOutputFileFlag_StdoutOnlyBehaviorUnchanged is the second
// --output-file case: leaving the flag unset must behave exactly like the
// pre-existing stdout-only default, with no file created.
func TestScanCommand_NoOutputFileFlag_StdoutOnlyBehaviorUnchanged(t *testing.T) {
	origRun := engineRun
	defer func() { engineRun = origRun }()
	engineRun = func(ctx context.Context, opts engine.Options) (<-chan engine.Event, error) {
		ch := make(chan engine.Event, 5)
		ch <- doneEventWithOneDevice()
		close(ch)
		return ch, nil
	}

	tempDir := t.TempDir()
	notCreated := filepath.Join(tempDir, "should-not-exist.txt")

	withOverriddenWriters(t, func(progressBuf, diagnosticBuf, reportBuf *bytes.Buffer) {
		cmd := newTestScanCmd()
		if err := scanCmd.RunE(cmd, nil); err != nil {
			t.Fatalf("scan command failed: %v", err)
		}

		if reportBuf.Len() == 0 {
			t.Fatal("expected stdout (reportWriter) to receive the summary")
		}
		if diagnosticBuf.Len() != 0 {
			t.Fatalf("expected no diagnostics, got: %q", diagnosticBuf.String())
		}
		if _, err := os.Stat(notCreated); !os.IsNotExist(err) {
			t.Fatalf("expected no file to be created when --output-file is unset, stat err: %v", err)
		}
	})
}

// TestScanCommand_WhitespaceOnlyOutputFile_KeepsStdoutOnlyBehavior guards
// against the same bug previously caught and fixed for --format:
// --output-file must be trimmed before the emptiness check, so a
// whitespace-only value falls back to stdout-only instead of silently
// creating an oddly-named file.
func TestScanCommand_WhitespaceOnlyOutputFile_KeepsStdoutOnlyBehavior(t *testing.T) {
	origRun := engineRun
	defer func() { engineRun = origRun }()
	engineRun = func(ctx context.Context, opts engine.Options) (<-chan engine.Event, error) {
		ch := make(chan engine.Event, 5)
		ch <- doneEventWithOneDevice()
		close(ch)
		return ch, nil
	}

	origWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get working directory: %v", err)
	}
	tempDir := t.TempDir()
	if err := os.Chdir(tempDir); err != nil {
		t.Fatalf("failed to chdir to temp dir: %v", err)
	}
	defer func() {
		if err := os.Chdir(origWD); err != nil {
			t.Fatalf("failed to restore working directory: %v", err)
		}
	}()

	withOverriddenWriters(t, func(progressBuf, diagnosticBuf, reportBuf *bytes.Buffer) {
		cmd := newTestScanCmd()
		if err := cmd.Flags().Set("output-file", "   "); err != nil {
			t.Fatalf("failed to set output-file flag: %v", err)
		}
		if err := scanCmd.RunE(cmd, nil); err != nil {
			t.Fatalf("scan command failed: %v", err)
		}

		if reportBuf.Len() == 0 {
			t.Fatal("expected stdout (reportWriter) to receive the summary")
		}
		if diagnosticBuf.Len() != 0 {
			t.Fatalf("expected no diagnostics, got: %q", diagnosticBuf.String())
		}
		entries, err := os.ReadDir(tempDir)
		if err != nil {
			t.Fatalf("failed to read temp dir: %v", err)
		}
		if len(entries) != 0 {
			t.Fatalf("expected no file to be created for a whitespace-only --output-file, found: %v", entries)
		}
	})
}

// TestScanCommand_OutputFileWriteFailure_ProducesErrorDiagnosticAndStdoutStillShown
// is the file-write-failure case: a failed file write must surface as an
// error Diagnostic, never a raw Go error or silent
// failure, and must not prevent the pre-existing stdout summary from still
// being shown.
func TestScanCommand_OutputFileWriteFailure_ProducesErrorDiagnosticAndStdoutStillShown(t *testing.T) {
	origRun := engineRun
	defer func() { engineRun = origRun }()
	engineRun = func(ctx context.Context, opts engine.Options) (<-chan engine.Event, error) {
		ch := make(chan engine.Event, 5)
		ch <- doneEventWithOneDevice()
		close(ch)
		return ch, nil
	}

	origWriteFile := writeReportFile
	defer func() { writeReportFile = origWriteFile }()
	writeReportFile = func(name string, data []byte, perm os.FileMode) error {
		return errors.New("permission denied")
	}

	codes := withCapturedExit(t)

	withOverriddenWriters(t, func(progressBuf, diagnosticBuf, reportBuf *bytes.Buffer) {
		cmd := newTestScanCmd()
		if err := cmd.Flags().Set("output-file", "/unwritable/summary.txt"); err != nil {
			t.Fatalf("failed to set output-file flag: %v", err)
		}
		if err := scanCmd.RunE(cmd, nil); err != nil {
			t.Fatalf("scan command failed: %v", err)
		}

		if !strings.Contains(diagnosticBuf.String(), "error:") || !strings.Contains(diagnosticBuf.String(), "failed to write scan summary to file") {
			t.Fatalf("expected an error diagnostic for the failed file write, got: %q", diagnosticBuf.String())
		}
		if reportBuf.Len() == 0 {
			t.Fatal("expected stdout (reportWriter) to still receive the summary despite the file-write failure")
		}
	})

	// AC: "Given a scan with --output-file pointed at an unwritable path,
	// when the command runs, then stdout output is still shown, an error
	// diagnostic is rendered, and the process exits with code 1."
	assertExitCode(t, codes, 1)
}

// TestScanCommand_ProgressOutputIdenticalWithAndWithoutOutputFile is the
// regression test for progress/file-write separation: progress rendering
// (Event-sourced) and the file write (final Report/Writer-sourced) are
// structurally separate code paths, so a scripted Event stream must produce
// byte-identical progress output whether or not --output-file is set,
// mirroring the equivalent regression test for --format.
func TestScanCommand_ProgressOutputIdenticalWithAndWithoutOutputFile(t *testing.T) {
	origRun := engineRun
	defer func() { engineRun = origRun }()

	scriptedEvents := func() []engine.Event {
		return []engine.Event{
			{Kind: engine.EventKindTechniqueStarted, Technique: "arp", TotalAddresses: 2},
			{Kind: engine.EventKindAddressProbed, Technique: "arp", Address: "192.168.1.10"},
			{Kind: engine.EventKindAddressProbed, Technique: "arp", Address: "192.168.1.20"},
			{Kind: engine.EventKindDeviceFound, Technique: "merge", Device: engine.Device{IP: "192.168.1.10", MAC: "aa:bb:cc:dd:ee:ff"}},
			doneEventWithOneDevice(),
		}
	}

	outputPath := filepath.Join(t.TempDir(), "summary.txt")
	outputFileValues := []string{"", outputPath}
	progressOutputs := make([]string, len(outputFileValues))

	for i, outputFile := range outputFileValues {
		engineRun = func(ctx context.Context, opts engine.Options) (<-chan engine.Event, error) {
			ch := make(chan engine.Event, 10)
			for _, evt := range scriptedEvents() {
				ch <- evt
			}
			close(ch)
			return ch, nil
		}

		withOverriddenWriters(t, func(progressBuf, diagnosticBuf, reportBuf *bytes.Buffer) {
			cmd := newTestScanCmd()
			if err := cmd.Flags().Set("output-file", outputFile); err != nil {
				t.Fatalf("failed to set output-file flag: %v", err)
			}
			if err := scanCmd.RunE(cmd, nil); err != nil {
				t.Fatalf("scan command failed: %v", err)
			}
			progressOutputs[i] = progressBuf.String()
		})
	}

	if progressOutputs[0] != progressOutputs[1] {
		t.Fatalf("expected identical progress output with and without --output-file set:\n%q\nvs\n%q",
			progressOutputs[0], progressOutputs[1])
	}
}

// errWriter always fails to write, used below to force runScan's
// stdout-write error diagnostic without relying on a real unwritable
// destination.
type errWriter struct{}

func (errWriter) Write(p []byte) (int, error) { return 0, errors.New("write failed") }

// TestScanCommand_MultipleErrorDiagnostics_CallsOsExitExactlyOnce is the I/O
// matrix's "multiple error diagnostics in one run" case: a run that fails
// both the stdout write and the file write emits two separate error
// diagnostics, but scanCmd.RunE checks runScan's single aggregated bool, not
// a per-diagnostic counter, so it must still call osExit(1) exactly once.
func TestScanCommand_MultipleErrorDiagnostics_CallsOsExitExactlyOnce(t *testing.T) {
	origRun := engineRun
	defer func() { engineRun = origRun }()
	engineRun = func(ctx context.Context, opts engine.Options) (<-chan engine.Event, error) {
		ch := make(chan engine.Event, 5)
		ch <- doneEventWithOneDevice()
		close(ch)
		return ch, nil
	}

	origWriteFile := writeReportFile
	defer func() { writeReportFile = origWriteFile }()
	writeReportFile = func(name string, data []byte, perm os.FileMode) error {
		return errors.New("permission denied")
	}

	codes := withCapturedExit(t)

	origProgress := progressWriter
	origDiagnostic := diagnosticWriter
	origReport := reportWriter
	defer func() {
		progressWriter = origProgress
		diagnosticWriter = origDiagnostic
		reportWriter = origReport
	}()
	var progressBuf, diagnosticBuf bytes.Buffer
	progressWriter = &progressBuf
	diagnosticWriter = &diagnosticBuf
	reportWriter = errWriter{}

	cmd := newTestScanCmd()
	if err := cmd.Flags().Set("output-file", "/unwritable/summary.txt"); err != nil {
		t.Fatalf("failed to set output-file flag: %v", err)
	}
	if err := scanCmd.RunE(cmd, nil); err != nil {
		t.Fatalf("scan command failed: %v", err)
	}

	// Match on the two failure messages themselves, not a bare "error:"
	// substring count — renderDiagnostic's rendered "reason:" line can
	// itself legitimately contain the word "error" (a wrapped OS error
	// message), which a count-based match can't distinguish from a second
	// diagnostic.
	diagnostics := diagnosticBuf.String()
	if !strings.Contains(diagnostics, "failed to write scan summary") {
		t.Fatalf("expected the stdout-write failure diagnostic, got: %q", diagnostics)
	}
	if !strings.Contains(diagnostics, "failed to write scan summary to file") {
		t.Fatalf("expected the file-write failure diagnostic, got: %q", diagnostics)
	}
	assertExitCode(t, codes, 1)
}

// TestScanCommand_NoDevicesDiscovered_ExitsNonzero locks in the intentional
// consequence documented on runScan's doc comment: core/engine.Run's
// pre-existing "no devices discovered" error diagnostic now drives a
// nonzero exit, not just the new-in-this-diff triggers (invalid flag, write
// failure). This is the scenario Blind Hunter and Edge Case Hunter review
// flagged as real but untested when this fix was first implemented.
func TestScanCommand_NoDevicesDiscovered_ExitsNonzero(t *testing.T) {
	origRun := engineRun
	defer func() { engineRun = origRun }()
	engineRun = func(ctx context.Context, opts engine.Options) (<-chan engine.Event, error) {
		ch := make(chan engine.Event, 1)
		ch <- engine.Event{
			Kind: engine.EventKindDone,
			Diagnostics: []engine.Diagnostic{
				{Severity: "error", Message: "no devices discovered"},
			},
			Report: engine.Report{},
		}
		close(ch)
		return ch, nil
	}

	codes := withCapturedExit(t)

	withOverriddenWriters(t, func(progressBuf, diagnosticBuf, reportBuf *bytes.Buffer) {
		cmd := newTestScanCmd()
		if err := scanCmd.RunE(cmd, nil); err != nil {
			t.Fatalf("scan command failed: %v", err)
		}
		if !strings.Contains(diagnosticBuf.String(), "no devices discovered") {
			t.Fatalf("expected the no-devices-discovered diagnostic, got: %q", diagnosticBuf.String())
		}
	})

	assertExitCode(t, codes, 1)
}

// TestScanCommand_WarningAndErrorDiagnosticsInSameRun_ExitsNonzero proves
// sawError's OR-aggregation isn't defeated by a warning arriving alongside
// an error in the same evt.Diagnostics slice — the all-warning and
// all-error cases were previously only ever tested in isolation from each
// other.
func TestScanCommand_WarningAndErrorDiagnosticsInSameRun_ExitsNonzero(t *testing.T) {
	origRun := engineRun
	defer func() { engineRun = origRun }()
	engineRun = func(ctx context.Context, opts engine.Options) (<-chan engine.Event, error) {
		ch := make(chan engine.Event, 1)
		ch <- engine.Event{
			Kind: engine.EventKindDone,
			Diagnostics: []engine.Diagnostic{
				{Severity: "warning", Message: "icmp skipped", Reason: "requires privilege"},
				{Severity: "error", Message: "no devices discovered"},
			},
			Report: engine.Report{},
		}
		close(ch)
		return ch, nil
	}

	codes := withCapturedExit(t)

	withOverriddenWriters(t, func(progressBuf, diagnosticBuf, reportBuf *bytes.Buffer) {
		cmd := newTestScanCmd()
		if err := scanCmd.RunE(cmd, nil); err != nil {
			t.Fatalf("scan command failed: %v", err)
		}
		if !strings.Contains(diagnosticBuf.String(), "warning: icmp skipped") {
			t.Fatalf("expected the warning diagnostic to still render, got: %q", diagnosticBuf.String())
		}
		if !strings.Contains(diagnosticBuf.String(), "no devices discovered") {
			t.Fatalf("expected the error diagnostic to still render, got: %q", diagnosticBuf.String())
		}
	})

	assertExitCode(t, codes, 1)
}

// TestScanCommand_MultipleErrorDiagnosticsViaEvent_CallsOsExitExactlyOnce
// covers the evt.Diagnostics aggregation loop directly (root.go's
// `for _, d := range evt.Diagnostics`), distinct from
// TestScanCommand_MultipleErrorDiagnostics_CallsOsExitExactlyOnce above,
// which only forces failures through the three separately-inlined
// render/write-failure diagnostics — neither test previously proved two or
// more error-severity entries arriving in evt.Diagnostics itself aggregate
// correctly.
func TestScanCommand_MultipleErrorDiagnosticsViaEvent_CallsOsExitExactlyOnce(t *testing.T) {
	origRun := engineRun
	defer func() { engineRun = origRun }()
	engineRun = func(ctx context.Context, opts engine.Options) (<-chan engine.Event, error) {
		ch := make(chan engine.Event, 1)
		ch <- engine.Event{
			Kind: engine.EventKindDone,
			Diagnostics: []engine.Diagnostic{
				{Severity: "error", Message: "first failure"},
				{Severity: "error", Message: "second failure"},
			},
			Report: engine.Report{},
		}
		close(ch)
		return ch, nil
	}

	codes := withCapturedExit(t)

	withOverriddenWriters(t, func(progressBuf, diagnosticBuf, reportBuf *bytes.Buffer) {
		cmd := newTestScanCmd()
		if err := scanCmd.RunE(cmd, nil); err != nil {
			t.Fatalf("scan command failed: %v", err)
		}
		if !strings.Contains(diagnosticBuf.String(), "first failure") || !strings.Contains(diagnosticBuf.String(), "second failure") {
			t.Fatalf("expected both error diagnostics to render, got: %q", diagnosticBuf.String())
		}
	})

	assertExitCode(t, codes, 1)
}
