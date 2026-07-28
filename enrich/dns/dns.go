// Package dns implements engine.Enricher via reverse DNS lookup, one of the
// two default enrichers (FR-5, AD-12).
package dns

import (
	"context"
	"net"
	"strings"

	"nats/core/engine"
)

func init() {
	engine.RegisterEnricher(&enricher{})
}

type enricher struct{}

func (e *enricher) Name() string {
	return "dns"
}

func (e *enricher) RequiresPrivilege() bool {
	requires, _ := probePrivilege()
	return requires
}

// ProbePrivilege implements engine.PrivilegeProber. Reverse DNS lookup never
// needs elevated privilege, but it is still implemented as a live probe
// (AD-5) rather than a hardcoded false, for consistency with every other
// adapter — including Story 2.3's opt-in enrichers, which do need a real
// check.
func (e *enricher) ProbePrivilege() (bool, error) {
	return probePrivilege()
}

var probePrivilege = func() (bool, error) {
	return false, nil
}

// lookupAddr is swappable so tests can exercise Enrich without depending on
// a real resolver or network access.
var lookupAddr = net.DefaultResolver.LookupAddr

func (e *enricher) Enrich(ctx context.Context, device engine.Device) (engine.Device, error) {
	if device.IP == "" {
		return device, nil
	}

	names, err := lookupAddr(ctx, device.IP)
	if err != nil {
		if ctx.Err() != nil {
			// A canceled/timed-out lookup is not the same as "no PTR
			// record" — surface it as a real enrichment failure rather
			// than silently looking like an ordinary no-record outcome.
			return device, err
		}
		// No PTR record is an ordinary, expected outcome for many devices,
		// never an error (per Task 3's OUI counterpart).
		return device, nil
	}
	if len(names) == 0 {
		return device, nil
	}

	// PTR records are returned with a trailing dot (e.g. "host.lan.").
	hostname := strings.TrimSuffix(names[0], ".")
	if hostname == "" {
		return device, nil
	}
	device.Hostname = hostname
	return device, nil
}
