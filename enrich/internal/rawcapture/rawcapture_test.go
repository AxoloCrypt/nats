package rawcapture

import (
	"errors"
	"net"
	"testing"
	"time"

	"github.com/gopacket/gopacket"
)

type stubHandle struct{}

func (stubHandle) Close() {}
func (stubHandle) WritePacketData([]byte) error {
	return nil
}
func (stubHandle) ReadPacketData() ([]byte, gopacket.CaptureInfo, error) {
	return nil, gopacket.CaptureInfo{}, errors.New("no packets")
}

func ipv4Addrs(ip string) []net.Addr {
	return []net.Addr{&net.IPNet{IP: net.ParseIP(ip), Mask: net.CIDRMask(24, 32)}}
}

func TestResolveInterfaceForIP_FindsContainingSubnet(t *testing.T) {
	origIfaces := netInterfaces
	origAddrs := ifaceAddrs
	defer func() {
		netInterfaces = origIfaces
		ifaceAddrs = origAddrs
	}()

	netInterfaces = func() ([]net.Interface, error) {
		return []net.Interface{
			{Name: "lo", Flags: upAndRunning | net.FlagLoopback},
			{Name: "eth0", Flags: upAndRunning, HardwareAddr: net.HardwareAddr{0, 1, 2, 3, 4, 5}},
		}, nil
	}
	ifaceAddrs = func(iface net.Interface) ([]net.Addr, error) {
		if iface.Name == "eth0" {
			return ipv4Addrs("192.168.1.10"), nil
		}
		return ipv4Addrs("127.0.0.1"), nil
	}

	iface, localIP, err := ResolveInterfaceForIP(net.ParseIP("192.168.1.20"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if iface.Name != "eth0" {
		t.Fatalf("expected eth0, got %s", iface.Name)
	}
	if !localIP.Equal(net.ParseIP("192.168.1.10")) {
		t.Fatalf("expected 192.168.1.10, got %s", localIP)
	}
}

func TestResolveInterfaceForIP_NoMatchReturnsError(t *testing.T) {
	origIfaces := netInterfaces
	origAddrs := ifaceAddrs
	defer func() {
		netInterfaces = origIfaces
		ifaceAddrs = origAddrs
	}()

	netInterfaces = func() ([]net.Interface, error) {
		return []net.Interface{{Name: "eth0", Flags: upAndRunning}}, nil
	}
	ifaceAddrs = func(iface net.Interface) ([]net.Addr, error) {
		return ipv4Addrs("192.168.1.10"), nil
	}

	_, _, err := ResolveInterfaceForIP(net.ParseIP("10.0.0.5"))
	if err == nil {
		t.Fatal("expected error when no interface routes to the target IP")
	}
}

func TestResolveInterfaceForIP_SkipsDownAndLoopback(t *testing.T) {
	origIfaces := netInterfaces
	origAddrs := ifaceAddrs
	defer func() {
		netInterfaces = origIfaces
		ifaceAddrs = origAddrs
	}()

	netInterfaces = func() ([]net.Interface, error) {
		return []net.Interface{
			{Name: "lo", Flags: upAndRunning | net.FlagLoopback},
			{Name: "eth-down", Flags: 0},
		}, nil
	}
	ifaceAddrs = func(iface net.Interface) ([]net.Addr, error) {
		return ipv4Addrs("192.168.1.10"), nil
	}

	_, _, err := ResolveInterfaceForIP(net.ParseIP("192.168.1.20"))
	if err == nil {
		t.Fatal("expected error: only loopback/down interfaces available")
	}
}

func TestResolveInterfaceForIP_InterfaceListError(t *testing.T) {
	origIfaces := netInterfaces
	defer func() { netInterfaces = origIfaces }()

	listErr := errors.New("cannot list interfaces")
	netInterfaces = func() ([]net.Interface, error) { return nil, listErr }

	_, _, err := ResolveInterfaceForIP(net.ParseIP("192.168.1.20"))
	if err != listErr {
		t.Fatalf("expected underlying list error, got %v", err)
	}
}

func TestProbeCapture_NoUsableInterfaceReturnsError(t *testing.T) {
	origIfaces := netInterfaces
	origAddrs := ifaceAddrs
	defer func() {
		netInterfaces = origIfaces
		ifaceAddrs = origAddrs
	}()

	netInterfaces = func() ([]net.Interface, error) {
		return []net.Interface{{Name: "lo", Flags: upAndRunning | net.FlagLoopback}}, nil
	}
	ifaceAddrs = func(iface net.Interface) ([]net.Addr, error) {
		return ipv4Addrs("127.0.0.1"), nil
	}

	requires, err := ProbeCapture()
	if !requires {
		t.Fatal("expected ProbeCapture to report requiring privilege with no usable interface")
	}
	if err == nil {
		t.Fatal("expected a descriptive error")
	}
}

func TestProbeCapture_OpensAndClosesHandleOnSuccess(t *testing.T) {
	origIfaces := netInterfaces
	origAddrs := ifaceAddrs
	origOpen := OpenLive
	defer func() {
		netInterfaces = origIfaces
		ifaceAddrs = origAddrs
		OpenLive = origOpen
	}()

	netInterfaces = func() ([]net.Interface, error) {
		return []net.Interface{{Name: "eth0", Flags: upAndRunning}}, nil
	}
	ifaceAddrs = func(iface net.Interface) ([]net.Addr, error) {
		return ipv4Addrs("192.168.1.10"), nil
	}
	OpenLive = func(device string, snaplen int32, promisc bool, timeout time.Duration) (PacketHandle, error) {
		if device != "eth0" {
			t.Fatalf("expected eth0, got %s", device)
		}
		return stubHandle{}, nil
	}

	requires, err := ProbeCapture()
	if requires {
		t.Fatalf("expected ProbeCapture to succeed, got error: %v", err)
	}
}

func TestProbeCapture_PropagatesOpenError(t *testing.T) {
	origIfaces := netInterfaces
	origAddrs := ifaceAddrs
	origOpen := OpenLive
	defer func() {
		netInterfaces = origIfaces
		ifaceAddrs = origAddrs
		OpenLive = origOpen
	}()

	netInterfaces = func() ([]net.Interface, error) {
		return []net.Interface{{Name: "eth0", Flags: upAndRunning}}, nil
	}
	ifaceAddrs = func(iface net.Interface) ([]net.Addr, error) {
		return ipv4Addrs("192.168.1.10"), nil
	}
	openErr := errors.New("permission denied")
	OpenLive = func(device string, snaplen int32, promisc bool, timeout time.Duration) (PacketHandle, error) {
		return nil, openErr
	}

	requires, err := ProbeCapture()
	if !requires {
		t.Fatal("expected ProbeCapture to report requiring privilege on open failure")
	}
	if err != openErr {
		t.Fatalf("expected underlying open error, got %v", err)
	}
}
