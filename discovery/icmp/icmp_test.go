package icmp

import (
	"context"
	"errors"
	"net"
	"strings"
	"testing"

	"nats/core/engine"

	"golang.org/x/net/icmp"
)

func TestTechnique_Name(t *testing.T) {
	tech := &technique{}
	if tech.Name() != "icmp" {
		t.Fatalf("expected 'icmp', got %s", tech.Name())
	}
}

func TestTechnique_RequiresPrivilege(t *testing.T) {
	orig := probePrivilege
	defer func() { probePrivilege = orig }()

	probePrivilege = func() (bool, error) { return false, nil }
	tech := &technique{}
	if tech.RequiresPrivilege() {
		t.Fatal("expected RequiresPrivilege to return false from mocked probe")
	}
}

func TestTechnique_RequiresPrivilege_ReturnsTrueOnFailure(t *testing.T) {
	orig := probePrivilege
	defer func() { probePrivilege = orig }()

	probePrivilege = func() (bool, error) { return true, errors.New("boom") }
	tech := &technique{}
	if !tech.RequiresPrivilege() {
		t.Fatal("expected RequiresPrivilege to return true from mocked probe")
	}
}

func TestTechnique_ProbePrivilege_ReturnsUnderlyingError(t *testing.T) {
	orig := probePrivilege
	defer func() { probePrivilege = orig }()

	probeErr := errors.New("operation not permitted")
	probePrivilege = func() (bool, error) { return true, probeErr }

	tech := &technique{}
	requires, err := tech.ProbePrivilege()
	if !requires {
		t.Fatal("expected ProbePrivilege to report requiring privilege")
	}
	if err != probeErr {
		t.Fatalf("expected the underlying probe error to be returned, got %v", err)
	}
}

func TestOpenICMPConn_SucceedsOnUDP4WithoutTryingRaw(t *testing.T) {
	orig := listenICMP
	defer func() { listenICMP = orig }()

	var attempted []string
	listenICMP = func(network, address string) (*icmp.PacketConn, error) {
		attempted = append(attempted, network)
		return &icmp.PacketConn{}, nil
	}

	_, network, err := openICMPConn()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if network != "udp4" {
		t.Fatalf("expected network 'udp4', got %q", network)
	}
	if len(attempted) != 1 {
		t.Fatalf("expected only udp4 to be attempted, got %v", attempted)
	}
}

func TestOpenICMPConn_FallsBackToRawWhenUnprivilegedFails(t *testing.T) {
	orig := listenICMP
	defer func() { listenICMP = orig }()

	var attempted []string
	listenICMP = func(network, address string) (*icmp.PacketConn, error) {
		attempted = append(attempted, network)
		if network == "udp4" {
			return nil, errors.New("socket: permission denied")
		}
		return &icmp.PacketConn{}, nil
	}

	conn, network, err := openICMPConn()
	if err != nil {
		t.Fatalf("expected fallback to succeed, got error: %v", err)
	}
	if network != "ip4:icmp" {
		t.Fatalf("expected fallback network 'ip4:icmp', got %q", network)
	}
	if conn == nil {
		t.Fatal("expected non-nil conn from the raw fallback")
	}
	if len(attempted) != 2 || attempted[0] != "udp4" || attempted[1] != "ip4:icmp" {
		t.Fatalf("expected udp4 tried before ip4:icmp, got %v", attempted)
	}
}

func TestOpenICMPConn_CombinesBothErrorsWhenAllFail(t *testing.T) {
	orig := listenICMP
	defer func() { listenICMP = orig }()

	listenICMP = func(network, address string) (*icmp.PacketConn, error) {
		if network == "udp4" {
			return nil, errors.New("permission denied")
		}
		return nil, errors.New("operation not permitted")
	}

	_, _, err := openICMPConn()
	if err == nil {
		t.Fatal("expected an error when every network fails")
	}
	if !strings.Contains(err.Error(), "udp4") || !strings.Contains(err.Error(), "permission denied") {
		t.Fatalf("expected the udp4 failure reason to be preserved, got: %v", err)
	}
	if !strings.Contains(err.Error(), "ip4:icmp") || !strings.Contains(err.Error(), "operation not permitted") {
		t.Fatalf("expected the ip4:icmp failure reason to be preserved, got: %v", err)
	}
}

func TestOpenICMPConn_EmptyNetworkListReturnsNonNilError(t *testing.T) {
	origNetworks := icmpNetworks
	defer func() { icmpNetworks = origNetworks }()

	icmpNetworks = nil

	conn, _, err := openICMPConn()
	if err == nil {
		t.Fatal("expected a non-nil error when icmpNetworks is empty, not a silent nil-conn success")
	}
	if conn != nil {
		t.Fatalf("expected nil conn alongside the error, got %v", conn)
	}
}

func TestProbePrivilege_SucceedsViaOpenICMPConn(t *testing.T) {
	orig := listenICMP
	defer func() { listenICMP = orig }()

	listenICMP = func(network, address string) (*icmp.PacketConn, error) {
		return &icmp.PacketConn{}, nil
	}

	requires, err := probePrivilege()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if requires {
		t.Fatal("expected probePrivilege to report no privilege needed when a listen succeeds")
	}
}

func TestProbePrivilege_ReturnsCombinedErrorWhenAllListensFail(t *testing.T) {
	orig := listenICMP
	defer func() { listenICMP = orig }()

	listenICMP = func(network, address string) (*icmp.PacketConn, error) {
		return nil, errors.New("permission denied")
	}

	requires, err := probePrivilege()
	if !requires {
		t.Fatal("expected probePrivilege to report privilege required when every listen fails")
	}
	if err == nil {
		t.Fatal("expected a non-nil underlying error")
	}
}

func TestDestAddr(t *testing.T) {
	ip := net.IPv4(192, 168, 1, 20)

	if _, ok := destAddr(false, ip).(*net.UDPAddr); !ok {
		t.Fatalf("expected destAddr(false, ...) to return *net.UDPAddr for the udp4 endpoint")
	}
	if _, ok := destAddr(true, ip).(*net.IPAddr); !ok {
		t.Fatalf("expected destAddr(true, ...) to return *net.IPAddr for the raw ip4:icmp endpoint")
	}
}

func TestPeerIPString(t *testing.T) {
	ip := net.IPv4(192, 168, 1, 20)

	if s, ok := peerIPString(&net.UDPAddr{IP: ip}); !ok || s != ip.String() {
		t.Fatalf("expected peerIPString(*net.UDPAddr) = (%q, true), got (%q, %v)", ip.String(), s, ok)
	}
	if s, ok := peerIPString(&net.IPAddr{IP: ip}); !ok || s != ip.String() {
		t.Fatalf("expected peerIPString(*net.IPAddr) = (%q, true), got (%q, %v)", ip.String(), s, ok)
	}
	if _, ok := peerIPString(&net.TCPAddr{IP: ip}); ok {
		t.Fatal("expected peerIPString to reject an unrecognized address type")
	}
}

func TestTechnique_SelfRegistration(t *testing.T) {
	tech, ok := engine.GetTechnique("icmp")
	if !ok {
		t.Fatal("expected icmp technique to be registered in engine registry")
	}
	if tech.Name() != "icmp" {
		t.Fatalf("expected registered technique Name() to return 'icmp', got %s", tech.Name())
	}
}

func TestTechnique_Run_InvalidTarget(t *testing.T) {
	_, err := (&technique{}).Run(context.Background(), "not-a-cidr")
	if err == nil {
		t.Fatal("expected error for invalid target")
	}
}

func TestTechnique_Run_RejectsIPv6Target(t *testing.T) {
	_, err := (&technique{}).Run(context.Background(), "fe80::/64")
	if err == nil {
		t.Fatal("expected error for IPv6 target")
	}
	if !strings.Contains(err.Error(), "only IPv4 targets supported") {
		t.Fatalf("expected IPv4-only error, got: %v", err)
	}
}

func TestTechnique_Run_RejectsOversizedSubnet(t *testing.T) {
	_, err := (&technique{}).Run(context.Background(), "10.0.0.0/8")
	if err == nil {
		t.Fatal("expected error for oversized subnet")
	}
	if !strings.Contains(err.Error(), "too large to scan") {
		t.Fatalf("expected too-large error, got: %v", err)
	}
}

func TestTechnique_EnumerateAddresses(t *testing.T) {
	addrs, err := (&technique{}).EnumerateAddresses("192.168.1.0/24")
	if err != nil {
		t.Fatalf("EnumerateAddresses returned unexpected error: %v", err)
	}
	// A /24 has 254 usable host addresses; FindLocalIP excludes at most one.
	if len(addrs) != 254 && len(addrs) != 253 {
		t.Fatalf("expected 253 or 254 target addresses, got %d", len(addrs))
	}
}

func TestTechnique_EnumerateAddresses_InvalidTarget(t *testing.T) {
	_, err := (&technique{}).EnumerateAddresses("not-a-cidr")
	if err == nil {
		t.Fatal("expected error for invalid target")
	}
}
