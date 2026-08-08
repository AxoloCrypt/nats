// Package tcpsyn implements engine.Enricher via a raw TCP SYN scan, one of
// three opt-in "deeper enrichment" enrichers that never run unless a user
// explicitly names them via cmd/cli's --enrich flag.
package tcpsyn

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

// scanPorts mirrors enrich/tcpconnect's default port list so the opt-in SYN
// scan checks the same common-services set rather than an unrelated one —
// tcpconnect and tcpsyn disagreeing on the same port would be confusing
// rather than "deeper".
var scanPorts = []int{21, 22, 23, 25, 53, 80, 110, 139, 143, 443, 445, 554, 631, 3389, 8009, 8080, 9100}

// scanSourcePort is the source port used to address every crafted SYN in a
// given Enrich call. It never needs to vary between devices or ports: the
// scan doesn't go through the OS TCP stack (raw packets), so there's no
// real socket to collide with, and responses are correlated by this fixed
// value in the reply's destination port.
const scanSourcePort = layers.TCPPort(54321)

// responseWindow bounds how long Enrich waits for SYN-ACK/RST replies
// after sending every port's SYN for one device — short enough that
// scanning many devices stays within core/engine's shared enrichTimeout,
// long enough for a real LAN round-trip.
var responseWindow = 1 * time.Second

type enricher struct{}

func (e *enricher) Name() string {
	return "tcpsyn"
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
		// A raw SYN needs a destination MAC to address the Ethernet frame
		// (this is a same-subnet, L2 scan, like discovery/arp) — a device
		// merged without one (e.g. mDNS/SSDP-only, no ARP corroboration)
		// can't be targeted this way. Not an error: enrich/tcpconnect still
		// covers it via a normal IP-routed connect.
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
		// No local interface currently routes to this device — e.g. it
		// left the LAN mid-scan. Expected, not a capability failure
		// (RequiresPrivilege/ProbePrivilege already gates that).
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
		pkt, err := buildSYNPacket(srcMAC, dstMAC, srcIP, dstIP.To4(), scanSourcePort, layers.TCPPort(port))
		if err != nil {
			continue
		}
		if err := handle.WritePacketData(pkt); err != nil {
			break
		}
	}

	for _, port := range collectSYNResponses(ctx, handle, dstIP.To4(), scanSourcePort, responseWindow) {
		device.Upsert(upsertOpenTCPPort(device, port, "open"))
	}

	return device, nil
}

// upsertOpenTCPPort builds the OpenPort value to pass to device.Upsert for a
// tcp port this enricher just confirmed, preserving any field (e.g. Banner,
// set by enrich/banner) already recorded on a matching existing entry —
// Upsert itself does a full-struct overwrite, so building a bare literal
// here would silently blank a field owned by a different enricher
// that already ran, the same pitfall enrich/banner's own Upsert call site
// avoids.
func upsertOpenTCPPort(device engine.Device, port int, state string) engine.OpenPort {
	entry := engine.OpenPort{Port: port, Protocol: "tcp", State: state}
	for _, existing := range device.OpenPorts {
		if existing.Port == port && existing.Protocol == "tcp" {
			entry.Banner = existing.Banner
			break
		}
	}
	return entry
}

func buildSYNPacket(srcMAC, dstMAC net.HardwareAddr, srcIP, dstIP net.IP, srcPort, dstPort layers.TCPPort) ([]byte, error) {
	eth := &layers.Ethernet{
		SrcMAC:       srcMAC,
		DstMAC:       dstMAC,
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
		Seq:     0,
		SYN:     true,
		Window:  65535,
	}
	if err := tcp.SetNetworkLayerForChecksum(ip); err != nil {
		return nil, err
	}

	buf := gopacket.NewSerializeBuffer()
	opts := gopacket.SerializeOptions{ComputeChecksums: true, FixLengths: true}
	if err := gopacket.SerializeLayers(buf, opts, eth, ip, tcp); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// collectSYNResponses reads from handle until window elapses, ctx is
// canceled, or the handle reports an error (e.g. a mock's replies being
// exhausted, mirroring discovery/arp's Run loop), returning the ports that
// answered SYN-ACK (open). Ports that answered RST (closed) or never
// answered within window are simply absent from the result — closed and
// filtered are indistinguishable from a raw scan's perspective, and only
// "open" is ever recorded.
func collectSYNResponses(ctx context.Context, handle rawcapture.PacketHandle, expectedSrcIP net.IP, ourPort layers.TCPPort, window time.Duration) []int {
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

	var open []int
	classify := func(data []byte) {
		port, isOpen, ok := parseSYNResponse(data, expectedSrcIP, ourPort)
		if !ok || !isOpen {
			return
		}
		open = append(open, port)
	}

	// drain flushes any packets the reader goroutine already buffered before
	// its terminal errCh send — that send happens-after those packetCh sends
	// in program order, so by the time errCh is received here they're
	// guaranteed visible. Without this, select's pseudo-random tie-breaking
	// between a ready packetCh and a ready errCh could silently discard a
	// buffered reply on every exit path (mirrors enrich/udpscan's identical
	// fix).
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
			return open
		case <-deadline:
			drain()
			return open
		case <-errCh:
			drain()
			return open
		case data := <-packetCh:
			classify(data)
		}
	}
}

func parseSYNResponse(data []byte, expectedSrcIP net.IP, ourPort layers.TCPPort) (port int, open bool, ok bool) {
	packet := gopacket.NewPacket(data, layers.LayerTypeEthernet, gopacket.NoCopy)

	ipLayer := packet.Layer(layers.LayerTypeIPv4)
	if ipLayer == nil {
		return 0, false, false
	}
	ip, isIP := ipLayer.(*layers.IPv4)
	if !isIP || !ip.SrcIP.Equal(expectedSrcIP) {
		return 0, false, false
	}

	tcpLayer := packet.Layer(layers.LayerTypeTCP)
	if tcpLayer == nil {
		return 0, false, false
	}
	tcp, isTCP := tcpLayer.(*layers.TCP)
	if !isTCP || tcp.DstPort != ourPort {
		return 0, false, false
	}

	if tcp.SYN && tcp.ACK {
		return int(tcp.SrcPort), true, true
	}
	if tcp.RST {
		return int(tcp.SrcPort), false, true
	}
	return 0, false, false
}
