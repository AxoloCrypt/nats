// Package plain renders a Report as simple line-oriented human-readable text
// (AD-7), with no table-drawing characters, suited to restrictive terminals
// or log capture. It consumes only the engine's final Report struct, never
// core/engine's live state.
package plain

import (
	"bytes"
	"fmt"

	"nats/core/engine"
	"nats/report/internal/render"
)

func init() {
	engine.RegisterWriter(Writer{})
}

// Writer renders a Report as line-oriented plain text.
type Writer struct{}

// Name implements engine.Writer, registering this writer under the "plain"
// format name.
func (Writer) Name() string {
	return "plain"
}

func (Writer) Write(r engine.Report) ([]byte, error) {
	var buf bytes.Buffer

	for i, d := range r.Devices {
		if i > 0 {
			fmt.Fprintln(&buf)
		}
		// DeviceType's zero value renders as "unknown" per FR-7, matching
		// report/table's convention (Story 1.7) — Hostname, Vendor, and
		// OpenPorts have no such taxonomy default and render as their true
		// empty/zero value.
		deviceType := d.DeviceType
		if deviceType == "" {
			deviceType = engine.DeviceTypeUnknown
		}
		// Hostname/Vendor originate from untrusted network data (reverse-DNS,
		// OUI lookups); an embedded newline would otherwise be
		// indistinguishable from the blank line separating devices below.
		fmt.Fprintf(&buf, "IP: %s\n", d.IP)
		fmt.Fprintf(&buf, "MAC: %s\n", d.MAC)
		fmt.Fprintf(&buf, "Hostname: %s\n", render.SanitizeLine(d.Hostname))
		fmt.Fprintf(&buf, "Vendor: %s\n", render.SanitizeLine(d.Vendor))
		fmt.Fprintf(&buf, "Open Ports: %s\n", render.FormatPorts(d.OpenPorts))
		fmt.Fprintf(&buf, "Device Type: %s\n", deviceType)
	}

	return buf.Bytes(), nil
}
