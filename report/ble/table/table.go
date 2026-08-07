// Package table renders a BLE Report as a plain-text summary table (spine
// AD-11), mirroring report/table's structure: ADDRESS, NAME, VENDOR, DEVICE
// TYPE, DISTANCE. It consumes only core/ble's final Report struct, never
// core/ble's live scan state.
package table

import (
	"bytes"
	"fmt"
	"text/tabwriter"

	"nats/core/ble"
	"nats/report/ble/internal/blerender"
)

func init() {
	ble.RegisterWriter(Writer{})
}

// Writer renders a BLE Report as a plain-text table.
type Writer struct{}

// Name implements ble.Writer, registering this writer under the "table"
// format name — the default format, matching report/table's role for the
// LAN vertical.
func (Writer) Name() string {
	return "table"
}

func (Writer) Write(r ble.Report) ([]byte, error) {
	var buf bytes.Buffer
	tw := tabwriter.NewWriter(&buf, 0, 0, 2, ' ', 0)

	fmt.Fprintln(tw, "ADDRESS\tNAME\tVENDOR\tDEVICE TYPE\tDISTANCE")
	for _, d := range r.Devices {
		// Placeholder substitution and sanitization for every column live in
		// blerender, shared with report/ble/markdown and report/ble/plain —
		// including Name's empty-value fallback, the substitution Story 4.2
		// deferred to "a future Writer" (this one).
		address, name, vendor, deviceType, distance := blerender.Fields(d)
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n",
			address, name, vendor, deviceType, distance)
	}

	if err := tw.Flush(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
