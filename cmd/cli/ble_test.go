package main

import (
	"bytes"
	"context"
	"errors"
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

func TestBLECommand_PrintsDiagnosticAndCompletionMessage(t *testing.T) {
	origBLERun := bleRun
	defer func() { bleRun = origBLERun }()

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
		if !bytes.Contains(reportBuf.Bytes(), []byte("BLE scan complete.")) {
			t.Fatalf("expected the completion message, got: %q", reportBuf.String())
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
// The nil-error assertion pins ble to scan's existing exit-code convention
// (root.go's scanCmd.RunE returns nil even on an error-severity Diagnostic —
// a known, deliberately-deferred limitation from Story 1.1's review). Story
// 4.6 keeps the two commands consistent; changing that convention
// project-wide is out of scope here.
func TestBLECommand_PermissionGap_CompletesCleanly(t *testing.T) {
	reasons := []string{
		"Bluetooth permission denied",
		"Bluetooth adapter unavailable",
	}

	for _, reason := range reasons {
		t.Run(reason, func(t *testing.T) {
			withRegisteredScanner(t, &probeFailScanner{reason: reason})

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
				if !strings.Contains(reportBuf.String(), "BLE scan complete.") {
					t.Fatalf("expected the command to complete normally, got: %q", reportBuf.String())
				}
			})
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
	withRegisteredScanner(t, zeroDeviceScanner{})

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
		if !strings.Contains(reportBuf.String(), "BLE scan complete.") {
			t.Fatalf("expected the command to complete normally, got: %q", reportBuf.String())
		}
	})
}
