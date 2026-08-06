package main

import (
	"context"
	"fmt"
	"io"

	"nats/core/ble"
	"nats/core/engine"

	"github.com/spf13/cobra"
)

var bleRun = ble.Run

// buildBLEOptions mirrors buildOptions in root.go: start from the package's
// own defaults and override only what the user explicitly passed. Presence
// is checked via Flags().Changed rather than a non-zero comparison because
// a Duration flag's zero value is indistinguishable from an unset one, and
// the two need to stay tellable apart — bleCmd rejects an explicitly passed
// non-positive window instead of silently substituting the default.
func buildBLEOptions(cmd *cobra.Command) ble.Options {
	opts := ble.DefaultOptions()
	if cmd.Flags().Changed("window") {
		// Error ignored, matching buildOptions' style in root.go: it can
		// only fire if "window" is registered with a non-Duration type,
		// which TestBLECommand_WindowFlagReachesBLERun catches directly.
		window, _ := cmd.Flags().GetDuration("window")
		opts.Window = window
	}
	return opts
}

// renderBLEDiagnostic converts a core/ble.Diagnostic into the base spine's
// engine.Diagnostic shape and reuses renderDiagnostic (AD-8), so every
// Diagnostic — LAN or BLE — is still printed through the one function that
// produces the "severity: message" / "  reason: ..." shape, even though
// core/ble.Diagnostic is a distinct type from engine.Diagnostic (NL-AD-1's
// import-boundary rule forbids core/ble depending on core/engine).
func renderBLEDiagnostic(w io.Writer, d ble.Diagnostic) {
	renderDiagnostic(w, engine.Diagnostic{
		Severity: d.Severity,
		Message:  d.Message,
		Reason:   d.Reason,
	})
}

func runBLEScan(w io.Writer, events <-chan ble.Event) {
	for evt := range events {
		if evt.Kind != ble.EventKindDone {
			continue
		}
		for _, d := range evt.Diagnostics {
			renderBLEDiagnostic(diagnosticWriter, d)
		}
		fmt.Fprintln(w, "BLE scan complete.")
	}
}

var bleCmd = &cobra.Command{
	Use:   "ble",
	Short: "Scan for nearby BLE devices",
	Long: fmt.Sprintf(`Scan for nearby Bluetooth Low Energy (BLE) devices.

nats ble is fully independent of "nats scan" — it never triggers LAN or
Wi-Fi discovery, and vice versa. It runs OS-native passive BLE scanning
(CoreBluetooth/WinRT/BlueZ via tinygo.org/x/bluetooth) and never requires
root/sudo; when the OS Bluetooth permission isn't granted, nats reports
why and exits cleanly instead of prompting for elevated privilege.

Each run is a single bounded listening window: nats ble listens for the
--window duration (default %s, must be greater than zero), reports once,
and exits.
Nothing is cached, persisted, or correlated across runs — running it
twice in a row produces two fully independent result sets.

Full table/JSON/markdown/plain device listing is not yet implemented by
this command.`, ble.DefaultOptions().Window),
	RunE: func(cmd *cobra.Command, args []string) error {
		opts := buildBLEOptions(cmd)

		// Resolved before the scan starts (mirroring scanCmd's --format
		// handling in root.go) so an invalid --window is reported
		// immediately rather than after the user waits out a scan. A
		// non-positive window is rejected rather than honoured: it can only
		// ever yield an empty, successful-looking result set, and it leaves
		// discovery/blescan's stop-timer racing a scan that hasn't started.
		if opts.Window <= 0 {
			renderDiagnostic(diagnosticWriter, engine.Diagnostic{
				Severity: "error",
				Message:  fmt.Sprintf("invalid --window %s", opts.Window),
				Reason:   "expected a duration greater than zero (e.g. 5s)",
			})
			return nil
		}

		ctx := context.Background()
		events, err := bleRun(ctx, opts)
		if err != nil {
			return fmt.Errorf("ble run failed: %w", err)
		}

		runBLEScan(reportWriter, events)
		return nil
	},
}

func init() {
	// Local flag on bleCmd only (NL-AD-1) — never a PersistentFlags on a
	// shared parent, matching scanCmd's flags in root.go. A genuine
	// duration value gets pflag's native Duration type (unlike scanCmd's
	// comma-list/enum flags, which are all String) so an invalid value like
	// "abc" is rejected by cobra with a clear error instead of silently
	// parsing to zero.
	bleCmd.Flags().Duration("window", ble.DefaultOptions().Window, "Listening window duration for the BLE scan, greater than zero (e.g. 5s, 10s).")
}
