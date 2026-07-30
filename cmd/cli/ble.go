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
	Long: `Scan for nearby Bluetooth Low Energy (BLE) devices.

nats ble is fully independent of "nats scan" — it never triggers LAN or
Wi-Fi discovery, and vice versa. It runs OS-native passive BLE scanning
(CoreBluetooth/WinRT/BlueZ via tinygo.org/x/bluetooth) and never requires
root/sudo; when the OS Bluetooth permission isn't granted, nats reports
why and exits cleanly instead of prompting for elevated privilege.

Full table/JSON/markdown/plain device listing is not yet implemented by
this command.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		opts := ble.DefaultOptions()

		ctx := context.Background()
		events, err := bleRun(ctx, opts)
		if err != nil {
			return fmt.Errorf("ble run failed: %w", err)
		}

		runBLEScan(reportWriter, events)
		return nil
	},
}
