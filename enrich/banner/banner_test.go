package banner

import (
	"context"
	"errors"
	"net"
	"strconv"
	"testing"
	"time"

	"nats/core/engine"
)

func TestEnricher_Name(t *testing.T) {
	e := &enricher{}
	if e.Name() != "banner" {
		t.Fatalf("expected 'banner', got %s", e.Name())
	}
}

func TestEnricher_RequiresPrivilegeIsAlwaysFalse(t *testing.T) {
	e := &enricher{}
	if e.RequiresPrivilege() {
		t.Fatal("expected banner grabbing over an already-open connection to never require privilege")
	}
}

func TestEnricher_SelfRegistration(t *testing.T) {
	if _, ok := engine.GetEnricher("banner"); !ok {
		t.Fatal("expected banner to self-register via init()")
	}
}

func TestEnrich_NoIPReturnsUnchanged(t *testing.T) {
	called := false
	orig := dial
	defer func() { dial = orig }()
	dial = func(ctx context.Context, network, address string) (net.Conn, error) {
		called = true
		return nil, errors.New("should not be called")
	}

	e := &enricher{}
	device := engine.Device{OpenPorts: []engine.OpenPort{{Port: 22, Protocol: "tcp", State: "open"}}}
	result, err := e.Enrich(context.Background(), device)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if called {
		t.Fatal("expected dial not to be called for a Device with no IP")
	}
	if len(result.OpenPorts) != 1 || result.OpenPorts[0].Banner != "" {
		t.Fatalf("expected OpenPorts unchanged, got %+v", result.OpenPorts)
	}
}

func TestEnrich_SkipsNonTCPOrNonOpenPorts(t *testing.T) {
	called := false
	orig := dial
	defer func() { dial = orig }()
	dial = func(ctx context.Context, network, address string) (net.Conn, error) {
		called = true
		return nil, errors.New("should not be called")
	}

	e := &enricher{}
	device := engine.Device{
		IP: "10.0.0.5",
		OpenPorts: []engine.OpenPort{
			{Port: 53, Protocol: "udp", State: "open"},
			{Port: 161, Protocol: "udp", State: "open|filtered"},
			{Port: 445, Protocol: "tcp", State: "closed"},
		},
	}
	result, err := e.Enrich(context.Background(), device)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if called {
		t.Fatal("expected dial not to be called for non-tcp/non-open entries")
	}
	for _, op := range result.OpenPorts {
		if op.Banner != "" {
			t.Fatalf("expected no banner set, got %+v", op)
		}
	}
}

func TestEnrich_DialErrorSkipsPortNoChange(t *testing.T) {
	orig := dial
	defer func() { dial = orig }()
	dial = func(ctx context.Context, network, address string) (net.Conn, error) {
		return nil, errors.New("connection refused")
	}

	e := &enricher{}
	device := engine.Device{IP: "10.0.0.5", OpenPorts: []engine.OpenPort{{Port: 22, Protocol: "tcp", State: "open"}}}
	result, err := e.Enrich(context.Background(), device)
	if err != nil {
		t.Fatalf("expected a failed dial to be a no-op, not an error, got %v", err)
	}
	if len(result.OpenPorts) != 1 || result.OpenPorts[0].Banner != "" {
		t.Fatalf("expected OpenPorts unchanged, got %+v", result.OpenPorts)
	}
}

func TestEnrich_NoBannerWithinTimeoutSkipsPort(t *testing.T) {
	origDial := dial
	origTimeout := readTimeout
	defer func() {
		dial = origDial
		readTimeout = origTimeout
	}()
	readTimeout = 50 * time.Millisecond

	clientConn, serverConn := net.Pipe()
	t.Cleanup(func() { serverConn.Close() })
	dial = func(ctx context.Context, network, address string) (net.Conn, error) {
		return clientConn, nil
	}

	e := &enricher{}
	device := engine.Device{IP: "10.0.0.5", OpenPorts: []engine.OpenPort{{Port: 80, Protocol: "tcp", State: "open"}}}
	result, err := e.Enrich(context.Background(), device)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if result.OpenPorts[0].Banner != "" {
		t.Fatalf("expected no banner recorded when the server never sends one, got %+v", result.OpenPorts[0])
	}
}

// TestEnrich_AgainstLocalListener exercises the real dial path (no fake
// swapped in) against an actual local TCP listener that writes a banner
// immediately upon accept, mirroring enrich/tcpconnect's
// TestEnrich_AgainstLocalListener (Story 2.2).
func TestEnrich_AgainstLocalListener(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to start local listener: %v", err)
	}
	defer ln.Close()
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		conn.Write([]byte("220 ready\r\n"))
	}()

	host, portStr, err := net.SplitHostPort(ln.Addr().String())
	if err != nil {
		t.Fatalf("failed to split listener address: %v", err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatalf("failed to parse listener port: %v", err)
	}

	e := &enricher{}
	device := engine.Device{IP: host, OpenPorts: []engine.OpenPort{{Port: port, Protocol: "tcp", State: "open"}}}
	result, err := e.Enrich(context.Background(), device)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(result.OpenPorts) != 1 {
		t.Fatalf("expected exactly 1 OpenPort, got %+v", result.OpenPorts)
	}
	if result.OpenPorts[0].Banner != "220 ready" {
		t.Fatalf("expected banner '220 ready', got %q", result.OpenPorts[0].Banner)
	}
}

func TestEnrich_BannerFoundUpsertsExistingEntryPreservingFields(t *testing.T) {
	origDial := dial
	defer func() { dial = origDial }()

	clientConn, serverConn := net.Pipe()
	go func() {
		serverConn.Write([]byte("SSH-2.0-OpenSSH_8.9\r\n"))
		serverConn.Close()
	}()
	dial = func(ctx context.Context, network, address string) (net.Conn, error) {
		return clientConn, nil
	}

	e := &enricher{}
	device := engine.Device{
		IP:        "10.0.0.5",
		OpenPorts: []engine.OpenPort{{Port: 22, Protocol: "tcp", State: "open"}},
	}
	result, err := e.Enrich(context.Background(), device)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.OpenPorts) != 1 {
		t.Fatalf("expected the existing entry to be updated, not duplicated (AD-11), got %+v", result.OpenPorts)
	}
	got := result.OpenPorts[0]
	if got.Port != 22 || got.Protocol != "tcp" || got.State != "open" {
		t.Fatalf("expected Port/Protocol/State preserved from the existing entry, got %+v", got)
	}
	if got.Banner != "SSH-2.0-OpenSSH_8.9" {
		t.Fatalf("expected banner captured, got %q", got.Banner)
	}
}
