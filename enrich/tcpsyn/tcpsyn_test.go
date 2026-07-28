package tcpsyn

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

func buildTCPReply(srcIP, dstIP net.IP, srcPort, dstPort layers.TCPPort, syn, ack, rst bool) gopacket.Packet {
	eth := &layers.Ethernet{
		SrcMAC:       net.HardwareAddr{0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0xff},
		DstMAC:       net.HardwareAddr{0x00, 0x11, 0x22, 0x33, 0x44, 0x55},
		EthernetType: layers.EthernetTypeIPv4,
	}
	ip := &layers.IPv4{
		Version:  4,
		TTL:      64,
		Protocol: layers.IPProtocolTCP,
		SrcIP:    srcIP,
		DstIP:    dstIP,
	}
	tcp := &layers.TCP{
		SrcPort: srcPort,
		DstPort: dstPort,
		SYN:     syn,
		ACK:     ack,
		RST:     rst,
		Window:  65535,
	}
	tcp.SetNetworkLayerForChecksum(ip)

	buf := gopacket.NewSerializeBuffer()
	opts := gopacket.SerializeOptions{ComputeChecksums: true, FixLengths: true}
	gopacket.SerializeLayers(buf, opts, eth, ip, tcp)
	return gopacket.NewPacket(buf.Bytes(), layers.LayerTypeEthernet, gopacket.NoCopy)
}

func ipv4Addrs(ip string) []net.Addr {
	return []net.Addr{&net.IPNet{IP: net.ParseIP(ip), Mask: net.CIDRMask(24, 32)}}
}

func TestEnricher_Name(t *testing.T) {
	e := &enricher{}
	if e.Name() != "tcpsyn" {
		t.Fatalf("expected 'tcpsyn', got %s", e.Name())
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
	if _, ok := engine.GetEnricher("tcpsyn"); !ok {
		t.Fatal("expected tcpsyn to self-register via init()")
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

func TestEnrich_SYNACKRecordsOpenPort_RSTDoesNotRecord_NoReplyDoesNotRecord(t *testing.T) {
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

	openReply := buildTCPReply(deviceIP, localIP, 22, scanSourcePort, true, true, false)
	closedReply := buildTCPReply(deviceIP, localIP, 23, scanSourcePort, false, false, true)

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
		t.Fatalf("expected %d SYN packets written, got %d", len(scanPorts), len(mock.written))
	}

	if len(result.OpenPorts) != 1 {
		t.Fatalf("expected exactly 1 open port recorded, got %+v", result.OpenPorts)
	}
	got := result.OpenPorts[0]
	if got.Port != 22 || got.Protocol != "tcp" || got.State != "open" {
		t.Fatalf("expected port 22/tcp/open, got %+v", got)
	}
}

func TestEnrich_UpsertUpdatesExistingEntryNotDuplicate(t *testing.T) {
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

	openReply := buildTCPReply(deviceIP, localIP, 22, scanSourcePort, true, true, false)
	mock := &mockPacketHandle{replies: []gopacket.Packet{openReply}}
	openHandle = func(device string, snaplen int32, promisc bool, timeout time.Duration) (rawcapture.PacketHandle, error) {
		return mock, nil
	}

	e := &enricher{}
	device := engine.Device{
		IP:  "192.168.1.20",
		MAC: "aa:bb:cc:dd:ee:ff",
		OpenPorts: []engine.OpenPort{
			{Port: 22, Protocol: "tcp", State: "open"},
		},
	}
	result, err := e.Enrich(context.Background(), device)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.OpenPorts) != 1 {
		t.Fatalf("expected still exactly 1 OpenPort entry (upserted, not duplicated), got %+v", result.OpenPorts)
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

	openReply := buildTCPReply(deviceIP, localIP, 22, scanSourcePort, true, true, false)
	mock := &mockPacketHandle{replies: []gopacket.Packet{openReply}}
	openHandle = func(device string, snaplen int32, promisc bool, timeout time.Duration) (rawcapture.PacketHandle, error) {
		return mock, nil
	}

	e := &enricher{}
	device := engine.Device{
		IP:  "192.168.1.20",
		MAC: "aa:bb:cc:dd:ee:ff",
		OpenPorts: []engine.OpenPort{
			{Port: 22, Protocol: "tcp", State: "open", Banner: "SSH-2.0-OpenSSH"},
		},
	}
	result, err := e.Enrich(context.Background(), device)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.OpenPorts) != 1 {
		t.Fatalf("expected still exactly 1 OpenPort entry, got %+v", result.OpenPorts)
	}
	if result.OpenPorts[0].Banner != "SSH-2.0-OpenSSH" {
		t.Fatalf("expected re-confirming port 22 via SYN scan to preserve the Banner set by enrich/banner, got %+v", result.OpenPorts[0])
	}
}
