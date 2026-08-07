// Package render holds formatting helpers shared by report/table,
// report/markdown, and report/plain, so the four Writer implementations
// (AD-7) don't duplicate the same field-rendering logic.
package render

import (
	"strconv"
	"strings"

	"nats/core/engine"
)

// FormatPorts renders a Device's OpenPorts as a compact "port/protocol"
// list, e.g. "80/tcp,9100/tcp".
func FormatPorts(ports []engine.OpenPort) string {
	if len(ports) == 0 {
		return ""
	}
	parts := make([]string, len(ports))
	for i, p := range ports {
		parts[i] = strconv.Itoa(p.Port) + "/" + p.Protocol
	}
	return strings.Join(parts, ",")
}

// SanitizeLine collapses embedded record- and column-separator characters in
// a field value to spaces. Hostname and Vendor (reverse-DNS, OUI/mDNS/SSDP
// lookups) and a BLE device's broadcast Name all originate from untrusted
// data, so a crafted value could otherwise be indistinguishable from a
// writer's own separators.
//
// Tabs are collapsed alongside newlines because report/table and
// report/ble/table delimit their columns with "\t": a name containing a tab
// would otherwise forge whole cells and push the row's real values into
// columns past the header, which is the same class of defect as an embedded
// newline forging a record boundary.
func SanitizeLine(s string) string {
	s = strings.ReplaceAll(s, "\r\n", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", " ")
	s = strings.ReplaceAll(s, "\t", " ")
	return s
}
