package arp

import (
	"context"
	"fmt"
	"net"
	"time"

	"nats/core/engine"
	"nats/discovery/internal/subnetutil"

	"github.com/gopacket/gopacket"
	"github.com/gopacket/gopacket/layers"
	"github.com/gopacket/gopacket/pcap"
)

func init() {
	engine.RegisterTechnique(&technique{})
}

type technique struct{}

func (t *technique) Name() string {
	return "arp"
}

func (t *technique) RequiresPrivilege() bool {
	requires, _ := probePrivilege()
	return requires
}

// ProbePrivilege implements engine.PrivilegeProber, exposing the real
// underlying error (e.g. permission denied, missing driver, no such device)
// instead of the generic "requires privilege" message RequiresPrivilege()
// alone can only imply.
func (t *technique) ProbePrivilege() (bool, error) {
	return probePrivilege()
}

// netInterfaces and ifaceAddrs are swappable so probePrivilege's
// device-resolution logic can be exercised in tests without depending on
// the host's real NICs.
var netInterfaces = net.Interfaces
var ifaceAddrs = func(iface net.Interface) ([]net.Addr, error) {
	return iface.Addrs()
}

// probePrivilege opens a capture handle on a real, up, running,
// non-loopback interface carrying a non-loopback IPv4 address, rather
// than an empty device name — libpcap does not treat "" as "pick any
// device" on every platform, and on some systems it fails with "No such
// device exists" regardless of privilege, which previously made this
// probe (and thus RequiresPrivilege) report "needs privilege" even when
// running as root.
//
// Requiring an IPv4 address is a heuristic, not a guarantee: it favors a
// real LAN-facing NIC over most virtual interfaces (docker0, veth*,
// tun/tap), but RequiresPrivilege() takes no target subnet, so it can
// still end up probing a different interface than the one Run()
// resolves for a given scan's target. It answers "can this process open a
// capture handle at all", which is what determines skip-vs-run.
var probePrivilege = func() (bool, error) {
	ifaces, err := netInterfaces()
	if err != nil {
		return true, err
	}

	var device string
	for _, iface := range ifaces {
		const upAndRunning = net.FlagUp | net.FlagRunning
		if iface.Flags&upAndRunning != upAndRunning || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := ifaceAddrs(iface)
		if err != nil {
			continue
		}
		if !hasIPv4Address(addrs) {
			continue
		}
		device = iface.Name
		break
	}
	if device == "" {
		return true, fmt.Errorf("arp: no active network interface with an IPv4 address found")
	}

	h, err := openPcap(device, 65536, false, pcap.BlockForever)
	if err != nil {
		return true, err
	}
	h.Close()
	return false, nil
}

func hasIPv4Address(addrs []net.Addr) bool {
	for _, addr := range addrs {
		ipnet, ok := addr.(*net.IPNet)
		if ok && ipnet.IP.To4() != nil && !ipnet.IP.IsLoopback() {
			return true
		}
	}
	return false
}

type packetHandle interface {
	Close()
	WritePacketData([]byte) error
	ReadPacketData() ([]byte, gopacket.CaptureInfo, error)
}

type pcapAdapter struct {
	handle *pcap.Handle
}

func (a *pcapAdapter) Close()                            { a.handle.Close() }
func (a *pcapAdapter) WritePacketData(data []byte) error { return a.handle.WritePacketData(data) }
func (a *pcapAdapter) ReadPacketData() ([]byte, gopacket.CaptureInfo, error) {
	return a.handle.ReadPacketData()
}

var openPcap = func(device string, snaplen int32, promisc bool, timeout time.Duration) (packetHandle, error) {
	h, err := pcap.OpenLive(device, snaplen, promisc, timeout)
	if err != nil {
		return nil, err
	}
	return &pcapAdapter{h}, nil
}

var resolveInterface = subnetutil.ResolveInterface

// EnumerateAddresses implements engine.AddressEnumerator, letting core/engine
// report how many addresses this sweep will cover, so a driving adapter can
// show "still pending" progress.
func (t *technique) EnumerateAddresses(target string) ([]string, error) {
	_, ipnet, err := net.ParseCIDR(target)
	if err != nil {
		return nil, fmt.Errorf("arp: invalid target %q: %w", target, err)
	}

	_, localIP, err := resolveInterface(ipnet)
	if err != nil {
		return nil, fmt.Errorf("arp: %w", err)
	}

	targets := subnetutil.EnumerateTargets(ipnet, localIP)
	addrs := make([]string, len(targets))
	for i, ip := range targets {
		addrs[i] = ip.String()
	}
	return addrs, nil
}

func (t *technique) Run(ctx context.Context, target string) (<-chan engine.Sighting, error) {
	_, ipnet, err := net.ParseCIDR(target)
	if err != nil {
		return nil, fmt.Errorf("arp: invalid target %q: %w", target, err)
	}

	if ones, bits := ipnet.Mask.Size(); bits == 32 && ones < 16 {
		return nil, fmt.Errorf("arp: subnet %s too large to scan (max /16)", ipnet.String())
	}

	iface, localIP, err := resolveInterface(ipnet)
	if err != nil {
		return nil, fmt.Errorf("arp: %w", err)
	}

	mac := iface.HardwareAddr
	if len(mac) == 0 {
		return nil, fmt.Errorf("arp: interface %s has no MAC address", iface.Name)
	}

	handle, err := openPcap(iface.Name, 65536, false, pcap.BlockForever)
	if err != nil {
		return nil, fmt.Errorf("arp: cannot open pcap handle on %s: %w", iface.Name, err)
	}

	targets := subnetutil.EnumerateTargets(ipnet, localIP)

	ch := make(chan engine.Sighting, len(targets))

	go func() {
		defer handle.Close()
		defer close(ch)

		for _, targetIP := range targets {
			pkt, err := buildARPRequest(mac, localIP, targetIP)
			if err != nil {
				continue
			}
			if err := handle.WritePacketData(pkt); err != nil {
				break
			}
		}

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

		seen := make(map[string]bool)
		localIPStr := localIP.String()

		handleReply := func(data []byte) bool {
			srcIP, srcMAC, ok := parseARPRely(data)
			if !ok || seen[srcIP] || srcIP == localIPStr {
				return true
			}
			seen[srcIP] = true
			select {
			case ch <- engine.Sighting{
				IP:        srcIP,
				MAC:       srcMAC,
				Technique: "arp",
			}:
				return true
			case <-ctx.Done():
				return false
			}
		}

		// drain flushes any packets the reader goroutine already buffered
		// before its terminal errCh send — that send happens-after those
		// packetCh sends in program order, so by the time errCh is received
		// here they're guaranteed visible. Without this, select's
		// pseudo-random tie-breaking between a ready packetCh and a ready
		// errCh could silently discard a buffered reply on every exit path
		// (mirrors enrich/tcpsyn and enrich/udpscan's identical fix).
		drain := func() {
			for {
				select {
				case data := <-packetCh:
					if !handleReply(data) {
						return
					}
				default:
					return
				}
			}
		}

		deadline := time.After(2 * time.Second)

		for {
			select {
			case <-ctx.Done():
				return
			case <-deadline:
				drain()
				return
			case data := <-packetCh:
				if !handleReply(data) {
					return
				}
			case <-errCh:
				drain()
				return
			}
		}
	}()

	return ch, nil
}

func buildARPRequest(srcMAC net.HardwareAddr, srcIP, dstIP net.IP) ([]byte, error) {
	eth := &layers.Ethernet{
		SrcMAC:       srcMAC,
		DstMAC:       net.HardwareAddr{0xff, 0xff, 0xff, 0xff, 0xff, 0xff},
		EthernetType: layers.EthernetTypeARP,
	}

	arp := &layers.ARP{
		AddrType:          layers.LinkTypeEthernet,
		Protocol:          0x0800,
		HwAddressSize:     6,
		ProtAddressSize:   4,
		Operation:         layers.ARPRequest,
		SourceHwAddress:   srcMAC,
		SourceProtAddress: srcIP.To4(),
		DstHwAddress:      net.HardwareAddr{0, 0, 0, 0, 0, 0},
		DstProtAddress:    dstIP.To4(),
	}

	buf := gopacket.NewSerializeBuffer()
	opts := gopacket.SerializeOptions{}
	if err := gopacket.SerializeLayers(buf, opts, eth, arp); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func parseARPRely(data []byte) (srcIP, srcMAC string, ok bool) {
	packet := gopacket.NewPacket(data, layers.LayerTypeEthernet, gopacket.NoCopy)
	arpLayer := packet.Layer(layers.LayerTypeARP)
	if arpLayer == nil {
		return "", "", false
	}
	arp, ok := arpLayer.(*layers.ARP)
	if !ok || arp.Operation != layers.ARPReply {
		return "", "", false
	}
	srcIP = net.IP(arp.SourceProtAddress).String()
	srcMAC = net.HardwareAddr(arp.SourceHwAddress).String()
	return srcIP, srcMAC, true
}
