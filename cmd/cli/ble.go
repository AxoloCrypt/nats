package main

import (
	"context"
	"fmt"
	"io"
	"strings"

	"nats/core/ble"
	"nats/core/engine"

	_ "nats/report/ble/json"
	_ "nats/report/ble/markdown"
	_ "nats/report/ble/plain"
	_ "nats/report/ble/table"

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

// defaultBLEFormat is the single source of truth for --format's default,
// used both by the flag registration and by resolveBLEFormat's fallback —
// mirroring how --window sources its default from ble.DefaultOptions(). Two
// independent literals could drift silently: changing only the flag default
// would leave --format "" still resolving to the old value, with nothing
// failing to compile and no test catching it.
const defaultBLEFormat = "table"

// resolveBLEFormat mirrors buildOptions' --format handling in root.go: a
// whitespace-only or unset --format keeps the default rather than resolving
// to an empty format name.
func resolveBLEFormat(cmd *cobra.Command) string {
	format := defaultBLEFormat
	if raw, _ := cmd.Flags().GetString("format"); strings.TrimSpace(raw) != "" {
		format = strings.ToLower(strings.TrimSpace(raw))
	}
	return format
}

// renderBLEDone writes the terminal "scan complete" line, mirroring
// renderDone's role for "nats scan".
//
// It goes to progressWriter (stderr), never to the report writer: that is
// what lets it coexist with a Writer's stdout bytes without corrupting them
// — a completion line printed on stdout would make --format json output
// unparseable, which is why Story 4.1's "BLE scan complete." stub had to go.
// Dropping the signal entirely was the wrong half of that fix, though: with
// no completion line, "nats ble --format plain" in a room with nothing
// broadcasting writes zero bytes to stdout (plain renders no devices as no
// output) and zero to stderr (core/ble.Run deliberately reports no
// Diagnostic for an ordinary zero-device scan), leaving the user unable to
// tell a completed scan from a command that silently did nothing.
func renderBLEDone(w io.Writer, deviceCount int) {
	deviceWord := "devices"
	if deviceCount == 1 {
		deviceWord = "device"
	}
	fmt.Fprintf(w, "BLE scan complete. %d %s found.\n", deviceCount, deviceWord)
}

// renderBLEDiagnostic converts a core/ble.Diagnostic into the base spine's
// engine.Diagnostic shape and reuses renderDiagnostic (AD-8), so every
// Diagnostic — LAN or BLE — is still printed through the one function that
// produces the "severity: message" / "  reason: ..." shape, even though
// core/ble.Diagnostic is a distinct type from engine.Diagnostic (NL-AD-1's
// import-boundary rule forbids core/ble depending on core/engine).
//
// It returns whether d was error-severity — purely a passthrough of
// renderDiagnostic's own return value, since severity is part of the
// engine.Diagnostic shape d was just converted into, not a notion BLE
// resolves independently (NL-AD-1: core/ble.Diagnostic has no severity logic
// of its own). This lets runBLEScan track a run's overall exit status
// without itself reading a ble.Diagnostic field — renderBLEDiagnostic must
// stay the only reader of ble.Diagnostic's fields outside this conversion,
// per the AD-8 enforcement test (diagnostic_enforcement_test.go).
func renderBLEDiagnostic(w io.Writer, d ble.Diagnostic) bool {
	return renderDiagnostic(w, engine.Diagnostic{
		Severity: d.Severity,
		Message:  d.Message,
		Reason:   d.Reason,
	})
}

// runBLEScan mirrors root.go's runScan Done-handling block: announce
// completion on stderr, render the final Report via the resolved Writer,
// write it to stdout, and — if outputFile is set — additionally write the
// identical bytes to that file. File persistence wraps the same Writer
// output already written to stdout above (spine AD-11) — it never blocks or
// replaces the stdout write, it's strictly additive.
//
// Note the parameter is named reportW, not w as in runScan: runScan's w is
// its *progress* writer and it sends the report to the reportWriter global,
// whereas this function is called with reportWriter and reaches for
// progressWriter/diagnosticWriter directly. The two are deliberately spelled
// differently so the inverted meaning can't be missed — writing a progress
// line to this parameter would land it on stdout and corrupt --format json.
// runBLEScan reports whether any error-severity Diagnostic was rendered
// during the run (from evt.Diagnostics and the three inline render/write-
// failure diagnostics below) — mirrors runScan's return value in root.go,
// the single source of truth bleCmd.RunE consults to decide whether to
// osExit(1).
func runBLEScan(reportW io.Writer, events <-chan ble.Event, writer ble.Writer, outputFile string) bool {
	sawError := false
	for evt := range events {
		if evt.Kind != ble.EventKindDone {
			continue
		}
		renderBLEDone(progressWriter, len(evt.Report.Devices))
		for _, d := range evt.Diagnostics {
			if renderBLEDiagnostic(diagnosticWriter, d) {
				sawError = true
			}
		}

		out, err := writer.Write(evt.Report)
		if err != nil {
			renderDiagnostic(diagnosticWriter, engine.Diagnostic{
				Severity: "error",
				Message:  "failed to render BLE scan summary",
				Reason:   err.Error(),
			})
			sawError = true
			continue
		}
		if _, err := reportW.Write(out); err != nil {
			renderDiagnostic(diagnosticWriter, engine.Diagnostic{
				Severity: "error",
				Message:  "failed to write BLE scan summary",
				Reason:   err.Error(),
			})
			sawError = true
		}
		if outputFile != "" {
			if err := writeReportFile(outputFile, out, 0o644); err != nil {
				renderDiagnostic(diagnosticWriter, engine.Diagnostic{
					Severity: "error",
					Message:  "failed to write BLE scan summary to file",
					Reason:   err.Error(),
				})
				sawError = true
			}
		}
	}
	return sawError
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

Results are rendered in the same table/JSON/markdown/plain formats "nats
scan" supports, selected via --format (default table), and can optionally
also be written to a file via --output-file.

Exit code: 0 on a clean, warning-only, or zero-device scan (finding nothing
nearby is ordinary, not a failure) — 1 only for an invalid flag or a write
failure.`, ble.DefaultOptions().Window),
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
			// osExit(1), not return fmt.Errorf(...): see the matching
			// comment on scanCmd.RunE's --format check in root.go — cobra
			// would otherwise double-print this on top of the diagnostic.
			osExit(1)
			return nil
		}

		// Resolved before the scan starts (mirroring scanCmd's --format
		// handling in root.go) so an invalid --format is reported
		// immediately, rather than after the user waits through a scan.
		format := resolveBLEFormat(cmd)
		writer, ok := ble.GetWriter(format)
		if !ok {
			renderDiagnostic(diagnosticWriter, engine.Diagnostic{
				Severity: "error",
				Message:  fmt.Sprintf("unrecognized output format %q", format),
				Reason:   "expected one of: " + strings.Join(ble.WriterNames(), ", "),
			})
			osExit(1)
			return nil
		}

		ctx := context.Background()
		events, err := bleRun(ctx, opts)
		if err != nil {
			return fmt.Errorf("ble run failed: %w", err)
		}

		outputFile, _ := cmd.Flags().GetString("output-file")
		if runBLEScan(reportWriter, events, writer, strings.TrimSpace(outputFile)) {
			osExit(1)
		}
		return nil
	},
}

func init() {
	// Local flags on bleCmd only (NL-AD-1) — never a PersistentFlags on a
	// shared parent, matching scanCmd's flags in root.go. A genuine
	// duration value gets pflag's native Duration type (unlike scanCmd's
	// comma-list/enum flags, which are all String) so an invalid value like
	// "abc" is rejected by cobra with a clear error instead of silently
	// parsing to zero.
	bleCmd.Flags().Duration("window", ble.DefaultOptions().Window, "Listening window duration for the BLE scan, greater than zero (e.g. 5s, 10s).")
	// --format/--output-file flag names match nats scan's (NL-AD-1's
	// explicit rule, unchanged since Story 4.1), still declared locally.
	bleCmd.Flags().String("format", defaultBLEFormat, "Output format for the BLE scan summary: table, json, markdown, or plain.")
	bleCmd.Flags().String("output-file", "", "Additionally write the BLE scan summary to this file, verbatim, alongside the normal stdout output. Unset: stdout only.")
}
