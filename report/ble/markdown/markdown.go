// Package markdown renders a BLE Report as a Markdown table (spine AD-11),
// reusing the same column set as report/ble/table: Address, Name, Vendor,
// Device Type, Distance. It consumes only core/ble's final Report struct,
// never core/ble's live scan state.
package markdown

import (
	"bytes"
	"fmt"
	"strings"

	"nats/core/ble"
	"nats/report/ble/internal/blerender"
)

func init() {
	ble.RegisterWriter(Writer{})
}

// Writer renders a BLE Report as a Markdown table.
type Writer struct{}

// Name implements ble.Writer, registering this writer under the "markdown"
// format name.
func (Writer) Name() string {
	return "markdown"
}

func (Writer) Write(r ble.Report) ([]byte, error) {
	var buf bytes.Buffer
	fmt.Fprintln(&buf, "| Address | Name | Vendor | Device Type | Distance |")
	fmt.Fprintln(&buf, "| --- | --- | --- | --- | --- |")

	for _, d := range r.Devices {
		// Same placeholder and sanitization rules as report/ble/table, shared
		// via blerender; escapeCell then adds the Markdown-specific escaping
		// on top.
		address, name, vendor, deviceType, distance := blerender.Fields(d)
		fmt.Fprintf(&buf, "| %s | %s | %s | %s | %s |\n",
			escapeCell(address), escapeCell(name), escapeCell(vendor), escapeCell(deviceType), escapeCell(distance))
	}

	return buf.Bytes(), nil
}

// escapeCell escapes a value for use inside a Markdown table cell. Name in
// particular originates from untrusted, attacker-broadcastable data, and an
// unescaped "|" would otherwise split a row into extra or misaligned columns.
// (Newlines and tabs are already collapsed upstream by blerender.Fields.)
//
// Backslashes are escaped before pipes, and the order matters: escaping only
// the pipe would turn the input `x\|` into `x\\|`, which a CommonMark parser
// reads as an escaped backslash followed by a *live* cell delimiter — the
// row-splitting this function exists to prevent, reachable by broadcasting a
// name that ends in a backslash.
func escapeCell(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	return strings.ReplaceAll(s, "|", "\\|")
}
