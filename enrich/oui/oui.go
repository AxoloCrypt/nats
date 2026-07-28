// Package oui implements engine.Enricher via MAC OUI vendor lookup, the
// other of the two default enrichers (FR-5, AD-12).
//
// The vendor dataset is github.com/gopacket/gopacket/macs.ValidMACPrefixMap,
// an embedded, regenerated-from-IEEE table already shipped by gopacket —
// which this module already depends on for discovery/arp's packet capture
// (AD-1's Stack table). Reusing it avoids pulling in a second, separately
// maintained OUI library or dataset for the same lookup.
package oui

import (
	"context"
	"net"

	"nats/core/engine"

	"github.com/gopacket/gopacket/macs"
)

func init() {
	engine.RegisterEnricher(&enricher{})
}

type enricher struct{}

func (e *enricher) Name() string {
	return "oui"
}

func (e *enricher) RequiresPrivilege() bool {
	requires, _ := probePrivilege()
	return requires
}

// ProbePrivilege implements engine.PrivilegeProber. An in-memory table
// lookup never needs elevated privilege, but it is still implemented as a
// live probe (AD-5) rather than a hardcoded false, for consistency with
// every other adapter — including Story 2.3's opt-in enrichers, which do
// need a real check.
func (e *enricher) ProbePrivilege() (bool, error) {
	return probePrivilege()
}

var probePrivilege = func() (bool, error) {
	return false, nil
}

// vendorLookup is swappable so tests can exercise Enrich against a fake
// vendor table rather than the real, very large IEEE dataset.
var vendorLookup = func(mac net.HardwareAddr) (string, bool) {
	if len(mac) < 3 {
		return "", false
	}
	vendor, ok := macs.ValidMACPrefixMap[[3]byte{mac[0], mac[1], mac[2]}]
	return vendor, ok
}

func (e *enricher) Enrich(ctx context.Context, device engine.Device) (engine.Device, error) {
	if device.MAC == "" {
		// A device merged purely by IP (Story 1.4's IP-match rule) has no
		// MAC to resolve a vendor from — expected, not an error.
		return device, nil
	}

	mac, err := net.ParseMAC(device.MAC)
	if err != nil {
		return device, nil
	}

	vendor, ok := vendorLookup(mac)
	if !ok || vendor == "" {
		return device, nil
	}

	device.Vendor = vendor
	return device, nil
}
