package subnetutil

import (
	"net"
	"testing"
)

func TestEnumerateTargets(t *testing.T) {
	_, ipnet, _ := net.ParseCIDR("192.168.1.0/24")
	exclude := net.IPv4(192, 168, 1, 10)
	targets := EnumerateTargets(ipnet, exclude)

	if len(targets) != 253 {
		t.Fatalf("expected 253 targets for /24 (excluding own IP), got %d", len(targets))
	}

	if !targets[0].Equal(net.IPv4(192, 168, 1, 1)) {
		t.Fatalf("expected first target 192.168.1.1, got %s", targets[0].String())
	}

	if !targets[len(targets)-1].Equal(net.IPv4(192, 168, 1, 254)) {
		t.Fatalf("expected last target 192.168.1.254, got %s", targets[len(targets)-1].String())
	}

	for _, ip := range targets {
		if ip.Equal(net.IPv4(192, 168, 1, 10)) {
			t.Fatal("targets should not include excluded IP 192.168.1.10")
		}
	}
}

func TestEnumerateTargets_WithoutExcludeReturns254(t *testing.T) {
	_, ipnet, _ := net.ParseCIDR("10.0.0.0/24")
	targets := EnumerateTargets(ipnet, nil)
	if len(targets) != 254 {
		t.Fatalf("expected 254 targets for /24, got %d", len(targets))
	}
}

func TestEnumerateTargets_ExcludesNetworkAndBroadcast(t *testing.T) {
	_, ipnet, _ := net.ParseCIDR("10.0.0.0/24")
	targets := EnumerateTargets(ipnet, nil)

	for _, ip := range targets {
		if ip.Equal(net.IPv4(10, 0, 0, 0)) {
			t.Fatal("targets should not include network address")
		}
		if ip.Equal(net.IPv4(10, 0, 0, 255)) {
			t.Fatal("targets should not include broadcast address")
		}
	}
}

func TestResolveInterface_FindsMatchingInterface(t *testing.T) {
	_, ipnet, _ := net.ParseCIDR("192.168.1.0/24")
	mask := net.IPv4Mask(255, 255, 255, 0)
	ifaces := []net.Interface{
		{Index: 1, Name: "eth0", Flags: net.FlagUp},
	}
	getAddrs := func(iface net.Interface) ([]net.Addr, error) {
		return []net.Addr{&net.IPNet{IP: net.IPv4(192, 168, 1, 10), Mask: mask}}, nil
	}

	iface, ip, err := resolveInterface(ipnet, ifaces, getAddrs)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if iface.Name != "eth0" {
		t.Fatalf("expected eth0, got %s", iface.Name)
	}
	if !ip.Equal(net.IPv4(192, 168, 1, 10)) {
		t.Fatalf("expected 192.168.1.10, got %s", ip)
	}
}

func TestResolveInterface_SkipsDownInterfaces(t *testing.T) {
	_, ipnet, _ := net.ParseCIDR("192.168.1.0/24")
	mask := net.IPv4Mask(255, 255, 255, 0)
	ifaces := []net.Interface{
		{Index: 1, Name: "eth0", Flags: 0},
	}
	getAddrs := func(iface net.Interface) ([]net.Addr, error) {
		return []net.Addr{&net.IPNet{IP: net.IPv4(192, 168, 1, 10), Mask: mask}}, nil
	}

	_, _, err := resolveInterface(ipnet, ifaces, getAddrs)
	if err == nil {
		t.Fatal("expected error when interface is down")
	}
}

func TestResolveInterface_NoMatch(t *testing.T) {
	_, ipnet, _ := net.ParseCIDR("10.99.99.0/24")
	_, _, err := resolveInterface(ipnet, nil, nil)
	if err == nil {
		t.Fatal("expected error when no interface matches")
	}
}

func TestFindLocalIP_ReturnsNilOnNoMatch(t *testing.T) {
	_, ipnet, _ := net.ParseCIDR("10.99.99.0/24")
	ip := FindLocalIP(ipnet)
	if ip != nil {
		t.Fatalf("expected nil for non-matching subnet, got %s", ip)
	}
}
