package oui

import (
	"context"
	"net"
	"testing"

	"nats/core/engine"
)

func TestEnricher_Name(t *testing.T) {
	e := &enricher{}
	if e.Name() != "oui" {
		t.Fatalf("expected name 'oui', got %q", e.Name())
	}
}

func TestEnricher_RequiresPrivilegeIsAlwaysFalse(t *testing.T) {
	e := &enricher{}
	if e.RequiresPrivilege() {
		t.Fatal("expected MAC OUI lookup to never require privilege")
	}
}

func TestEnrich_SetsVendorFromKnownOUI(t *testing.T) {
	orig := vendorLookup
	defer func() { vendorLookup = orig }()
	vendorLookup = func(mac net.HardwareAddr) (string, bool) {
		return "Acme Corp", true
	}

	e := &enricher{}
	device, err := e.Enrich(context.Background(), engine.Device{MAC: "aa:bb:cc:dd:ee:ff"})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if device.Vendor != "Acme Corp" {
		t.Fatalf("expected Vendor 'Acme Corp', got %q", device.Vendor)
	}
}

func TestEnrich_OverridesDiscoverySourcedVendor(t *testing.T) {
	orig := vendorLookup
	defer func() { vendorLookup = orig }()
	vendorLookup = func(mac net.HardwareAddr) (string, bool) {
		return "Resolved Vendor", true
	}

	e := &enricher{}
	device, err := e.Enrich(context.Background(), engine.Device{MAC: "aa:bb:cc:dd:ee:ff", Vendor: "from-ssdp"})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if device.Vendor != "Resolved Vendor" {
		t.Fatalf("expected enricher's vendor to override discovery-sourced value, got %q", device.Vendor)
	}
}

func TestEnrich_UnknownOUILeavesDeviceUnchangedNotAnError(t *testing.T) {
	orig := vendorLookup
	defer func() { vendorLookup = orig }()
	vendorLookup = func(mac net.HardwareAddr) (string, bool) {
		return "", false
	}

	e := &enricher{}
	device, err := e.Enrich(context.Background(), engine.Device{MAC: "aa:bb:cc:dd:ee:ff", Vendor: "from-ssdp"})
	if err != nil {
		t.Fatalf("expected an unknown OUI to be a no-op, not an error, got %v", err)
	}
	if device.Vendor != "from-ssdp" {
		t.Fatalf("expected Vendor unchanged for an unknown OUI, got %q", device.Vendor)
	}
}

func TestEnrich_EmptyVendorStringLeavesDeviceUnchanged(t *testing.T) {
	orig := vendorLookup
	defer func() { vendorLookup = orig }()
	vendorLookup = func(mac net.HardwareAddr) (string, bool) {
		return "", true
	}

	e := &enricher{}
	device, err := e.Enrich(context.Background(), engine.Device{MAC: "aa:bb:cc:dd:ee:ff", Vendor: "from-ssdp"})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if device.Vendor != "from-ssdp" {
		t.Fatalf("expected an empty-but-'ok' lookup result not to blank out an existing Vendor, got %q", device.Vendor)
	}
}

func TestEnrich_NoMACLeavesDeviceUnchanged(t *testing.T) {
	called := false
	orig := vendorLookup
	defer func() { vendorLookup = orig }()
	vendorLookup = func(mac net.HardwareAddr) (string, bool) {
		called = true
		return "", false
	}

	e := &enricher{}
	// Simulates a Device merged purely by IP (Merge's IP-match rule), which
	// has no MAC to resolve a vendor from.
	device, err := e.Enrich(context.Background(), engine.Device{IP: "10.0.0.5"})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if called {
		t.Fatal("expected vendorLookup not to be called for a Device with no MAC")
	}
	if device.Vendor != "" {
		t.Fatalf("expected empty Vendor, got %q", device.Vendor)
	}
}

func TestEnrich_MalformedMACLeavesDeviceUnchangedNotAnError(t *testing.T) {
	e := &enricher{}
	device, err := e.Enrich(context.Background(), engine.Device{MAC: "not-a-mac"})
	if err != nil {
		t.Fatalf("expected a malformed MAC to be a no-op, not an error, got %v", err)
	}
	if device.Vendor != "" {
		t.Fatalf("expected empty Vendor, got %q", device.Vendor)
	}
}

func TestEnrich_RealDataset_KnownVendorResolves(t *testing.T) {
	// Exercises the real github.com/gopacket/gopacket/macs dataset (not the
	// swappable fake) against a stable, long-assigned OUI, confirming the
	// lookup-layer wiring itself (not just the interface boundary logic).
	e := &enricher{}
	device, err := e.Enrich(context.Background(), engine.Device{MAC: "00:00:0c:11:22:33"})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if device.Vendor == "" {
		t.Fatal("expected a known Cisco OUI to resolve to a non-empty vendor")
	}
}
