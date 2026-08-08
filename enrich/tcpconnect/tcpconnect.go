// Package tcpconnect implements engine.Enricher via a TCP connect scan
// against a fixed default port list, one of the always-on default
// enrichers.
package tcpconnect

import (
	"context"
	"net"
	"strconv"
	"time"

	"nats/core/engine"
)

func init() {
	engine.RegisterEnricher(&enricher{})
}

// defaultPorts is the v1 default port list for the connect scan. No exact
// set is mandated, so this is a small common-services list (not the full
// well-known-ports range) chosen to keep an unprivileged connect scan
// against every discovered device fast: FTP (21), SSH (22), Telnet (23),
// SMTP (25), DNS (53), HTTP (80), POP3 (110), NetBIOS (139), IMAP (143),
// HTTPS (443), SMB (445), RDP (3389), HTTP alt/proxy (8080), IPP (631),
// JetDirect (9100), RTSP (554), Chromecast control (8009) — the last four
// are here so core/engine's printer/smart-tv port-signature classification
// has real port data to classify against.
var defaultPorts = []int{21, 22, 23, 25, 53, 80, 110, 139, 143, 443, 445, 554, 631, 3389, 8009, 8080, 9100}

type enricher struct{}

func (e *enricher) Name() string {
	return "tcpconnect"
}

func (e *enricher) RequiresPrivilege() bool {
	requires, _ := probePrivilege()
	return requires
}

// ProbePrivilege implements engine.PrivilegeProber. A plain TCP connect
// never needs elevated privilege on any target platform, but it is still
// implemented as a live probe rather than a hardcoded false, for
// consistency with every other adapter — including the opt-in raw-socket
// enrichers, which do need a real check.
func (e *enricher) ProbePrivilege() (bool, error) {
	return probePrivilege()
}

var probePrivilege = func() (bool, error) {
	return false, nil
}

// dialTimeout bounds each individual port connect attempt so an
// unreachable/filtered port (no RST, no SYN-ACK) can't stall the whole
// enrichment step, which core/engine already bounds overall via its own
// enrichTimeout.
var dialTimeout = 500 * time.Millisecond

// dial is swappable so tests can exercise Enrich against a fake dialer
// without needing real sockets or binding a real listener on every port.
var dial = func(ctx context.Context, network, address string) (net.Conn, error) {
	d := net.Dialer{Timeout: dialTimeout}
	return d.DialContext(ctx, network, address)
}

func (e *enricher) Enrich(ctx context.Context, device engine.Device) (engine.Device, error) {
	if device.IP == "" {
		return device, nil
	}

	for _, port := range defaultPorts {
		address := net.JoinHostPort(device.IP, strconv.Itoa(port))
		conn, err := dial(ctx, "tcp", address)
		if err != nil {
			// A refused/filtered/timed-out connect just means the port is
			// not open — an ordinary, expected outcome for most ports on
			// most devices, never an enrichment error.
			continue
		}
		conn.Close()
		device.Upsert(engine.OpenPort{Port: port, Protocol: "tcp", State: "open"})
	}

	return device, nil
}
