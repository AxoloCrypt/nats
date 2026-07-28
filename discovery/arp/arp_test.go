package arp

import (
	"context"
	"errors"
	"net"
	"strings"
	"testing"
	"time"

	"nats/core/engine"

	"github.com/gopacket/gopacket"
	"github.com/gopacket/gopacket/layers"
)

type mockPacketHandle struct {
	written [][]byte
	replies []gopacket.Packet
	readIdx int
}

func (m *mockPacketHandle) Close() {}

func (m *mockPacketHandle) WritePacketData(data []byte) error {
	m.written = append(m.written, data)
	return nil
}

func (m *mockPacketHandle) ReadPacketData() ([]byte, gopacket.CaptureInfo, error) {
	if m.readIdx >= len(m.replies) {
		return nil, gopacket.CaptureInfo{}, errors.New("no more packets")
	}
	p := m.replies[m.readIdx]
	m.readIdx++
	return p.Data(), p.Metadata().CaptureInfo, nil
}

func buildEthernetARPReply(srcMAC net.HardwareAddr, srcIP net.IP, dstMAC net.HardwareAddr, dstIP net.IP) gopacket.Packet {
	eth := &layers.Ethernet{
		SrcMAC:       srcMAC,
		DstMAC:       dstMAC,
		EthernetType: layers.EthernetTypeARP,
	}
	arp := &layers.ARP{
		AddrType:          layers.LinkTypeEthernet,
		Protocol:          0x0800,
		HwAddressSize:     6,
		ProtAddressSize:   4,
		Operation:         layers.ARPReply,
		SourceHwAddress:   srcMAC,
		SourceProtAddress: srcIP.To4(),
		DstHwAddress:      dstMAC,
		DstProtAddress:    dstIP.To4(),
	}
	buf := gopacket.NewSerializeBuffer()
	opts := gopacket.SerializeOptions{}
	gopacket.SerializeLayers(buf, opts, eth, arp)
	return gopacket.NewPacket(buf.Bytes(), layers.LayerTypeEthernet, gopacket.NoCopy)
}

func TestTechnique_Name(t *testing.T) {
	tech := &technique{}
	if tech.Name() != "arp" {
		t.Fatalf("expected 'arp', got %s", tech.Name())
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

func TestTechnique_ProbePrivilege_ReturnsUnderlyingError(t *testing.T) {
	orig := probePrivilege
	defer func() { probePrivilege = orig }()

	probeErr := errors.New("no such device exists")
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

func ipv4Addrs(ip string) []net.Addr {
	return []net.Addr{&net.IPNet{IP: net.ParseIP(ip), Mask: net.CIDRMask(24, 32)}}
}

func TestProbePrivilege_SkipsDownAndLoopbackAndAddresslessInterfaces(t *testing.T) {
	origIfaces := netInterfaces
	origAddrs := ifaceAddrs
	origOpenPcap := openPcap
	defer func() {
		netInterfaces = origIfaces
		ifaceAddrs = origAddrs
		openPcap = origOpenPcap
	}()

	upAndRunning := net.FlagUp | net.FlagRunning

	netInterfaces = func() ([]net.Interface, error) {
		return []net.Interface{
			{Name: "lo", Flags: upAndRunning | net.FlagLoopback},
			{Name: "eth-down", Flags: 0},
			{Name: "eth-no-carrier", Flags: net.FlagUp}, // up but not running
			{Name: "eth-no-addr", Flags: upAndRunning},  // up+running but no IPv4 addr
			{Name: "eth0", Flags: upAndRunning},
			{Name: "eth1", Flags: upAndRunning},
		}, nil
	}

	ifaceAddrs = func(iface net.Interface) ([]net.Addr, error) {
		switch iface.Name {
		case "lo":
			return []net.Addr{&net.IPNet{IP: net.ParseIP("127.0.0.1"), Mask: net.CIDRMask(8, 32)}}, nil
		case "eth-no-addr":
			return nil, nil
		case "eth0", "eth1":
			return ipv4Addrs("192.168.1.10"), nil
		default:
			return nil, nil
		}
	}

	var openedDevice string
	openPcap = func(device string, snaplen int32, promisc bool, timeout time.Duration) (packetHandle, error) {
		openedDevice = device
		return &mockPacketHandle{}, nil
	}

	requires, err := probePrivilege()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if requires {
		t.Fatal("expected probePrivilege to report no privilege needed when open succeeds")
	}
	if openedDevice != "eth0" {
		t.Fatalf("expected probe to open the first up+running interface with an IPv4 address ('eth0'), got %q", openedDevice)
	}
}

func TestProbePrivilege_NoUsableInterfaceReturnsDescriptiveError(t *testing.T) {
	origIfaces := netInterfaces
	origAddrs := ifaceAddrs
	defer func() {
		netInterfaces = origIfaces
		ifaceAddrs = origAddrs
	}()

	netInterfaces = func() ([]net.Interface, error) {
		return []net.Interface{
			{Name: "lo", Flags: net.FlagUp | net.FlagRunning | net.FlagLoopback},
			{Name: "eth-down", Flags: 0},
		}, nil
	}
	ifaceAddrs = func(iface net.Interface) ([]net.Addr, error) { return nil, nil }

	requires, err := probePrivilege()
	if !requires {
		t.Fatal("expected probePrivilege to report privilege required when no usable interface exists")
	}
	if err == nil || !strings.Contains(err.Error(), "no active network interface with an IPv4 address found") {
		t.Fatalf("expected descriptive no-interface error, got %v", err)
	}
}

func TestProbePrivilege_NoInterfacesAtAllReturnsDescriptiveError(t *testing.T) {
	origIfaces := netInterfaces
	defer func() { netInterfaces = origIfaces }()

	netInterfaces = func() ([]net.Interface, error) { return nil, nil }

	requires, err := probePrivilege()
	if !requires {
		t.Fatal("expected probePrivilege to report privilege required when there are no interfaces at all")
	}
	if err == nil || !strings.Contains(err.Error(), "no active network interface with an IPv4 address found") {
		t.Fatalf("expected descriptive no-interface error, got %v", err)
	}
}

func TestProbePrivilege_PropagatesOpenError(t *testing.T) {
	origIfaces := netInterfaces
	origAddrs := ifaceAddrs
	origOpenPcap := openPcap
	defer func() {
		netInterfaces = origIfaces
		ifaceAddrs = origAddrs
		openPcap = origOpenPcap
	}()

	netInterfaces = func() ([]net.Interface, error) {
		return []net.Interface{{Name: "eth0", Flags: net.FlagUp | net.FlagRunning}}, nil
	}
	ifaceAddrs = func(iface net.Interface) ([]net.Addr, error) { return ipv4Addrs("192.168.1.10"), nil }

	openErr := errors.New("you don't have permission to perform this capture")
	openPcap = func(device string, snaplen int32, promisc bool, timeout time.Duration) (packetHandle, error) {
		return nil, openErr
	}

	requires, err := probePrivilege()
	if !requires {
		t.Fatal("expected probePrivilege to report privilege required on open failure")
	}
	if err != openErr {
		t.Fatalf("expected the underlying open error to be returned, got %v", err)
	}
}

func TestTechnique_SelfRegistration(t *testing.T) {
	tech, ok := engine.GetTechnique("arp")
	if !ok {
		t.Fatal("expected arp technique to be registered in engine registry")
	}
	if tech.Name() != "arp" {
		t.Fatalf("expected registered technique Name() to return 'arp', got %s", tech.Name())
	}
}

func TestTechnique_Run_EmitsSightingsForARPReplies(t *testing.T) {
	origResolve := resolveInterface
	origPcap := openPcap
	defer func() {
		resolveInterface = origResolve
		openPcap = origPcap
	}()

	localMAC := net.HardwareAddr{0x00, 0x11, 0x22, 0x33, 0x44, 0x55}
	localIP := net.IPv4(192, 168, 1, 10)

	resolveInterface = func(ipnet *net.IPNet) (*net.Interface, net.IP, error) {
		return &net.Interface{
			Index:        1,
			Name:         "eth0",
			Flags:        net.FlagUp,
			HardwareAddr: localMAC,
		}, localIP, nil
	}

	targetMAC := net.HardwareAddr{0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0xff}
	targetIP := net.IPv4(192, 168, 1, 20)

	replyPkt := buildEthernetARPReply(targetMAC, targetIP, localMAC, localIP)

	mockHandle := &mockPacketHandle{
		replies: []gopacket.Packet{replyPkt},
	}

	openPcap = func(device string, snaplen int32, promisc bool, timeout time.Duration) (packetHandle, error) {
		return mockHandle, nil
	}

	ctx := context.Background()
	sightingsCh, err := (&technique{}).Run(ctx, "192.168.1.0/24")
	if err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}

	var sightings []engine.Sighting
	for s := range sightingsCh {
		sightings = append(sightings, s)
	}

	if len(sightings) != 1 {
		t.Fatalf("expected 1 sighting, got %d", len(sightings))
	}
	if sightings[0].IP != "192.168.1.20" {
		t.Fatalf("expected IP 192.168.1.20, got %s", sightings[0].IP)
	}
	if sightings[0].MAC != "aa:bb:cc:dd:ee:ff" {
		t.Fatalf("expected MAC aa:bb:cc:dd:ee:ff, got %s", sightings[0].MAC)
	}
	if sightings[0].Technique != "arp" {
		t.Fatalf("expected Technique 'arp', got %s", sightings[0].Technique)
	}
}

func TestTechnique_Run_NoDuplicates(t *testing.T) {
	origResolve := resolveInterface
	origPcap := openPcap
	defer func() {
		resolveInterface = origResolve
		openPcap = origPcap
	}()

	localMAC := net.HardwareAddr{0x00, 0x11, 0x22, 0x33, 0x44, 0x55}
	localIP := net.IPv4(192, 168, 1, 10)

	resolveInterface = func(ipnet *net.IPNet) (*net.Interface, net.IP, error) {
		return &net.Interface{
			Index:        1,
			Name:         "eth0",
			Flags:        net.FlagUp,
			HardwareAddr: localMAC,
		}, localIP, nil
	}

	targetMAC := net.HardwareAddr{0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0xff}
	targetIP := net.IPv4(192, 168, 1, 20)

	replyPkt := buildEthernetARPReply(targetMAC, targetIP, localMAC, localIP)

	mockHandle := &mockPacketHandle{
		replies: []gopacket.Packet{replyPkt, replyPkt},
	}

	openPcap = func(device string, snaplen int32, promisc bool, timeout time.Duration) (packetHandle, error) {
		return mockHandle, nil
	}

	ctx := context.Background()
	sightingsCh, err := (&technique{}).Run(ctx, "192.168.1.0/24")
	if err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}

	var sightings []engine.Sighting
	for s := range sightingsCh {
		sightings = append(sightings, s)
	}

	if len(sightings) != 1 {
		t.Fatalf("expected 1 sighting (deduplicated), got %d", len(sightings))
	}
}

func TestTechnique_Run_ExcludesOwnIP(t *testing.T) {
	origResolve := resolveInterface
	origPcap := openPcap
	defer func() {
		resolveInterface = origResolve
		openPcap = origPcap
	}()

	localMAC := net.HardwareAddr{0x00, 0x11, 0x22, 0x33, 0x44, 0x55}
	localIP := net.IPv4(192, 168, 1, 10)

	resolveInterface = func(ipnet *net.IPNet) (*net.Interface, net.IP, error) {
		return &net.Interface{
			Index:        1,
			Name:         "eth0",
			Flags:        net.FlagUp,
			HardwareAddr: localMAC,
		}, localIP, nil
	}

	mockHandle := &mockPacketHandle{}

	openPcap = func(device string, snaplen int32, promisc bool, timeout time.Duration) (packetHandle, error) {
		return mockHandle, nil
	}

	sightingsCh, err := (&technique{}).Run(context.Background(), "192.168.1.0/24")
	if err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}

	for range sightingsCh {
	}

	for _, data := range mockHandle.written {
		packet := gopacket.NewPacket(data, layers.LayerTypeEthernet, gopacket.NoCopy)
		arpLayer := packet.Layer(layers.LayerTypeARP)
		if arpLayer == nil {
			continue
		}
		arp, ok := arpLayer.(*layers.ARP)
		if !ok {
			continue
		}
		dstIP := net.IP(arp.DstProtAddress).String()
		if dstIP == "192.168.1.10" {
			t.Fatal("should not send ARP request to own IP")
		}
	}
}

func TestTechnique_Run_InvalidTarget(t *testing.T) {
	_, err := (&technique{}).Run(context.Background(), "not-a-cidr")
	if err == nil {
		t.Fatal("expected error for invalid target")
	}
}

func TestTechnique_Run_InterfaceResolutionFails(t *testing.T) {
	origResolve := resolveInterface
	defer func() { resolveInterface = origResolve }()

	resolveInterface = func(ipnet *net.IPNet) (*net.Interface, net.IP, error) {
		return nil, nil, errors.New("no interface found")
	}

	_, err := (&technique{}).Run(context.Background(), "10.0.0.0/24")
	if err == nil {
		t.Fatal("expected error when no interface matches subnet")
	}
}

func TestTechnique_EnumerateAddresses(t *testing.T) {
	origResolve := resolveInterface
	defer func() { resolveInterface = origResolve }()

	localMAC := net.HardwareAddr{0x00, 0x11, 0x22, 0x33, 0x44, 0x55}
	localIP := net.IPv4(192, 168, 1, 10)

	resolveInterface = func(ipnet *net.IPNet) (*net.Interface, net.IP, error) {
		return &net.Interface{
			Index:        1,
			Name:         "eth0",
			Flags:        net.FlagUp,
			HardwareAddr: localMAC,
		}, localIP, nil
	}

	addrs, err := (&technique{}).EnumerateAddresses("192.168.1.0/24")
	if err != nil {
		t.Fatalf("EnumerateAddresses returned unexpected error: %v", err)
	}

	// A /24 has 254 usable host addresses; the local IP is excluded.
	if len(addrs) != 253 {
		t.Fatalf("expected 253 target addresses, got %d", len(addrs))
	}
	for _, a := range addrs {
		if a == localIP.String() {
			t.Fatalf("expected local IP %s to be excluded from targets", localIP)
		}
	}
}

func TestTechnique_EnumerateAddresses_InvalidTarget(t *testing.T) {
	_, err := (&technique{}).EnumerateAddresses("not-a-cidr")
	if err == nil {
		t.Fatal("expected error for invalid target")
	}
}

func TestTechnique_EnumerateAddresses_InterfaceResolutionFails(t *testing.T) {
	origResolve := resolveInterface
	defer func() { resolveInterface = origResolve }()

	resolveInterface = func(ipnet *net.IPNet) (*net.Interface, net.IP, error) {
		return nil, nil, errors.New("no interface found")
	}

	_, err := (&technique{}).EnumerateAddresses("10.0.0.0/24")
	if err == nil {
		t.Fatal("expected error when no interface matches subnet")
	}
}
