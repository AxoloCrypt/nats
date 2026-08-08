package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"nats/core/ble"
	"nats/core/engine"

	"github.com/spf13/cobra"
)

func TestBLEAndScanCommands_AreIndependent(t *testing.T) {
	if bleCmd.Flags() == scanCmd.Flags() {
		t.Fatal("expected ble and scan to have independent flag sets")
	}
	if bleCmd.PersistentPreRunE != nil || scanCmd.PersistentPreRunE != nil {
		t.Fatal("expected neither command to set a PersistentPreRunE")
	}

	found := false
	for _, cmd := range rootCmd.Commands() {
		if cmd == bleCmd {
			found = true
		}
	}
	if !found {
		t.Fatal("expected bleCmd to be registered on rootCmd alongside scanCmd")
	}
}

func TestBLECommand_NeverCallsEngineRun(t *testing.T) {
	origBLERun := bleRun
	origEngineRun := engineRun
	defer func() {
		bleRun = origBLERun
		engineRun = origEngineRun
	}()

	engineRunCalled := false
	engineRun = func(ctx context.Context, opts engine.Options) (<-chan engine.Event, error) {
		engineRunCalled = true
		ch := make(chan engine.Event)
		close(ch)
		return ch, nil
	}

	bleRun = func(ctx context.Context, opts ble.Options) (<-chan ble.Event, error) {
		ch := make(chan ble.Event, 1)
		ch <- ble.Event{Kind: ble.EventKindDone}
		close(ch)
		return ch, nil
	}

	withOverriddenWriters(t, func(progressBuf, diagnosticBuf, reportBuf *bytes.Buffer) {
		if err := bleCmd.RunE(bleCmd, nil); err != nil {
			t.Fatalf("ble command failed: %v", err)
		}
	})

	if engineRunCalled {
		t.Fatal("expected nats ble to never call engine.Run")
	}
}

func TestScanCommand_NeverCallsBLERun(t *testing.T) {
	origBLERun := bleRun
	origEngineRun := engineRun
	defer func() {
		bleRun = origBLERun
		engineRun = origEngineRun
	}()

	bleRunCalled := false
	bleRun = func(ctx context.Context, opts ble.Options) (<-chan ble.Event, error) {
		bleRunCalled = true
		ch := make(chan ble.Event)
		close(ch)
		return ch, nil
	}

	engineRun = func(ctx context.Context, opts engine.Options) (<-chan engine.Event, error) {
		ch := make(chan engine.Event, 1)
		ch <- engine.Event{Kind: engine.EventKindDone, Report: engine.Report{}}
		close(ch)
		return ch, nil
	}

	withOverriddenWriters(t, func(progressBuf, diagnosticBuf, reportBuf *bytes.Buffer) {
		cmd := newTestScanCmd()
		if err := scanCmd.RunE(cmd, nil); err != nil {
			t.Fatalf("scan command failed: %v", err)
		}
	})

	if bleRunCalled {
		t.Fatal("expected nats scan to never call ble.Run")
	}
}

func TestBLECommand_PrintsDiagnosticAndReport(t *testing.T) {
	origBLERun := bleRun
	defer func() { bleRun = origBLERun }()
	// This test asserts on the table writer's "ADDRESS" header, which only
	// appears because --format resolved to its default. Reset it up front
	// rather than deferring: the deferred call restored a flag this test
	// never set, leaving the precondition itself unguaranteed.
	resetBLEFormatFlag(t)

	bleRun = func(ctx context.Context, opts ble.Options) (<-chan ble.Event, error) {
		ch := make(chan ble.Event, 1)
		ch <- ble.Event{
			Kind: ble.EventKindDone,
			Diagnostics: []ble.Diagnostic{
				{Severity: "warning", Message: "BLE scan skipped", Reason: "no adapter"},
			},
		}
		close(ch)
		return ch, nil
	}

	withOverriddenWriters(t, func(progressBuf, diagnosticBuf, reportBuf *bytes.Buffer) {
		if err := bleCmd.RunE(bleCmd, nil); err != nil {
			t.Fatalf("ble command failed: %v", err)
		}

		if !bytes.Contains(diagnosticBuf.Bytes(), []byte("warning: BLE scan skipped")) {
			t.Fatalf("expected the warning diagnostic to be rendered, got: %q", diagnosticBuf.String())
		}
		if !bytes.Contains(diagnosticBuf.Bytes(), []byte("reason: no adapter")) {
			t.Fatalf("expected the reason to be rendered, got: %q", diagnosticBuf.String())
		}
		// A skipped scan still resolves and renders the default writer
		// against an empty Report — a header-only table (AC #1), not a
		// stub completion string.
		if !bytes.Contains(reportBuf.Bytes(), []byte("ADDRESS")) {
			t.Fatalf("expected the header-only default table report, got: %q", reportBuf.String())
		}
	})
}

// newTestBLECmd mirrors newTestScanCmd (root_test.go): a throwaway cobra
// command carrying only the "window" flag, so tests can drive
// buildBLEOptions without mutating the real, package-level bleCmd.
func newTestBLECmd() *cobra.Command {
	cmd := &cobra.Command{Use: "ble"}
	cmd.Flags().Duration("window", ble.DefaultOptions().Window, "")
	return cmd
}

func TestBuildBLEOptions_DefaultsToPackageDefaultWindow(t *testing.T) {
	cmd := newTestBLECmd()

	opts := buildBLEOptions(cmd)

	if opts.Window != ble.DefaultOptions().Window {
		t.Fatalf("expected default window %v, got %v", ble.DefaultOptions().Window, opts.Window)
	}
}

func TestBuildBLEOptions_OverridesWindowWhenExplicitlySet(t *testing.T) {
	cmd := newTestBLECmd()
	if err := cmd.Flags().Set("window", "10s"); err != nil {
		t.Fatalf("failed to set window flag: %v", err)
	}

	opts := buildBLEOptions(cmd)

	if opts.Window != 10*time.Second {
		t.Fatalf("expected window overridden to 10s, got %v", opts.Window)
	}
}

// TestBLECommand_WindowFlag_RejectsInvalidDuration drives the real bleCmd,
// not a throwaway stand-in: the point is that bleCmd's own "window" flag is
// registered with pflag's Duration type, so a garbage value is rejected at
// parse time. Asserting this against a locally-constructed Duration flag
// would only re-test pflag and would pass even if bleCmd had no --window
// flag at all.
func TestBLECommand_WindowFlag_RejectsInvalidDuration(t *testing.T) {
	if err := bleCmd.Flags().Set("window", "abc"); err == nil {
		t.Fatal("expected bleCmd to reject an invalid --window value at parse time")
	}
}

// TestBLECommand_WindowFlagReachesBLERun is the end-to-end proof of AC #2's
// "adjustable via flag": it drives the real bleCmd through the real flag
// registration and asserts the value lands in the ble.Options handed to
// ble.Run. Without it, re-registering "window" as a String flag, or
// renaming it in init() while buildBLEOptions still looks up "window",
// leaves the flag silently inert with the whole suite green.
func TestBLECommand_WindowFlagReachesBLERun(t *testing.T) {
	origBLERun := bleRun
	defer func() { bleRun = origBLERun }()

	var got ble.Options
	bleRun = func(ctx context.Context, opts ble.Options) (<-chan ble.Event, error) {
		got = opts
		ch := make(chan ble.Event, 1)
		ch <- ble.Event{Kind: ble.EventKindDone}
		close(ch)
		return ch, nil
	}

	t.Run("defaults to the package default when unset", func(t *testing.T) {
		got = ble.Options{}
		withOverriddenWriters(t, func(progressBuf, diagnosticBuf, reportBuf *bytes.Buffer) {
			if err := bleCmd.RunE(bleCmd, nil); err != nil {
				t.Fatalf("ble command failed: %v", err)
			}
		})
		if got.Window != ble.DefaultOptions().Window {
			t.Fatalf("expected ble.Run to receive the default window %v, got %v", ble.DefaultOptions().Window, got.Window)
		}
	})

	t.Run("carries an explicitly passed --window through", func(t *testing.T) {
		defer resetBLEWindowFlag(t)
		if err := bleCmd.Flags().Set("window", "10s"); err != nil {
			t.Fatalf("failed to set window flag: %v", err)
		}

		got = ble.Options{}
		withOverriddenWriters(t, func(progressBuf, diagnosticBuf, reportBuf *bytes.Buffer) {
			if err := bleCmd.RunE(bleCmd, nil); err != nil {
				t.Fatalf("ble command failed: %v", err)
			}
		})
		if got.Window != 10*time.Second {
			t.Fatalf("expected ble.Run to receive the overridden window 10s, got %v", got.Window)
		}
	})
}

// resetBLEWindowFlag restores bleCmd's package-level flag state after a test
// mutates it, so flag changes can't leak into another test in this binary.
func resetBLEWindowFlag(t *testing.T) {
	t.Helper()
	flag := bleCmd.Flags().Lookup("window")
	if err := flag.Value.Set(flag.DefValue); err != nil {
		t.Fatalf("failed to restore the window flag: %v", err)
	}
	flag.Changed = false
}

// resetBLEFormatFlag mirrors resetBLEWindowFlag for the "format" flag.
func resetBLEFormatFlag(t *testing.T) {
	t.Helper()
	flag := bleCmd.Flags().Lookup("format")
	if err := flag.Value.Set(flag.DefValue); err != nil {
		t.Fatalf("failed to restore the format flag: %v", err)
	}
	flag.Changed = false
}

// resetBLEOutputFileFlag mirrors resetBLEWindowFlag for the "output-file"
// flag.
func resetBLEOutputFileFlag(t *testing.T) {
	t.Helper()
	flag := bleCmd.Flags().Lookup("output-file")
	if err := flag.Value.Set(flag.DefValue); err != nil {
		t.Fatalf("failed to restore the output-file flag: %v", err)
	}
	flag.Changed = false
}

// TestBLECommand_RejectsNonPositiveWindow covers the resolution of the
// review's decision item: a zero or negative --window is reported as an
// error diagnostic and the scan never starts, rather than producing an
// empty, successful-looking result set (or hanging the real adapter, whose
// stop-timer would race a scan that hasn't begun).
func TestBLECommand_RejectsNonPositiveWindow(t *testing.T) {
	origBLERun := bleRun
	defer func() { bleRun = origBLERun }()

	bleRunCalled := false
	bleRun = func(ctx context.Context, opts ble.Options) (<-chan ble.Event, error) {
		bleRunCalled = true
		ch := make(chan ble.Event)
		close(ch)
		return ch, nil
	}

	for _, window := range []string{"0s", "-3s"} {
		t.Run(window, func(t *testing.T) {
			defer resetBLEWindowFlag(t)
			if err := bleCmd.Flags().Set("window", window); err != nil {
				t.Fatalf("failed to set window flag: %v", err)
			}

			bleRunCalled = false
			codes := withCapturedExit(t)
			withOverriddenWriters(t, func(progressBuf, diagnosticBuf, reportBuf *bytes.Buffer) {
				if err := bleCmd.RunE(bleCmd, nil); err != nil {
					t.Fatalf("expected a clean exit, got: %v", err)
				}

				if !bytes.Contains(diagnosticBuf.Bytes(), []byte("error: invalid --window")) {
					t.Fatalf("expected an error diagnostic naming --window, got: %q", diagnosticBuf.String())
				}
				if !bytes.Contains(diagnosticBuf.Bytes(), []byte("greater than zero")) {
					t.Fatalf("expected the reason to state the constraint, got: %q", diagnosticBuf.String())
				}
				if reportBuf.Len() != 0 {
					t.Fatalf("expected no scan report for a rejected window, got: %q", reportBuf.String())
				}
			})

			if bleRunCalled {
				t.Fatal("expected ble.Run never to be called for a non-positive window")
			}
			// An invalid --window is the same class of upfront,
			// scan-never-started error as an invalid --format, so it must
			// exit 1 the same way.
			assertExitCode(t, codes, 1)
		})
	}
}

// TestBLECommand_HelpText_DocumentsStatelessnessAndWindowFlag guards Task 3
// (AC #1, #2): nats ble --help must tell the user, in its own words, that
// nothing is persisted/correlated across runs and what --window defaults to.
func TestBLECommand_HelpText_DocumentsStatelessnessAndWindowFlag(t *testing.T) {
	long := bleCmd.Long
	for _, want := range []string{"--window", "single bounded listening window", "Nothing is cached, persisted, or correlated across runs"} {
		if !bytes.Contains([]byte(long), []byte(want)) {
			t.Fatalf("expected bleCmd.Long to mention %q, got: %q", want, long)
		}
	}

	// The prose default is interpolated from DefaultOptions(), not typed
	// out, so changing the default can't leave --help quoting a stale
	// number while every test stays green.
	if !bytes.Contains([]byte(long), []byte("default "+ble.DefaultOptions().Window.String())) {
		t.Fatalf("expected bleCmd.Long to quote the default window %v, got: %q", ble.DefaultOptions().Window, long)
	}

	windowFlag := bleCmd.Flags().Lookup("window")
	if windowFlag == nil {
		t.Fatal("expected a registered --window flag on bleCmd")
	}
	if windowFlag.DefValue != ble.DefaultOptions().Window.String() {
		t.Fatalf("expected --window default %v, got %v", ble.DefaultOptions().Window, windowFlag.DefValue)
	}
}

// probeFailScanner is a real ble.BLEScanner, registered through the real
// registry, so the degradation tests below drive the actual
// bleCmd.RunE -> ble.Run -> BLEScanner path end to end rather than stubbing
// bleRun out. Stubbing bleRun would prove only that cmd/cli prints whatever
// it is handed; the point of Story 4.6's AC #1 is that the two halves compose
// — the scanner's refusal reaches the user as a warning and the command still
// exits cleanly.
type probeFailScanner struct {
	reason string
}

func (p *probeFailScanner) Probe() (bool, string) { return false, p.reason }

func (p *probeFailScanner) Scan(ctx context.Context, window time.Duration) (<-chan ble.Advertisement, error) {
	return nil, errors.New("Scan must never be called once Probe has reported the scan can't run")
}

// withRegisteredScanner swaps the process-wide registered BLEScanner (the
// real one is installed by discovery/blescan's init) for the duration of a
// test and restores it afterwards.
func withRegisteredScanner(t *testing.T, s ble.BLEScanner) {
	t.Helper()
	orig, _ := ble.GetScanner()
	t.Cleanup(func() { ble.RegisterScanner(orig) })
	ble.RegisterScanner(s)
}

// zeroDeviceScanner probes clean and then observes nothing — an ordinary
// scan in a room with nothing broadcasting nearby, not a failure.
type zeroDeviceScanner struct{}

func (zeroDeviceScanner) Probe() (bool, string) { return true, "" }

func (zeroDeviceScanner) Scan(ctx context.Context, window time.Duration) (<-chan ble.Advertisement, error) {
	ch := make(chan ble.Advertisement)
	close(ch)
	return ch, nil
}

// diagnosticSeverities extracts the severity token from each rendered
// diagnostic line, so severity assertions can't be confused by a reason
// string that happens to contain "error:" or "warning:". renderDiagnostic
// writes "<severity>: <message>" at column 0 and indents its "  reason:"
// continuation, which is what distinguishes the two here.
func diagnosticSeverities(rendered string) []string {
	var severities []string
	for _, line := range strings.Split(rendered, "\n") {
		if line == "" || strings.HasPrefix(line, " ") {
			continue
		}
		severity, _, found := strings.Cut(line, ": ")
		if found {
			severities = append(severities, severity)
		}
	}
	return severities
}

// runWithTimeout runs fn on its own goroutine and fails the test if it hasn't
// returned in time. AC #1's "completes cleanly rather than crashing or
// hanging" is otherwise untestable: a RunE that blocks forever would simply
// stall the suite until Go's package-level 10-minute panic, with nothing
// pointing at this scenario.
func runWithTimeout(t *testing.T, timeout time.Duration, fn func() error) error {
	t.Helper()

	errCh := make(chan error, 1)
	go func() { errCh <- fn() }()

	select {
	case err := <-errCh:
		return err
	case <-time.After(timeout):
		t.Fatalf("the ble command did not return within %s — it hung instead of completing", timeout)
		return nil
	}
}

// TestBLECommand_PermissionGap_CompletesCleanly is the end-to-end proof of
// AC #1: with the OS refusing Bluetooth access, nats ble prints a warning
// naming what was skipped and why, and still exits normally.
//
// The nil-error assertion pins ble to scan's RunE convention of always
// returning nil (root.go's scanCmd.RunE does the same) — process exit code
// is instead driven by the swappable osExit var, called directly from RunE
// only when an error-severity Diagnostic was rendered (spec
// cli-exit-code-on-error). A warning-severity Diagnostic, as tested here,
// must never trigger it; see the osExit assertion below.
func TestBLECommand_PermissionGap_CompletesCleanly(t *testing.T) {
	// Asserts on the default table writer's "ADDRESS" header below, so the
	// shared bleCmd's --format must be known-default rather than whatever an
	// earlier test happened to leave behind.
	resetBLEFormatFlag(t)

	reasons := []string{
		"Bluetooth permission denied",
		"Bluetooth adapter unavailable",
	}

	for _, reason := range reasons {
		t.Run(reason, func(t *testing.T) {
			withRegisteredScanner(t, &probeFailScanner{reason: reason})

			codes := withCapturedExit(t)

			withOverriddenWriters(t, func(progressBuf, diagnosticBuf, reportBuf *bytes.Buffer) {
				err := runWithTimeout(t, 30*time.Second, func() error {
					return bleCmd.RunE(bleCmd, nil)
				})
				if err != nil {
					t.Fatalf("expected a clean exit on a permission gap, got: %v", err)
				}

				diagnostics := diagnosticBuf.String()
				if !strings.Contains(diagnostics, "warning: BLE scan skipped") {
					t.Fatalf("expected a warning naming what was skipped, got: %q", diagnostics)
				}
				if !strings.Contains(diagnostics, "reason: "+reason) {
					t.Fatalf("expected the scanner's own reason %q to reach the user, got: %q", reason, diagnostics)
				}
				// Checked per line against the severity token only. A
				// substring search over the whole buffer would also scan the
				// "  reason: ..." line, and real BlueZ/D-Bus reasons routinely
				// embed "error:" — that would fail a command that behaved
				// perfectly, on the strength of the adapter's wording.
				if severities := diagnosticSeverities(diagnostics); slices.Contains(severities, "error") {
					t.Fatalf("a permission gap must never be reported at error severity, got severities %v in: %q", severities, diagnostics)
				}
				// A skipped scan still resolves and renders the default
				// writer against an empty Report — a header-only table
				// (AC #1), proving the command completed normally.
				if !strings.Contains(reportBuf.String(), "ADDRESS") {
					t.Fatalf("expected the command to complete normally and render the header-only report, got: %q", reportBuf.String())
				}
			})

			// AC: a warning-only run exits 0, unchanged — osExit must never
			// be called for a permission-gap warning.
			assertExitCode(t, codes)
		})
	}
}

// TestBLECommand_ZeroDevicesFound_IsNotReportedAsFailure is the cmd/cli half
// of Task 2: a scan that ran fine and simply saw nothing nearby prints no
// diagnostic at all — no "no devices discovered" error borrowed from the LAN
// vertical, nothing to suggest the user's Bluetooth is broken.
//
// Driven through the real registry rather than a bleRun stub, for the same
// reason as the permission-gap test above: the rule under test lives in
// core/ble.Run, so a stub that hands back a pre-built diagnostic-free Done
// would only assert that printing no diagnostics prints no diagnostics, and
// would stay green if core/ble ever grew engine.Run's "no devices
// discovered" error — the exact regression this test exists to catch.
func TestBLECommand_ZeroDevicesFound_IsNotReportedAsFailure(t *testing.T) {
	// Same reason as TestBLECommand_PermissionGap_CompletesCleanly: this
	// asserts on the default writer's "ADDRESS" header.
	resetBLEFormatFlag(t)

	withRegisteredScanner(t, zeroDeviceScanner{})

	codes := withCapturedExit(t)

	withOverriddenWriters(t, func(progressBuf, diagnosticBuf, reportBuf *bytes.Buffer) {
		err := runWithTimeout(t, 30*time.Second, func() error {
			return bleCmd.RunE(bleCmd, nil)
		})
		if err != nil {
			t.Fatalf("expected a clean exit for an empty scan, got: %v", err)
		}

		if diagnosticBuf.Len() != 0 {
			t.Fatalf("an ordinary empty BLE scan must print no diagnostic at all, got: %q", diagnosticBuf.String())
		}
		// Zero devices still renders the default writer's header-only table
		// (AC #1) — proof the command completed normally.
		if !strings.Contains(reportBuf.String(), "ADDRESS") {
			t.Fatalf("expected the command to complete normally and render the header-only report, got: %q", reportBuf.String())
		}
	})

	// AC: "Given a normal, diagnostic-free scan, when the command runs,
	// then it exits with code 0, unchanged from today."
	assertExitCode(t, codes)
}

// bleFormatMarkers pinpoints a substring unique to each writer's rendering
// of the "Device Type" column/field, distinguishing every format from the
// other three — mirrors root_test.go's formatMarkers for the LAN vertical.
var bleFormatMarkers = map[string]string{
	"table":    "DEVICE TYPE",
	"json":     "\"deviceType\"",
	"markdown": "| Device Type |",
	"plain":    "Device Type:",
}

func bleDoneEventWithOneDevice() ble.Event {
	return ble.Event{
		Kind: ble.EventKindDone,
		Report: ble.Report{
			Devices: []ble.BLEDeviceProfile{{Address: "aa:bb:cc:dd:ee:ff", DeviceType: "wearable"}},
		},
	}
}

// TestBLECommand_FormatSelection_EachFormatProducesExactlyOneWritersOutput is
// Task 6/7's proof of AC #1's "exactly one writer's output is produced":
// mirrors TestScanCommand_FormatSelection_EachFormatProducesExactlyOneWritersOutput
// for the BLE vertical.
func TestBLECommand_FormatSelection_EachFormatProducesExactlyOneWritersOutput(t *testing.T) {
	origBLERun := bleRun
	defer func() { bleRun = origBLERun }()
	bleRun = func(ctx context.Context, opts ble.Options) (<-chan ble.Event, error) {
		ch := make(chan ble.Event, 1)
		ch <- bleDoneEventWithOneDevice()
		close(ch)
		return ch, nil
	}

	for format, marker := range bleFormatMarkers {
		t.Run(format, func(t *testing.T) {
			defer resetBLEFormatFlag(t)
			withOverriddenWriters(t, func(progressBuf, diagnosticBuf, reportBuf *bytes.Buffer) {
				if err := bleCmd.Flags().Set("format", format); err != nil {
					t.Fatalf("failed to set format flag: %v", err)
				}
				if err := bleCmd.RunE(bleCmd, nil); err != nil {
					t.Fatalf("ble command failed: %v", err)
				}

				out := reportBuf.String()
				if !strings.Contains(out, marker) {
					t.Fatalf("expected %q format output to contain %q, got: %q", format, marker, out)
				}
				if diagnosticBuf.Len() != 0 {
					t.Fatalf("expected no diagnostics for a recognized format, got: %q", diagnosticBuf.String())
				}

				for otherFormat, otherMarker := range bleFormatMarkers {
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

// TestBLECommand_UnrecognizedFormat_ProducesErrorDiagnosticNotSilentDefault
// mirrors TestScanCommand_UnrecognizedFormat_ProducesErrorDiagnosticNotSilentDefault:
// an invalid --format is reported immediately, and the scan never starts.
func TestBLECommand_UnrecognizedFormat_ProducesErrorDiagnosticNotSilentDefault(t *testing.T) {
	origBLERun := bleRun
	defer func() { bleRun = origBLERun }()
	defer resetBLEFormatFlag(t)

	bleRunCalled := false
	bleRun = func(ctx context.Context, opts ble.Options) (<-chan ble.Event, error) {
		bleRunCalled = true
		ch := make(chan ble.Event, 1)
		ch <- bleDoneEventWithOneDevice()
		close(ch)
		return ch, nil
	}

	codes := withCapturedExit(t)

	withOverriddenWriters(t, func(progressBuf, diagnosticBuf, reportBuf *bytes.Buffer) {
		if err := bleCmd.Flags().Set("format", "yaml"); err != nil {
			t.Fatalf("failed to set format flag: %v", err)
		}
		if err := bleCmd.RunE(bleCmd, nil); err != nil {
			t.Fatalf("ble command failed: %v", err)
		}

		if !strings.Contains(diagnosticBuf.String(), `unrecognized output format "yaml"`) {
			t.Fatalf("expected an unrecognized-format error diagnostic, got: %q", diagnosticBuf.String())
		}
		if reportBuf.Len() != 0 {
			t.Fatalf("expected no report output for an unrecognized format (no silent fallback to table), got: %q", reportBuf.String())
		}
	})

	if bleRunCalled {
		t.Fatal("expected an unrecognized --format to be rejected before the scan runs, but bleRun was invoked")
	}
	// AC: "Given nats ble --format yaml, when the command runs, then an
	// error diagnostic is rendered and the process exits with code 1."
	assertExitCode(t, codes, 1)
}

// TestBLECommand_OutputFileFlag_WritesByteIdenticalContentToStdoutAndFile
// mirrors TestScanCommand_OutputFileFlag_WritesByteIdenticalContentToStdoutAndFile:
// the named file must contain exactly the same bytes as were written to
// stdout.
func TestBLECommand_OutputFileFlag_WritesByteIdenticalContentToStdoutAndFile(t *testing.T) {
	origBLERun := bleRun
	defer func() { bleRun = origBLERun }()
	defer resetBLEOutputFileFlag(t)
	bleRun = func(ctx context.Context, opts ble.Options) (<-chan ble.Event, error) {
		ch := make(chan ble.Event, 1)
		ch <- bleDoneEventWithOneDevice()
		close(ch)
		return ch, nil
	}

	outputPath := filepath.Join(t.TempDir(), "ble-summary.txt")

	withOverriddenWriters(t, func(progressBuf, diagnosticBuf, reportBuf *bytes.Buffer) {
		if err := bleCmd.Flags().Set("output-file", outputPath); err != nil {
			t.Fatalf("failed to set output-file flag: %v", err)
		}
		if err := bleCmd.RunE(bleCmd, nil); err != nil {
			t.Fatalf("ble command failed: %v", err)
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

// TestBLECommand_NoOutputFileFlag_StdoutOnlyBehaviorUnchanged mirrors
// TestScanCommand_NoOutputFileFlag_StdoutOnlyBehaviorUnchanged: unset
// --output-file must behave stdout-only, with no file created.
func TestBLECommand_NoOutputFileFlag_StdoutOnlyBehaviorUnchanged(t *testing.T) {
	origBLERun := bleRun
	defer func() { bleRun = origBLERun }()
	bleRun = func(ctx context.Context, opts ble.Options) (<-chan ble.Event, error) {
		ch := make(chan ble.Event, 1)
		ch <- bleDoneEventWithOneDevice()
		close(ch)
		return ch, nil
	}

	tempDir := t.TempDir()
	notCreated := filepath.Join(tempDir, "should-not-exist.txt")

	withOverriddenWriters(t, func(progressBuf, diagnosticBuf, reportBuf *bytes.Buffer) {
		if err := bleCmd.RunE(bleCmd, nil); err != nil {
			t.Fatalf("ble command failed: %v", err)
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

// TestBLECommand_PermissionGapDiagnostic_RendersThroughSameDiagnosticPath is
// Task 7's regression test locking in Task 6's consistency requirement (AC
// #2): Story 4.6's permission-gap warning must print through the exact same
// renderBLEDiagnostic -> renderDiagnostic path as every other BLE
// diagnostic, byte-for-byte identical in shape to a LAN diagnostic rendered
// through renderDiagnostic directly.
func TestBLECommand_PermissionGapDiagnostic_RendersThroughSameDiagnosticPath(t *testing.T) {
	withRegisteredScanner(t, &probeFailScanner{reason: "Bluetooth permission denied"})

	withOverriddenWriters(t, func(progressBuf, diagnosticBuf, reportBuf *bytes.Buffer) {
		err := runWithTimeout(t, 30*time.Second, func() error {
			return bleCmd.RunE(bleCmd, nil)
		})
		if err != nil {
			t.Fatalf("expected a clean exit on a permission gap, got: %v", err)
		}

		var wantBuf bytes.Buffer
		renderDiagnostic(&wantBuf, engine.Diagnostic{
			Severity: "warning",
			Message:  "BLE scan skipped",
			Reason:   "Bluetooth permission denied",
		})

		if diagnosticBuf.String() != wantBuf.String() {
			t.Fatalf("expected the permission-gap warning to render byte-identically to renderDiagnostic's own output;\n got:  %q\n want: %q",
				diagnosticBuf.String(), wantBuf.String())
		}
	})
}

// TestBLECommand_AlwaysAnnouncesCompletionOnStderr covers the completion
// signal that replaced Story 4.1's "BLE scan complete." stub. The stub was
// removed because it printed to the report writer, where it corrupted
// --format json; the fix is to move the signal to stderr, not to drop it.
//
// The plain/zero-device case is the one that matters: plain renders no bytes
// for an empty device list and core/ble.Run reports no Diagnostic for an
// ordinary zero-device scan, so without this line the command would write
// nothing at all to either stream and the user could not tell a completed
// scan from one that silently did nothing.
func TestBLECommand_AlwaysAnnouncesCompletionOnStderr(t *testing.T) {
	origBLERun := bleRun
	defer func() { bleRun = origBLERun }()

	tests := []struct {
		name       string
		format     string
		event      ble.Event
		wantStderr string
		wantEmpty  bool
	}{
		{
			name:       "zero devices, plain (writer emits nothing)",
			format:     "plain",
			event:      ble.Event{Kind: ble.EventKindDone, Report: ble.Report{}},
			wantStderr: "BLE scan complete. 0 devices found.",
			wantEmpty:  true,
		},
		{
			name:       "one device is singular",
			format:     "table",
			event:      bleDoneEventWithOneDevice(),
			wantStderr: "BLE scan complete. 1 device found.",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			defer resetBLEFormatFlag(t)
			if err := bleCmd.Flags().Set("format", tc.format); err != nil {
				t.Fatalf("failed to set format flag: %v", err)
			}
			bleRun = func(ctx context.Context, opts ble.Options) (<-chan ble.Event, error) {
				ch := make(chan ble.Event, 1)
				ch <- tc.event
				close(ch)
				return ch, nil
			}

			withOverriddenWriters(t, func(progressBuf, diagnosticBuf, reportBuf *bytes.Buffer) {
				if err := bleCmd.RunE(bleCmd, nil); err != nil {
					t.Fatalf("ble command failed: %v", err)
				}
				if !bytes.Contains(progressBuf.Bytes(), []byte(tc.wantStderr)) {
					t.Fatalf("expected progress writer to contain %q, got: %q", tc.wantStderr, progressBuf.String())
				}
				// The completion line must never reach stdout: it would make
				// --format json unparseable.
				if bytes.Contains(reportBuf.Bytes(), []byte("BLE scan complete")) {
					t.Fatalf("completion line leaked into the report writer: %q", reportBuf.String())
				}
				if tc.wantEmpty && reportBuf.Len() != 0 {
					t.Fatalf("expected plain writer to emit no report bytes for zero devices, got: %q", reportBuf.String())
				}
			})
		})
	}
}

// TestBLECommand_FormatFlagIsTrimmedAndCaseInsensitive covers resolveBLEFormat's
// TrimSpace/ToLower handling, which is the entire reason the function exists
// and was previously untested.
func TestBLECommand_FormatFlagIsTrimmedAndCaseInsensitive(t *testing.T) {
	origBLERun := bleRun
	defer func() { bleRun = origBLERun }()
	bleRun = func(ctx context.Context, opts ble.Options) (<-chan ble.Event, error) {
		ch := make(chan ble.Event, 1)
		ch <- bleDoneEventWithOneDevice()
		close(ch)
		return ch, nil
	}

	tests := []struct {
		format string
		marker string
	}{
		{"  ", "ADDRESS"},           // whitespace-only falls back to the table default
		{"JSON", `"devices"`},       // uppercase resolves to the json writer
		{"  markdown  ", "| --- |"}, // surrounding whitespace is trimmed
	}

	for _, tc := range tests {
		t.Run("format="+tc.format, func(t *testing.T) {
			defer resetBLEFormatFlag(t)
			if err := bleCmd.Flags().Set("format", tc.format); err != nil {
				t.Fatalf("failed to set format flag: %v", err)
			}
			withOverriddenWriters(t, func(progressBuf, diagnosticBuf, reportBuf *bytes.Buffer) {
				if err := bleCmd.RunE(bleCmd, nil); err != nil {
					t.Fatalf("ble command failed: %v", err)
				}
				if !bytes.Contains(reportBuf.Bytes(), []byte(tc.marker)) {
					t.Fatalf("expected --format %q to resolve and emit %q, got: %q", tc.format, tc.marker, reportBuf.String())
				}
				if bytes.Contains(diagnosticBuf.Bytes(), []byte("unrecognized output format")) {
					t.Fatalf("expected --format %q to be accepted, got: %q", tc.format, diagnosticBuf.String())
				}
			})
		})
	}
}

// TestBLECommand_WhitespaceOnlyOutputFile_KeepsStdoutOnlyBehavior mirrors
// root_test.go's equivalent for "nats scan": a whitespace-only --output-file
// is treated as unset rather than creating a oddly-named file.
func TestBLECommand_WhitespaceOnlyOutputFile_KeepsStdoutOnlyBehavior(t *testing.T) {
	origBLERun := bleRun
	origWrite := writeReportFile
	defer func() { bleRun = origBLERun; writeReportFile = origWrite }()
	bleRun = func(ctx context.Context, opts ble.Options) (<-chan ble.Event, error) {
		ch := make(chan ble.Event, 1)
		ch <- bleDoneEventWithOneDevice()
		close(ch)
		return ch, nil
	}

	writeCalled := false
	writeReportFile = func(name string, data []byte, perm os.FileMode) error {
		writeCalled = true
		return nil
	}

	defer resetBLEOutputFileFlag(t)
	if err := bleCmd.Flags().Set("output-file", "   "); err != nil {
		t.Fatalf("failed to set output-file flag: %v", err)
	}

	withOverriddenWriters(t, func(progressBuf, diagnosticBuf, reportBuf *bytes.Buffer) {
		if err := bleCmd.RunE(bleCmd, nil); err != nil {
			t.Fatalf("ble command failed: %v", err)
		}
		if writeCalled {
			t.Fatal("expected a whitespace-only --output-file to be treated as unset")
		}
		if reportBuf.Len() == 0 {
			t.Fatal("expected the report to still be written to stdout")
		}
	})
}

// TestBLECommand_OutputFileWriteFailure_ProducesErrorDiagnosticAndStdoutStillShown
// mirrors root_test.go's equivalent: file persistence is strictly additive, so
// a failure to write it must never suppress the stdout report.
func TestBLECommand_OutputFileWriteFailure_ProducesErrorDiagnosticAndStdoutStillShown(t *testing.T) {
	origBLERun := bleRun
	origWrite := writeReportFile
	defer func() { bleRun = origBLERun; writeReportFile = origWrite }()
	bleRun = func(ctx context.Context, opts ble.Options) (<-chan ble.Event, error) {
		ch := make(chan ble.Event, 1)
		ch <- bleDoneEventWithOneDevice()
		close(ch)
		return ch, nil
	}
	writeReportFile = func(name string, data []byte, perm os.FileMode) error {
		return errors.New("permission denied")
	}

	defer resetBLEOutputFileFlag(t)
	if err := bleCmd.Flags().Set("output-file", "/nonexistent/out.txt"); err != nil {
		t.Fatalf("failed to set output-file flag: %v", err)
	}

	codes := withCapturedExit(t)

	withOverriddenWriters(t, func(progressBuf, diagnosticBuf, reportBuf *bytes.Buffer) {
		if err := bleCmd.RunE(bleCmd, nil); err != nil {
			t.Fatalf("ble command failed: %v", err)
		}
		if !bytes.Contains(diagnosticBuf.Bytes(), []byte("error: failed to write BLE scan summary to file")) {
			t.Fatalf("expected an error diagnostic for the failed file write, got: %q", diagnosticBuf.String())
		}
		if !bytes.Contains(diagnosticBuf.Bytes(), []byte("reason: permission denied")) {
			t.Fatalf("expected the underlying reason to be rendered, got: %q", diagnosticBuf.String())
		}
		if !bytes.Contains(reportBuf.Bytes(), []byte("ADDRESS")) {
			t.Fatalf("expected the stdout report to survive a file-write failure, got: %q", reportBuf.String())
		}
	})

	// Mirrors root.go's equivalent AC: stdout output is still shown, an
	// error diagnostic is rendered, and the process exits with code 1.
	assertExitCode(t, codes, 1)
}

// bleErrWriter always fails to write, used below to force runBLEScan's
// stdout-write error diagnostic without relying on a real unwritable
// destination — mirrors root_test.go's errWriter.
type bleErrWriter struct{}

func (bleErrWriter) Write(p []byte) (int, error) { return 0, errors.New("write failed") }

// TestBLECommand_MultipleErrorDiagnostics_CallsOsExitExactlyOnce is the
// BLE-side counterpart to root_test.go's
// TestScanCommand_MultipleErrorDiagnostics_CallsOsExitExactlyOnce — every
// other test added by this fix is a deliberate one-for-one mirror between
// scan and ble, and this aggregation case (osExit called exactly once
// despite two separate error diagnostics in the same run) is exactly the
// kind of bug a future runBLEScan refactor could reintroduce without a
// dedicated regression test catching it.
func TestBLECommand_MultipleErrorDiagnostics_CallsOsExitExactlyOnce(t *testing.T) {
	origBLERun := bleRun
	origWrite := writeReportFile
	defer func() { bleRun = origBLERun; writeReportFile = origWrite }()
	bleRun = func(ctx context.Context, opts ble.Options) (<-chan ble.Event, error) {
		ch := make(chan ble.Event, 1)
		ch <- bleDoneEventWithOneDevice()
		close(ch)
		return ch, nil
	}
	writeReportFile = func(name string, data []byte, perm os.FileMode) error {
		return errors.New("permission denied")
	}

	defer resetBLEOutputFileFlag(t)
	if err := bleCmd.Flags().Set("output-file", "/nonexistent/out.txt"); err != nil {
		t.Fatalf("failed to set output-file flag: %v", err)
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
	reportWriter = bleErrWriter{}

	if err := bleCmd.RunE(bleCmd, nil); err != nil {
		t.Fatalf("ble command failed: %v", err)
	}

	diagnostics := diagnosticBuf.String()
	if !strings.Contains(diagnostics, "failed to write BLE scan summary") {
		t.Fatalf("expected the stdout-write failure diagnostic, got: %q", diagnostics)
	}
	if !strings.Contains(diagnostics, "failed to write BLE scan summary to file") {
		t.Fatalf("expected the file-write failure diagnostic, got: %q", diagnostics)
	}
	assertExitCode(t, codes, 1)
}

// TestBLECommand_SkippedScanIsVisibleInJSON covers the Report.Diagnostics
// field: without it, "nats ble --format json" emits an identical empty device
// list for an empty room and for a scan that never ran, leaving a scripting
// consumer unable to tell a successful result from a degraded one.
func TestBLECommand_SkippedScanIsVisibleInJSON(t *testing.T) {
	origBLERun := bleRun
	defer func() { bleRun = origBLERun }()
	bleRun = func(ctx context.Context, opts ble.Options) (<-chan ble.Event, error) {
		diags := []ble.Diagnostic{{
			Severity: "warning",
			Message:  "BLE scan skipped",
			Reason:   "bluetooth permission not granted",
		}}
		ch := make(chan ble.Event, 1)
		ch <- ble.Event{
			Kind:        ble.EventKindDone,
			Diagnostics: diags,
			Report:      ble.Report{Diagnostics: diags},
		}
		close(ch)
		return ch, nil
	}

	defer resetBLEFormatFlag(t)
	if err := bleCmd.Flags().Set("format", "json"); err != nil {
		t.Fatalf("failed to set format flag: %v", err)
	}

	withOverriddenWriters(t, func(progressBuf, diagnosticBuf, reportBuf *bytes.Buffer) {
		if err := bleCmd.RunE(bleCmd, nil); err != nil {
			t.Fatalf("ble command failed: %v", err)
		}
		var payload struct {
			Devices     []map[string]any `json:"devices"`
			Diagnostics []struct {
				Severity string `json:"severity"`
				Message  string `json:"message"`
				Reason   string `json:"reason"`
			} `json:"diagnostics"`
		}
		if err := json.Unmarshal(reportBuf.Bytes(), &payload); err != nil {
			t.Fatalf("expected valid JSON, got %q: %v", reportBuf.String(), err)
		}
		if len(payload.Diagnostics) != 1 {
			t.Fatalf("expected the skip warning to appear in the JSON payload, got: %q", reportBuf.String())
		}
		if payload.Diagnostics[0].Reason != "bluetooth permission not granted" {
			t.Fatalf("expected the verbatim reason in JSON, got: %+v", payload.Diagnostics[0])
		}
		// The stderr rendering must still happen — the JSON copy is additive.
		if !bytes.Contains(diagnosticBuf.Bytes(), []byte("warning: BLE scan skipped")) {
			t.Fatalf("expected the diagnostic to still render to stderr, got: %q", diagnosticBuf.String())
		}
	})
}
