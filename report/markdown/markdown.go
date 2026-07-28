// Package markdown renders a Report as a Markdown table (AD-7), reusing the
// same column set as report/table (Story 1.7): IP, MAC, Hostname, Vendor,
// Open Ports, Device Type. It consumes only the engine's final Report struct,
// never core/engine's live state.
package markdown

import (
	"bytes"
	"fmt"
	"strings"

	"nats/core/engine"
	"nats/report/internal/render"
)

func init() {
	engine.RegisterWriter(Writer{})
}

// Writer renders a Report as a Markdown table.
type Writer struct{}

// Name implements engine.Writer, registering this writer under the
// "markdown" format name.
func (Writer) Name() string {
	return "markdown"
}

func (Writer) Write(r engine.Report) ([]byte, error) {
	var buf bytes.Buffer
	fmt.Fprintln(&buf, "| IP | MAC | Hostname | Vendor | Open Ports | Device Type |")
	fmt.Fprintln(&buf, "| --- | --- | --- | --- | --- | --- |")

	for _, d := range r.Devices {
		// DeviceType's zero value renders as "unknown" per FR-7, matching
		// report/table's convention (Story 1.7) — Hostname, Vendor, and
		// OpenPorts have no such taxonomy default and render as their true
		// empty/zero value.
		deviceType := d.DeviceType
		if deviceType == "" {
			deviceType = engine.DeviceTypeUnknown
		}
		fmt.Fprintf(&buf, "| %s | %s | %s | %s | %s | %s |\n",
			escapeCell(d.IP), escapeCell(d.MAC), escapeCell(d.Hostname), escapeCell(d.Vendor),
			escapeCell(render.FormatPorts(d.OpenPorts)), escapeCell(deviceType))
	}

	return buf.Bytes(), nil
}

// escapeCell sanitizes a value for use inside a Markdown table cell.
// Hostname and Vendor in particular originate from untrusted network data
// (reverse-DNS, OUI lookups), and an unescaped "|" or embedded newline
// would otherwise split a row into extra or misaligned columns.
func escapeCell(s string) string {
	s = render.SanitizeLine(s)
	return strings.ReplaceAll(s, "|", "\\|")
}
