// Package udpscan implements engine.Enricher via a raw UDP scan, one of
// three opt-in "deeper enrichment" enrichers that never run unless a user
// explicitly names them via cmd/cli's --enrich flag.
package udpscan

import (
	"context"
	"net"
	"time"

	"nats/core/engine"
	"nats/enrich/internal/rawcapture"

	"github.com/gopacket/gopacket"
	"github.com/gopacket/gopacket/layers"
	"github.com/gopacket/gopacket/pcap"
)

func init() {
	engine.RegisterEnricher(&enricher{})
}

// scanPorts mirrors enrich/tcpconnect's default port list, substituting the
// two common UDP services (DNS 53, SNMP 161) for a couple
// of tcpconnect's TCP-only entries — kept otherwise aligned so a "deeper"
// scan is still recognizably the same common-services sweep.
var scanPorts = []int{53, 67, 68, 69, 123, 137, 138, 161, 162, 443, 500, 1900, 5353}

const scanSourcePort = layers.UDPPort(54321)

// responseWindow bounds how long Enrich waits for a UDP reply or an ICMPv4
// "port unreachable" after sending every port's probe for one device.
var responseWindow = 1 * time.Second

type enricher struct{}

func (e *enricher) Name() string {
	return "udpscan"
}

func (e *enricher) RequiresPrivilege() bool {
	requires, _ := probePrivilege()
	return requires
}

// ProbePrivilege implements engine.PrivilegeProber, exposing the real
// underlying error (e.g. permission denied, missing libpcap/Npcap driver)
// instead of the generic "requires privilege" message RequiresPrivilege()
// alone can only imply.
func (e *enricher) ProbePrivilege() (bool, error) {
	return probePrivilege()
}

var probePrivilege = rawcapture.ProbeCapture
var openHandle = rawcapture.OpenLive
var resolveInterfaceForIP = rawcapture.ResolveInterfaceForIP

func (e *enricher) Enrich(ctx context.Context, device engine.Device) (engine.Device, error) {
	if device.IP == "" || device.MAC == "" {
		// Same-subnet L2 scan (like enrich/tcpsyn) — a device without a
		// resolved MAC can't be addressed this way. Not an error.
		return device, nil
	}

	dstIP := net.ParseIP(device.IP)
	if dstIP == nil || dstIP.To4() == nil {
		return device, nil
	}
	dstMAC, err := net.ParseMAC(device.MAC)
	if err != nil {
		return device, nil
	}

	iface, srcIP, err := resolveInterfaceForIP(dstIP)
	if err != nil {
		// No local interface currently routes to this device — expected,
		// not a capability failure (RequiresPrivilege/ProbePrivilege
		// already gates that).
		return device, nil
	}
	srcMAC := iface.HardwareAddr
	if len(srcMAC) == 0 {
		return device, nil
	}

	handle, err := openHandle(iface.Name, 65536, false, pcap.BlockForever)
	if err != nil {
		return device, err
	}
	defer handle.Close()

	for _, port := range scanPorts {
		pkt, err := buildUDPProbe(srcMAC, dstMAC, srcIP, dstIP.To4(), scanSourcePort, layers.UDPPort(port))
		if err != nil {
			continue
		}
		if err := handle.WritePacketData(pkt); err != nil {
			break
		}
	}

	open, closedPorts := collectUDPResponses(ctx, handle, dstIP.To4(), scanSourcePort, responseWindow)
	closed := make(map[int]bool, len(closedPorts))
	for _, p := range closedPorts {
		closed[p] = true
	}

	for _, port := range open {
		device.Upsert(upsertUDPPort(device, port, "open"))
	}
	// A UDP port that neither answered directly nor triggered an ICMP
	// port-unreachable is genuinely ambiguous — closed-but-silently-dropped
	// and open-but-silent are indistinguishable to any UDP scanner without
	// a protocol-specific probe payload. Recording "open|filtered" (the
	// conventional UDP-scan term for this ambiguity) still tells the user
	// more than omitting the port entirely, without falsely claiming it's
	// confirmed open.
	for _, port := range scanPorts {
		if closed[port] {
			continue
		}
		if containsInt(open, port) {
			continue
		}
		device.Upsert(upsertUDPPort(device, port, "open|filtered"))
	}

	return device, nil
}

func containsInt(ports []int, port int) bool {
	for _, p := range ports {
		if p == port {
			return true
		}
	}
	return false
}

// upsertUDPPort builds the OpenPort value to pass to device.Upsert for a udp
// port, preserving any field already recorded on a matching existing entry
// — Upsert itself does a full-struct overwrite, so building a bare literal
// here would silently blank a field owned by a different enricher
// that already ran (mirrors enrich/tcpsyn's identical guard).
func upsertUDPPort(device engine.Device, port int, state string) engine.OpenPort {
	entry := engine.OpenPort{Port: port, Protocol: "udp", State: state}
	for _, existing := range device.OpenPorts {
		if existing.Port == port && existing.Protocol == "udp" {
			entry.Banner = existing.Banner
			break
		}
	}
	return entry
}

func buildUDPProbe(srcMAC, dstMAC net.HardwareAddr, srcIP, dstIP net.IP, srcPort, dstPort layers.UDPPort) ([]byte, error) {
	eth := &layers.Ethernet{
		SrcMAC:       srcMAC,
		DstMAC:       dstMAC,
		EthernetType: layers.EthernetTypeIPv4,
	}
	ip := &layers.IPv4{
		Version:  4,
		TTL:      64,
		Protocol: layers.IPProtocolUDP,
		SrcIP:    srcIP,
		DstIP:    dstIP,
	}
	udp := &layers.UDP{
		SrcPort: srcPort,
		DstPort: dstPort,
	}
	if err := udp.SetNetworkLayerForChecksum(ip); err != nil {
		return nil, err
	}

	buf := gopacket.NewSerializeBuffer()
	opts := gopacket.SerializeOptions{ComputeChecksums: true, FixLengths: true}
	if err := gopacket.SerializeLayers(buf, opts, eth, ip, udp, gopacket.Payload{0}); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// collectUDPResponses reads from handle until window elapses, ctx is
// canceled, or the handle reports an error (mirroring enrich/tcpsyn's
// collectSYNResponses), returning ports confirmed open (direct UDP reply)
// separately from ports confirmed closed (ICMP port-unreachable).
func collectUDPResponses(ctx context.Context, handle rawcapture.PacketHandle, expectedSrcIP net.IP, ourPort layers.UDPPort, window time.Duration) (open []int, closed []int) {
	packetCh := make(chan []byte, 100)
	errCh := make(chan error, 1)

	go func() {
		for {
			data, _, err := handle.ReadPacketData()
			if err != nil {
				select {
				case errCh <- err:
				default:
				}
				return
			}
			select {
			case packetCh <- data:
			case <-ctx.Done():
				return
			}
		}
	}()

	classify := func(data []byte) {
		if port, ok := parseUDPReply(data, expectedSrcIP, ourPort); ok {
			open = append(open, port)
			return
		}
		if port, ok := parsePortUnreachable(data, expectedSrcIP, ourPort); ok {
			closed = append(closed, port)
		}
	}

	// drain flushes any packets the reader goroutine already buffered before
	// its terminal errCh send — that send happens-after those packetCh sends
	// in program order, so by the time errCh is received here they're
	// guaranteed visible. Without this, select's pseudo-random tie-breaking
	// between a ready packetCh and a ready errCh could silently discard a
	// buffered reply on every exit path.
	drain := func() {
		for {
			select {
			case data := <-packetCh:
				classify(data)
			default:
				return
			}
		}
	}

	deadline := time.After(window)
	for {
		select {
		case <-ctx.Done():
			drain()
			return open, closed
		case <-deadline:
			drain()
			return open, closed
		case <-errCh:
			drain()
			return open, closed
		case data := <-packetCh:
			classify(data)
		}
	}
}

func parseUDPReply(data []byte, expectedSrcIP net.IP, ourPort layers.UDPPort) (port int, ok bool) {
	packet := gopacket.NewPacket(data, layers.LayerTypeEthernet, gopacket.NoCopy)

	ipLayer := packet.Layer(layers.LayerTypeIPv4)
	if ipLayer == nil {
		return 0, false
	}
	ip, isIP := ipLayer.(*layers.IPv4)
	if !isIP || !ip.SrcIP.Equal(expectedSrcIP) {
		return 0, false
	}

	udpLayer := packet.Layer(layers.LayerTypeUDP)
	if udpLayer == nil {
		return 0, false
	}
	udp, isUDP := udpLayer.(*layers.UDP)
	if !isUDP || udp.DstPort != ourPort {
		return 0, false
	}
	return int(udp.SrcPort), true
}

// parsePortUnreachable decodes an ICMPv4 "destination unreachable, port
// unreachable" reply and extracts the original destination port from its
// embedded copy of the probe's IP+UDP headers, so the specific closed port
// can be identified even though the ICMP packet's own IP source is the
// device, not a per-port response.
func parsePortUnreachable(data []byte, expectedSrcIP net.IP, ourPort layers.UDPPort) (port int, ok bool) {
	packet := gopacket.NewPacket(data, layers.LayerTypeEthernet, gopacket.NoCopy)

	ipLayer := packet.Layer(layers.LayerTypeIPv4)
	if ipLayer == nil {
		return 0, false
	}
	ip, isIP := ipLayer.(*layers.IPv4)
	if !isIP || !ip.SrcIP.Equal(expectedSrcIP) {
		return 0, false
	}

	icmpLayer := packet.Layer(layers.LayerTypeICMPv4)
	if icmpLayer == nil {
		return 0, false
	}
	icmp, isICMP := icmpLayer.(*layers.ICMPv4)
	if !isICMP {
		return 0, false
	}
	if icmp.TypeCode.Type() != layers.ICMPv4TypeDestinationUnreachable || icmp.TypeCode.Code() != layers.ICMPv4CodePort {
		return 0, false
	}

	inner := gopacket.NewPacket(icmp.LayerPayload(), layers.LayerTypeIPv4, gopacket.NoCopy)
	innerUDPLayer := inner.Layer(layers.LayerTypeUDP)
	if innerUDPLayer == nil {
		return 0, false
	}
	innerUDP, isInnerUDP := innerUDPLayer.(*layers.UDP)
	if !isInnerUDP || innerUDP.SrcPort != ourPort {
		return 0, false
	}
	return int(innerUDP.DstPort), true
}
