package dns

import (
	"context"
	"errors"
	"testing"

	"nats/core/engine"
)

func TestEnricher_Name(t *testing.T) {
	e := &enricher{}
	if e.Name() != "dns" {
		t.Fatalf("expected name 'dns', got %q", e.Name())
	}
}

func TestEnricher_RequiresPrivilegeIsAlwaysFalse(t *testing.T) {
	e := &enricher{}
	if e.RequiresPrivilege() {
		t.Fatal("expected reverse DNS lookup to never require privilege")
	}
}

func TestEnrich_SetsHostnameFromPTRRecord(t *testing.T) {
	orig := lookupAddr
	defer func() { lookupAddr = orig }()
	lookupAddr = func(ctx context.Context, ip string) ([]string, error) {
		return []string{"host.lan."}, nil
	}

	e := &enricher{}
	device, err := e.Enrich(context.Background(), engine.Device{IP: "192.168.1.10"})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if device.Hostname != "host.lan" {
		t.Fatalf("expected Hostname 'host.lan' (trailing dot trimmed), got %q", device.Hostname)
	}
}

func TestEnrich_OverridesDiscoverySourcedHostname(t *testing.T) {
	orig := lookupAddr
	defer func() { lookupAddr = orig }()
	lookupAddr = func(ctx context.Context, ip string) ([]string, error) {
		return []string{"resolved.lan."}, nil
	}

	e := &enricher{}
	device, err := e.Enrich(context.Background(), engine.Device{IP: "192.168.1.10", Hostname: "from-mdns"})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if device.Hostname != "resolved.lan" {
		t.Fatalf("expected enricher's hostname to override discovery-sourced value, got %q", device.Hostname)
	}
}

func TestEnrich_NoPTRRecordLeavesDeviceUnchangedNotAnError(t *testing.T) {
	orig := lookupAddr
	defer func() { lookupAddr = orig }()
	lookupAddr = func(ctx context.Context, ip string) ([]string, error) {
		return nil, errors.New("lookup: no such host")
	}

	e := &enricher{}
	device, err := e.Enrich(context.Background(), engine.Device{IP: "192.168.1.10", Hostname: "from-mdns"})
	if err != nil {
		t.Fatalf("expected a failed reverse lookup to be a no-op, not an error, got %v", err)
	}
	if device.Hostname != "from-mdns" {
		t.Fatalf("expected Hostname unchanged when lookup fails, got %q", device.Hostname)
	}
}

func TestEnrich_ContextCancellationIsAnError(t *testing.T) {
	orig := lookupAddr
	defer func() { lookupAddr = orig }()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	lookupAddr = func(ctx context.Context, ip string) ([]string, error) {
		return nil, ctx.Err()
	}

	e := &enricher{}
	device, err := e.Enrich(ctx, engine.Device{IP: "192.168.1.10", Hostname: "from-mdns"})
	if err == nil {
		t.Fatal("expected a canceled context to surface as an error, not be treated like a missing PTR record")
	}
	if device.Hostname != "from-mdns" {
		t.Fatalf("expected Hostname unchanged when the lookup is canceled, got %q", device.Hostname)
	}
}

func TestEnrich_EmptyFirstPTRNameLeavesDeviceUnchanged(t *testing.T) {
	orig := lookupAddr
	defer func() { lookupAddr = orig }()
	lookupAddr = func(ctx context.Context, ip string) ([]string, error) {
		return []string{""}, nil
	}

	e := &enricher{}
	device, err := e.Enrich(context.Background(), engine.Device{IP: "192.168.1.10", Hostname: "from-mdns"})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if device.Hostname != "from-mdns" {
		t.Fatalf("expected a blank resolver result not to blank out an existing Hostname, got %q", device.Hostname)
	}
}

func TestEnrich_NoIPLeavesDeviceUnchanged(t *testing.T) {
	called := false
	orig := lookupAddr
	defer func() { lookupAddr = orig }()
	lookupAddr = func(ctx context.Context, ip string) ([]string, error) {
		called = true
		return nil, nil
	}

	e := &enricher{}
	device, err := e.Enrich(context.Background(), engine.Device{})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if called {
		t.Fatal("expected lookupAddr not to be called for a Device with no IP")
	}
	if device.Hostname != "" {
		t.Fatalf("expected empty Hostname, got %q", device.Hostname)
	}
}
