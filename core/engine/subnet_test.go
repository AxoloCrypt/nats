package engine

import (
	"net"
	"testing"
)

func TestFindSubnet_WithExplicitSubnet(t *testing.T) {
	subnet, diag := resolveSubnet(Options{Subnet: "10.0.0.0/8"})
	if diag != nil {
		t.Fatalf("expected no diagnostic, got %v", diag)
	}
	if subnet != "10.0.0.0/8" {
		t.Fatalf("expected subnet 10.0.0.0/8, got %s", subnet)
	}
}

func TestFindSubnet_FindsActiveIPv4Interface(t *testing.T) {
	mask := net.IPv4Mask(255, 255, 255, 0)
	ifaces := []net.Interface{
		{Index: 1, Name: "eth0", Flags: net.FlagUp},
	}
	addrs := func(iface net.Interface) ([]net.Addr, error) {
		return []net.Addr{
			&net.IPNet{IP: net.IPv4(192, 168, 1, 1), Mask: mask},
		}, nil
	}

	subnet, diag := findSubnet(ifaces, addrs)
	if diag != nil {
		t.Fatalf("expected no diagnostic, got %v", diag)
	}
	if subnet != "192.168.1.0/24" {
		t.Fatalf("expected 192.168.1.0/24, got %s", subnet)
	}
}

func TestFindSubnet_SkipsLoopback(t *testing.T) {
	ifaces := []net.Interface{
		{Index: 1, Name: "lo", Flags: net.FlagUp | net.FlagLoopback},
		{Index: 2, Name: "eth0", Flags: net.FlagUp},
	}
	addrs := func(iface net.Interface) ([]net.Addr, error) {
		if iface.Name == "lo" {
			return []net.Addr{
				&net.IPNet{IP: net.IPv4(127, 0, 0, 1), Mask: net.IPv4Mask(255, 0, 0, 0)},
			}, nil
		}
		return []net.Addr{
			&net.IPNet{IP: net.IPv4(10, 0, 0, 5), Mask: net.IPv4Mask(255, 0, 0, 0)},
		}, nil
	}

	subnet, diag := findSubnet(ifaces, addrs)
	if diag != nil {
		t.Fatalf("expected no diagnostic, got %v", diag)
	}
	if subnet != "10.0.0.0/8" {
		t.Fatalf("expected 10.0.0.0/8, got %s", subnet)
	}
}

func TestFindSubnet_SkipsIPv6(t *testing.T) {
	ifaces := []net.Interface{
		{Index: 1, Name: "eth0", Flags: net.FlagUp},
	}
	addrs := func(iface net.Interface) ([]net.Addr, error) {
		return []net.Addr{
			&net.IPNet{IP: net.ParseIP("2001:db8::1"), Mask: net.CIDRMask(64, 128)},
		}, nil
	}

	_, diag := findSubnet(ifaces, addrs)
	if diag == nil {
		t.Fatal("expected diagnostic for no active IPv4 interface")
	}
	if diag.Severity != "error" {
		t.Fatalf("expected severity error, got %s", diag.Severity)
	}
}

func TestFindSubnet_NoActiveInterface(t *testing.T) {
	_, diag := findSubnet(nil, nil)
	if diag == nil {
		t.Fatal("expected diagnostic when no interfaces exist")
	}
	if diag.Severity != "error" {
		t.Fatalf("expected severity error, got %s", diag.Severity)
	}
	if diag.Message != "no active network interface found" {
		t.Fatalf("unexpected message: %s", diag.Message)
	}
	if diag.Reason == "" {
		t.Fatal("expected a non-empty reason")
	}
}

func TestFindSubnet_SkipsDownInterfaces(t *testing.T) {
	ifaces := []net.Interface{
		{Index: 1, Name: "eth0", Flags: 0},
	}
	addrs := func(iface net.Interface) ([]net.Addr, error) {
		return []net.Addr{
			&net.IPNet{IP: net.IPv4(10, 0, 0, 5), Mask: net.IPv4Mask(255, 0, 0, 0)},
		}, nil
	}

	_, diag := findSubnet(ifaces, addrs)
	if diag == nil {
		t.Fatal("expected diagnostic when interface is down")
	}
}
