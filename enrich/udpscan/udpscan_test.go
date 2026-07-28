package udpscan

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"nats/core/engine"
	"nats/enrich/internal/rawcapture"

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

func buildUDPReply(srcIP, dstIP net.IP, srcPort, dstPort layers.UDPPort) gopacket.Packet {
	eth := &layers.Ethernet{
		SrcMAC:       net.HardwareAddr{0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0xff},
		DstMAC:       net.HardwareAddr{0x00, 0x11, 0x22, 0x33, 0x44, 0x55},
		EthernetType: layers.EthernetTypeIPv4,
	}
	ip := &layers.IPv4{Version: 4, TTL: 64, Protocol: layers.IPProtocolUDP, SrcIP: srcIP, DstIP: dstIP}
	udp := &layers.UDP{SrcPort: srcPort, DstPort: dstPort}
	udp.SetNetworkLayerForChecksum(ip)

	buf := gopacket.NewSerializeBuffer()
	opts := gopacket.SerializeOptions{ComputeChecksums: true, FixLengths: true}
	gopacket.SerializeLayers(buf, opts, eth, ip, udp, gopacket.Payload{1, 2, 3})
	return gopacket.NewPacket(buf.Bytes(), layers.LayerTypeEthernet, gopacket.NoCopy)
}

// buildPortUnreachable crafts an ICMPv4 "destination unreachable, port
// unreachable" reply embedding a copy of the original probe's IP+UDP
// headers, the same shape a real device/router would send back for a
// closed UDP port.
func buildPortUnreachable(icmpSrcIP, icmpDstIP net.IP, origSrcIP, origDstIP net.IP, origSrcPort, origDstPort layers.UDPPort) gopacket.Packet {
	innerIP := &layers.IPv4{Version: 4, TTL: 64, Protocol: layers.IPProtocolUDP, SrcIP: origSrcIP, DstIP: origDstIP}
	innerUDP := &layers.UDP{SrcPort: origSrcPort, DstPort: origDstPort}
	innerUDP.SetNetworkLayerForChecksum(innerIP)

	innerBuf := gopacket.NewSerializeBuffer()
	gopacket.SerializeLayers(innerBuf, gopacket.SerializeOptions{ComputeChecksums: true, FixLengths: true}, innerIP, innerUDP)

	eth := &layers.Ethernet{
		SrcMAC:       net.HardwareAddr{0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0xff},
		DstMAC:       net.HardwareAddr{0x00, 0x11, 0x22, 0x33, 0x44, 0x55},
		EthernetType: layers.EthernetTypeIPv4,
	}
	outerIP := &layers.IPv4{Version: 4, TTL: 64, Protocol: layers.IPProtocolICMPv4, SrcIP: icmpSrcIP, DstIP: icmpDstIP}
	icmp := &layers.ICMPv4{TypeCode: layers.CreateICMPv4TypeCode(layers.ICMPv4TypeDestinationUnreachable, layers.ICMPv4CodePort)}

	buf := gopacket.NewSerializeBuffer()
	opts := gopacket.SerializeOptions{ComputeChecksums: true, FixLengths: true}
	gopacket.SerializeLayers(buf, opts, eth, outerIP, icmp, gopacket.Payload(innerBuf.Bytes()))
	return gopacket.NewPacket(buf.Bytes(), layers.LayerTypeEthernet, gopacket.NoCopy)
}

func TestEnricher_Name(t *testing.T) {
	e := &enricher{}
	if e.Name() != "udpscan" {
		t.Fatalf("expected 'udpscan', got %s", e.Name())
	}
}

func TestEnricher_RequiresPrivilege(t *testing.T) {
	orig := probePrivilege
	defer func() { probePrivilege = orig }()

	probePrivilege = func() (bool, error) { return true, nil }
	e := &enricher{}
	if !e.RequiresPrivilege() {
		t.Fatal("expected RequiresPrivilege true from mocked probe")
	}
}

func TestEnricher_ProbePrivilege_ReturnsUnderlyingError(t *testing.T) {
	orig := probePrivilege
	defer func() { probePrivilege = orig }()

	probeErr := errors.New("permission denied")
	probePrivilege = func() (bool, error) { return true, probeErr }

	e := &enricher{}
	requires, err := e.ProbePrivilege()
	if !requires || err != probeErr {
		t.Fatalf("expected (true, %v), got (%v, %v)", probeErr, requires, err)
	}
}

func TestEnricher_SelfRegistration(t *testing.T) {
	if _, ok := engine.GetEnricher("udpscan"); !ok {
		t.Fatal("expected udpscan to self-register via init()")
	}
}

func TestEnrich_NoIPReturnsUnchanged(t *testing.T) {
	e := &enricher{}
	device := engine.Device{MAC: "aa:bb:cc:dd:ee:ff"}
	result, err := e.Enrich(context.Background(), device)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.OpenPorts) != 0 {
		t.Fatalf("expected no open ports, got %v", result.OpenPorts)
	}
}

func TestEnrich_NoMACReturnsUnchanged(t *testing.T) {
	e := &enricher{}
	device := engine.Device{IP: "192.168.1.20"}
	result, err := e.Enrich(context.Background(), device)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.OpenPorts) != 0 {
		t.Fatalf("expected no open ports, got %v", result.OpenPorts)
	}
}

func TestEnrich_InterfaceResolutionFailsReturnsUnchangedNoError(t *testing.T) {
	origResolve := resolveInterfaceForIP
	defer func() { resolveInterfaceForIP = origResolve }()

	resolveInterfaceForIP = func(ip net.IP) (*net.Interface, net.IP, error) {
		return nil, nil, errors.New("no route")
	}

	e := &enricher{}
	device := engine.Device{IP: "192.168.1.20", MAC: "aa:bb:cc:dd:ee:ff"}
	result, err := e.Enrich(context.Background(), device)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(result.OpenPorts) != 0 {
		t.Fatalf("expected no open ports, got %v", result.OpenPorts)
	}
}

func TestEnrich_OpenHandleErrorPropagates(t *testing.T) {
	origResolve := resolveInterfaceForIP
	origOpen := openHandle
	defer func() {
		resolveInterfaceForIP = origResolve
		openHandle = origOpen
	}()

	resolveInterfaceForIP = func(ip net.IP) (*net.Interface, net.IP, error) {
		return &net.Interface{Name: "eth0", HardwareAddr: net.HardwareAddr{0, 1, 2, 3, 4, 5}}, net.IPv4(192, 168, 1, 10), nil
	}
	openErr := errors.New("no such device")
	openHandle = func(device string, snaplen int32, promisc bool, timeout time.Duration) (rawcapture.PacketHandle, error) {
		return nil, openErr
	}

	e := &enricher{}
	device := engine.Device{IP: "192.168.1.20", MAC: "aa:bb:cc:dd:ee:ff"}
	_, err := e.Enrich(context.Background(), device)
	if err != openErr {
		t.Fatalf("expected underlying open error, got %v", err)
	}
}

func TestEnrich_DirectReplyOpen_ICMPUnreachableClosed_RestAreOpenFiltered(t *testing.T) {
	origResolve := resolveInterfaceForIP
	origOpen := openHandle
	origWindow := responseWindow
	defer func() {
		resolveInterfaceForIP = origResolve
		openHandle = origOpen
		responseWindow = origWindow
	}()

	responseWindow = 50 * time.Millisecond
	localIP := net.IPv4(192, 168, 1, 10)
	deviceIP := net.IPv4(192, 168, 1, 20)

	resolveInterfaceForIP = func(ip net.IP) (*net.Interface, net.IP, error) {
		return &net.Interface{Name: "eth0", HardwareAddr: net.HardwareAddr{0, 1, 2, 3, 4, 5}}, localIP, nil
	}

	openPort := scanPorts[0]
	closedPort := scanPorts[1]

	openReply := buildUDPReply(deviceIP, localIP, layers.UDPPort(openPort), scanSourcePort)
	closedReply := buildPortUnreachable(deviceIP, localIP, localIP, deviceIP, scanSourcePort, layers.UDPPort(closedPort))

	mock := &mockPacketHandle{replies: []gopacket.Packet{openReply, closedReply}}
	openHandle = func(device string, snaplen int32, promisc bool, timeout time.Duration) (rawcapture.PacketHandle, error) {
		return mock, nil
	}

	e := &enricher{}
	device := engine.Device{IP: "192.168.1.20", MAC: "aa:bb:cc:dd:ee:ff"}
	result, err := e.Enrich(context.Background(), device)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(mock.written) != len(scanPorts) {
		t.Fatalf("expected %d UDP probes written, got %d", len(scanPorts), len(mock.written))
	}
	if len(result.OpenPorts) != len(scanPorts)-1 {
		t.Fatalf("expected %d recorded ports (all but the closed one), got %d: %+v", len(scanPorts)-1, len(result.OpenPorts), result.OpenPorts)
	}

	byPort := make(map[int]engine.OpenPort)
	for _, op := range result.OpenPorts {
		byPort[op.Port] = op
	}

	if got, ok := byPort[openPort]; !ok || got.State != "open" || got.Protocol != "udp" {
		t.Fatalf("expected port %d recorded udp/open, got %+v (ok=%v)", openPort, got, ok)
	}
	if _, ok := byPort[closedPort]; ok {
		t.Fatalf("expected closed port %d not recorded at all", closedPort)
	}
	for _, port := range scanPorts[2:] {
		got, ok := byPort[port]
		if !ok || got.State != "open|filtered" || got.Protocol != "udp" {
			t.Fatalf("expected port %d recorded udp/open|filtered, got %+v (ok=%v)", port, got, ok)
		}
	}
}

func TestEnrich_UpsertPreservesBannerSetByEarlierEnricher(t *testing.T) {
	origResolve := resolveInterfaceForIP
	origOpen := openHandle
	origWindow := responseWindow
	defer func() {
		resolveInterfaceForIP = origResolve
		openHandle = origOpen
		responseWindow = origWindow
	}()

	responseWindow = 50 * time.Millisecond
	localIP := net.IPv4(192, 168, 1, 10)
	deviceIP := net.IPv4(192, 168, 1, 20)

	resolveInterfaceForIP = func(ip net.IP) (*net.Interface, net.IP, error) {
		return &net.Interface{Name: "eth0", HardwareAddr: net.HardwareAddr{0, 1, 2, 3, 4, 5}}, localIP, nil
	}

	openPort := scanPorts[0]
	openReply := buildUDPReply(deviceIP, localIP, layers.UDPPort(openPort), scanSourcePort)
	mock := &mockPacketHandle{replies: []gopacket.Packet{openReply}}
	openHandle = func(device string, snaplen int32, promisc bool, timeout time.Duration) (rawcapture.PacketHandle, error) {
		return mock, nil
	}

	e := &enricher{}
	device := engine.Device{
		IP:  "192.168.1.20",
		MAC: "aa:bb:cc:dd:ee:ff",
		OpenPorts: []engine.OpenPort{
			{Port: openPort, Protocol: "udp", State: "open|filtered", Banner: "pre-existing"},
		},
	}
	result, err := e.Enrich(context.Background(), device)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, op := range result.OpenPorts {
		if op.Port != openPort || op.Protocol != "udp" {
			continue
		}
		if op.Banner != "pre-existing" {
			t.Fatalf("expected re-confirming port %d via UDP scan to preserve the existing Banner, got %+v", openPort, op)
		}
		return
	}
	t.Fatalf("expected port %d/udp to be present in result, got %+v", openPort, result.OpenPorts)
}
