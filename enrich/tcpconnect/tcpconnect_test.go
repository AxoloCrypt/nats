package tcpconnect

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
	if e.Name() != "tcpconnect" {
		t.Fatalf("expected name 'tcpconnect', got %q", e.Name())
	}
}

func TestEnricher_RequiresPrivilegeIsAlwaysFalse(t *testing.T) {
	e := &enricher{}
	if e.RequiresPrivilege() {
		t.Fatal("expected a plain TCP connect scan to never require privilege")
	}
}

func TestEnrich_NoIPLeavesDeviceUnchanged(t *testing.T) {
	called := false
	orig := dial
	defer func() { dial = orig }()
	dial = func(ctx context.Context, network, address string) (net.Conn, error) {
		called = true
		return nil, errors.New("should not be called")
	}

	e := &enricher{}
	device, err := e.Enrich(context.Background(), engine.Device{})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if called {
		t.Fatal("expected dial not to be called for a Device with no IP")
	}
	if len(device.OpenPorts) != 0 {
		t.Fatalf("expected no OpenPorts, got %+v", device.OpenPorts)
	}
}

func TestEnrich_RecordsOpenPortForAcceptedConnection(t *testing.T) {
	origPorts := defaultPorts
	defer func() { defaultPorts = origPorts }()
	defaultPorts = []int{22, 80}

	orig := dial
	defer func() { dial = orig }()
	dial = func(ctx context.Context, network, address string) (net.Conn, error) {
		if address == "10.0.0.5:22" {
			return &fakeConn{}, nil
		}
		return nil, errors.New("connection refused")
	}

	e := &enricher{}
	device, err := e.Enrich(context.Background(), engine.Device{IP: "10.0.0.5"})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(device.OpenPorts) != 1 {
		t.Fatalf("expected exactly 1 open port, got %+v", device.OpenPorts)
	}
	want := engine.OpenPort{Port: 22, Protocol: "tcp", State: "open"}
	if device.OpenPorts[0] != want {
		t.Fatalf("expected %+v, got %+v", want, device.OpenPorts[0])
	}
}

func TestEnrich_NoOpenPortsWhenAllConnectsFail(t *testing.T) {
	origPorts := defaultPorts
	defer func() { defaultPorts = origPorts }()
	defaultPorts = []int{22, 80}

	orig := dial
	defer func() { dial = orig }()
	dial = func(ctx context.Context, network, address string) (net.Conn, error) {
		return nil, errors.New("connection refused")
	}

	e := &enricher{}
	device, err := e.Enrich(context.Background(), engine.Device{IP: "10.0.0.5"})
	if err != nil {
		t.Fatalf("expected a failed connect to be a no-op, not an error, got %v", err)
	}
	if len(device.OpenPorts) != 0 {
		t.Fatalf("expected no open ports recorded, got %+v", device.OpenPorts)
	}
}

func TestEnrich_UpsertNotAppendedTwiceForAlreadyKnownPort(t *testing.T) {
	origPorts := defaultPorts
	defer func() { defaultPorts = origPorts }()
	defaultPorts = []int{22}

	orig := dial
	defer func() { dial = orig }()
	dial = func(ctx context.Context, network, address string) (net.Conn, error) {
		return &fakeConn{}, nil
	}

	e := &enricher{}
	device := engine.Device{IP: "10.0.0.5"}
	device.Upsert(engine.OpenPort{Port: 22, Protocol: "tcp", State: "open", Banner: "stale"})

	device, err := e.Enrich(context.Background(), device)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(device.OpenPorts) != 1 {
		t.Fatalf("expected the already-known (Port, Protocol) to be updated, not duplicated (AD-11), got %+v", device.OpenPorts)
	}
}

// TestEnrich_AgainstLocalListener exercises the real dial path (no fake
// swapped in) against an actual local TCP listener, per Task 4's
// requirement to test against a fake/local listener rather than a real
// remote host.
func TestEnrich_AgainstLocalListener(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to start local listener: %v", err)
	}
	defer ln.Close()
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			conn.Close()
		}
	}()

	host, portStr, err := net.SplitHostPort(ln.Addr().String())
	if err != nil {
		t.Fatalf("failed to split listener address: %v", err)
	}
	openPort, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatalf("failed to parse listener port: %v", err)
	}
	closedPort := findClosedPort(t)

	origPorts := defaultPorts
	defer func() { defaultPorts = origPorts }()
	defaultPorts = []int{openPort, closedPort}

	origTimeout := dialTimeout
	defer func() { dialTimeout = origTimeout }()
	dialTimeout = 200 * time.Millisecond

	e := &enricher{}
	device, err := e.Enrich(context.Background(), engine.Device{IP: host})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(device.OpenPorts) != 1 {
		t.Fatalf("expected exactly 1 open port from the real listener, got %+v", device.OpenPorts)
	}
	if device.OpenPorts[0].Port != openPort {
		t.Fatalf("expected the listening port %d to be recorded, got %+v", openPort, device.OpenPorts[0])
	}
}

// findClosedPort binds and immediately closes a listener to get a port
// number that is very likely closed for the duration of the test.
func findClosedPort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to allocate a throwaway port: %v", err)
	}
	_, portStr, _ := net.SplitHostPort(ln.Addr().String())
	ln.Close()
	port, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatalf("failed to parse throwaway port: %v", err)
	}
	return port
}

type fakeConn struct {
	net.Conn
}

func (f *fakeConn) Close() error { return nil }
