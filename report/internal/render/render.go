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

// SanitizeLine collapses embedded newlines in a field value to spaces.
// Hostname and Vendor originate from untrusted network data (reverse-DNS,
// OUI/mDNS/SSDP lookups), so a crafted value containing a newline could
// otherwise be indistinguishable from a line-oriented writer's own record
// separators.
func SanitizeLine(s string) string {
	s = strings.ReplaceAll(s, "\r\n", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", " ")
	return s
}
