package main

import (
	"bytes"
	"context"
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
