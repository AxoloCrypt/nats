// Package plain renders a BLE Report as simple line-oriented human-readable
// text, with no table-drawing characters, mirroring report/plain's
// structure. It consumes only core/ble's final Report struct, never
// core/ble's live scan state.
package plain

import (
	"bytes"
	"fmt"

	"nats/core/ble"
	"nats/report/ble/internal/blerender"
)

func init() {
	ble.RegisterWriter(Writer{})
}

// Writer renders a BLE Report as line-oriented plain text.
type Writer struct{}

// Name implements ble.Writer, registering this writer under the "plain"
// format name.
func (Writer) Name() string {
	return "plain"
}

func (Writer) Write(r ble.Report) ([]byte, error) {
	var buf bytes.Buffer

	for i, d := range r.Devices {
		if i > 0 {
			fmt.Fprintln(&buf)
		}
		// Same placeholder and sanitization rules as report/ble/table, shared
		// via blerender. Sanitization matters most here for Name, which
		// originates from untrusted, attacker-broadcastable data: an embedded
		// newline would otherwise be indistinguishable from the blank line
		// separating devices above.
		address, name, vendor, deviceType, distance := blerender.Fields(d)
		fmt.Fprintf(&buf, "Address: %s\n", address)
		fmt.Fprintf(&buf, "Name: %s\n", name)
		fmt.Fprintf(&buf, "Vendor: %s\n", vendor)
		fmt.Fprintf(&buf, "Device Type: %s\n", deviceType)
		fmt.Fprintf(&buf, "Distance: %s\n", distance)
	}

	return buf.Bytes(), nil
}
