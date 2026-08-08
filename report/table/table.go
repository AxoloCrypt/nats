// Package table renders a Report as the default plain-text summary table:
// IP, MAC, Hostname, Vendor, Open Ports, Device Type. It consumes only the
// engine's final Report struct, never core/engine's live state.
package table

import (
	"bytes"
	"fmt"
	"text/tabwriter"

	"nats/core/engine"
	"nats/report/internal/render"
)

func init() {
	engine.RegisterWriter(Writer{})
}

// Writer renders a Report as a plain-text table.
type Writer struct{}

// Name implements engine.Writer, registering this writer under the "table"
// format name — the default output format.
func (Writer) Name() string {
	return "table"
}

func (Writer) Write(r engine.Report) ([]byte, error) {
	var buf bytes.Buffer
	tw := tabwriter.NewWriter(&buf, 0, 0, 2, ' ', 0)

	fmt.Fprintln(tw, "IP\tMAC\tHOSTNAME\tVENDOR\tOPEN PORTS\tDEVICE TYPE")
	for _, d := range r.Devices {
		// DeviceType's zero value renders as "unknown" — Hostname, Vendor,
		// and OpenPorts have no such taxonomy default and render as their true
		// empty/zero value when no enricher has filled them in.
		deviceType := d.DeviceType
		if deviceType == "" {
			deviceType = engine.DeviceTypeUnknown
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n",
			d.IP, d.MAC, d.Hostname, d.Vendor, render.FormatPorts(d.OpenPorts), deviceType)
	}

	if err := tw.Flush(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
