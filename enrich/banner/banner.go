// Package banner implements engine.Enricher via best-effort banner
// grabbing on a Device's already-known open TCP ports (from enrich/
// tcpconnect or enrich/tcpsyn), one of three opt-in "deeper enrichment"
// enrichers that never run unless a user explicitly names them via
// cmd/cli's --enrich flag.
package banner

import (
	"context"
	"net"
	"strconv"
	"strings"
	"time"

	"nats/core/engine"
)

func init() {
	engine.RegisterEnricher(&enricher{})
}

type enricher struct{}

func (e *enricher) Name() string {
	return "banner"
}

func (e *enricher) RequiresPrivilege() bool {
	requires, _ := probePrivilege()
	return requires
}

// ProbePrivilege implements engine.PrivilegeProber. Grabbing a banner over
// an already-open TCP connection needs no special privilege, but it is
// still implemented as a live probe rather than a hardcoded false, for
// consistency with every other adapter.
func (e *enricher) ProbePrivilege() (bool, error) {
	return probePrivilege()
}

var probePrivilege = func() (bool, error) {
	return false, nil
}

// dialTimeout/readTimeout bound each port's connect and initial-read
// attempt so an unreachable device or a port that never sends anything
// unsolicited can't stall the whole enrichment step.
var dialTimeout = 500 * time.Millisecond
var readTimeout = 1 * time.Second

// dial is swappable so tests can exercise Enrich against a fake dialer
// without needing real sockets.
var dial = func(ctx context.Context, network, address string) (net.Conn, error) {
	d := net.Dialer{Timeout: dialTimeout}
	return d.DialContext(ctx, network, address)
}

func (e *enricher) Enrich(ctx context.Context, device engine.Device) (engine.Device, error) {
	if device.IP == "" {
		return device, nil
	}

	for _, existing := range device.OpenPorts {
		if existing.Protocol != "tcp" || existing.State != "open" {
			// Banner grabbing only makes sense against a port already
			// confirmed open by tcpconnect/tcpsyn — a "closed" or
			// "open|filtered" entry (e.g. udpscan's ambiguous UDP ports)
			// has nothing listening to read from.
			continue
		}

		banner, ok := grabBanner(ctx, device.IP, existing.Port)
		if !ok {
			continue
		}

		// Start from the existing entry (not a fresh OpenPort{}) so this
		// Upsert only adds Banner — Upsert itself replaces the whole
		// struct, and building a fresh value here would silently
		// blank out Port/Protocol/State's already-recorded values.
		updated := existing
		updated.Banner = banner
		device.Upsert(updated)
	}

	return device, nil
}

func grabBanner(ctx context.Context, ip string, port int) (string, bool) {
	address := net.JoinHostPort(ip, strconv.Itoa(port))
	conn, err := dial(ctx, "tcp", address)
	if err != nil {
		return "", false
	}
	defer conn.Close()

	conn.SetReadDeadline(time.Now().Add(readTimeout))
	buf := make([]byte, 256)
	n, err := conn.Read(buf)
	if err != nil || n == 0 {
		// A read timeout is the ordinary outcome for a protocol that waits
		// for the client to speak first (e.g. HTTP) rather than announcing
		// itself — not an enrichment error.
		return "", false
	}

	trimmed := strings.TrimSpace(string(buf[:n]))
	if trimmed == "" {
		return "", false
	}
	return trimmed, true
}
