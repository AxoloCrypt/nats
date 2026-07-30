package main

import (
	"bytes"
	"context"
	"testing"

	"nats/core/ble"
	"nats/core/engine"
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
