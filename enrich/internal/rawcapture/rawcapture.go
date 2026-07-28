// Package rawcapture holds the gopacket/libpcap capture-handle plumbing
// shared by enrich/tcpsyn and enrich/udpscan (Story 2.3): opening a live
// capture handle, probing whether one can be opened at all (for
// RequiresPrivilege/ProbePrivilege), and resolving which local
// interface/IP can reach a given remote IP.
//
// This mirrors discovery/arp's capture pattern (Story 1.2) rather than
// reinventing it, but cannot import arp's own logic directly: it lives in
// discovery/internal/subnetutil, which Go's internal-package visibility
// rule restricts to importers under discovery/, and enrich/tcpsyn,
// enrich/udpscan live under enrich/ instead.
package rawcapture

import (
	"fmt"
	"net"
	"time"

	"github.com/gopacket/gopacket"
	"github.com/gopacket/gopacket/pcap"
)

// PacketHandle is the minimal capture-handle surface tcpsyn/udpscan need,
// swappable in tests for a fake so no real libpcap/root access is required
// (AD-6 testability convention).
type PacketHandle interface {
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

// OpenLive is swappable so tests can inject a fake PacketHandle instead of
// opening a real capture device.
var OpenLive = func(device string, snaplen int32, promisc bool, timeout time.Duration) (PacketHandle, error) {
	h, err := pcap.OpenLive(device, snaplen, promisc, timeout)
	if err != nil {
		return nil, err
	}
	return &pcapAdapter{h}, nil
}

// netInterfaces and ifaceAddrs are swappable, mirroring discovery/arp's
// probePrivilege pattern, so ResolveInterfaceForIP and ProbeCapture can be
// exercised in tests without depending on the host's real NICs.
var netInterfaces = net.Interfaces
var ifaceAddrs = func(iface net.Interface) ([]net.Addr, error) {
	return iface.Addrs()
}

const upAndRunning = net.FlagUp | net.FlagRunning

// ResolveInterfaceForIP returns the up, non-loopback interface and its
// local IPv4 address whose subnet contains ip. Unlike discovery techniques
// (which resolve an interface from the scan's target subnet), an Enricher
// only ever receives a single Device IP (engine.Enricher's Enrich takes a
// Device, not a subnet), so this resolves outward from the target address
// instead.
func ResolveInterfaceForIP(ip net.IP) (*net.Interface, net.IP, error) {
	ifaces, err := netInterfaces()
	if err != nil {
		return nil, nil, err
	}

	for i := range ifaces {
		iface := ifaces[i]
		if iface.Flags&upAndRunning != upAndRunning || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := ifaceAddrs(iface)
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			ipnet, ok := addr.(*net.IPNet)
			if !ok || ipnet.IP.To4() == nil {
				continue
			}
			if ipnet.Contains(ip) {
				return &iface, ipnet.IP.To4(), nil
			}
		}
	}

	return nil, nil, fmt.Errorf("rawcapture: no local interface routes to %s", ip)
}

// ProbeCapture reports whether this process can open a live capture handle
// at all, for RequiresPrivilege/ProbePrivilege. It picks a real, up,
// running, non-loopback interface carrying a non-loopback IPv4 address
// (same heuristic as discovery/arp's probePrivilege) rather than an empty
// device name, since libpcap does not treat "" as "pick any device" on
// every platform.
func ProbeCapture() (bool, error) {
	ifaces, err := netInterfaces()
	if err != nil {
		return true, err
	}

	var device string
	for _, iface := range ifaces {
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
		return true, fmt.Errorf("rawcapture: no active network interface with an IPv4 address found")
	}

	h, err := OpenLive(device, 65536, false, pcap.BlockForever)
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
