package icmp

import (
	"context"
	"fmt"
	"net"
	"os"
	"strings"
	"time"

	"nats/core/engine"
	"nats/discovery/internal/subnetutil"

	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"
)

func init() {
	engine.RegisterTechnique(&technique{})
}

type technique struct{}

func (t *technique) Name() string {
	return "icmp"
}

// icmpNetworks lists, in order of preference, the ListenPacket network
// strings this technique tries. "udp4" is the non-privileged
// datagram-oriented ICMP endpoint: on Linux it only works when the
// process's group falls inside the net.ipv4.ping_group_range sysctl, which
// many distributions ship disabled by default — in that case even root is
// denied, since that gate checks group membership, not capabilities.
// "ip4:icmp" is the privileged raw ICMP endpoint, typically gated by
// CAP_NET_RAW (which root normally has) instead, so it's tried as a
// fallback; a sandboxing layer (e.g. seccomp) could still deny it even to
// root, in which case both attempts fail and the underlying errors from
// both are surfaced together.
var icmpNetworks = []string{"udp4", "ip4:icmp"}

var listenICMP = icmp.ListenPacket

// openICMPConn tries each network in icmpNetworks in turn and returns the
// first one that succeeds, along with which network it was — callers need
// that to know whether to address peers as net.UDPAddr (udp4) or
// net.IPAddr (ip4:icmp), per the icmp package's WriteTo contract. If every
// network fails, the returned error reports each attempt's failure so
// neither cause (e.g. ping_group_range vs. CAP_NET_RAW) is lost.
var openICMPConn = func() (*icmp.PacketConn, string, error) {
	var errs []string
	for _, network := range icmpNetworks {
		conn, err := listenICMP(network, "")
		if err == nil {
			return conn, network, nil
		}
		errs = append(errs, fmt.Sprintf("%s: %s", network, err))
	}
	return nil, "", fmt.Errorf("%s", strings.Join(errs, "; "))
}

var probePrivilege = func() (bool, error) {
	conn, _, err := openICMPConn()
	if err != nil {
		return true, err
	}
	conn.Close()
	return false, nil
}

func (t *technique) RequiresPrivilege() bool {
	requires, _ := probePrivilege()
	return requires
}

// ProbePrivilege implements engine.PrivilegeProber, exposing the real
// underlying error (e.g. permission denied, an unsupported socket type)
// instead of the generic "requires privilege" message RequiresPrivilege()
// alone can only imply.
func (t *technique) ProbePrivilege() (bool, error) {
	return probePrivilege()
}

// EnumerateAddresses implements engine.AddressEnumerator, letting core/engine
// report how many addresses this sweep will cover, so a driving adapter can
// show "still pending" progress.
func (t *technique) EnumerateAddresses(target string) ([]string, error) {
	_, ipnet, err := net.ParseCIDR(target)
	if err != nil {
		return nil, fmt.Errorf("icmp: invalid target %q: %w", target, err)
	}

	localIP := subnetutil.FindLocalIP(ipnet)
	targets := subnetutil.EnumerateTargets(ipnet, localIP)
	addrs := make([]string, len(targets))
	for i, ip := range targets {
		addrs[i] = ip.String()
	}
	return addrs, nil
}

// destAddr builds the WriteTo destination in the shape the icmp package's
// PacketConn requires for the connection's underlying network: net.IPAddr
// for the privileged raw endpoint (ip4:icmp), net.UDPAddr otherwise.
func destAddr(raw bool, ip net.IP) net.Addr {
	if raw {
		return &net.IPAddr{IP: ip}
	}
	return &net.UDPAddr{IP: ip}
}

// peerIPString extracts the peer IP from a ReadFrom result, which is a
// net.UDPAddr on the non-privileged udp4 endpoint or a net.IPAddr on the
// privileged raw ip4:icmp endpoint. ok is false for any other address type.
func peerIPString(peer net.Addr) (ip string, ok bool) {
	switch a := peer.(type) {
	case *net.UDPAddr:
		return a.IP.String(), true
	case *net.IPAddr:
		return a.IP.String(), true
	default:
		return "", false
	}
}

func (t *technique) Run(ctx context.Context, target string) (<-chan engine.Sighting, error) {
	_, ipnet, err := net.ParseCIDR(target)
	if err != nil {
		return nil, fmt.Errorf("icmp: invalid target %q: %w", target, err)
	}

	if ipnet.IP.To4() == nil {
		return nil, fmt.Errorf("icmp: only IPv4 targets supported, got %q", target)
	}

	if ones, bits := ipnet.Mask.Size(); bits == 32 && ones < 16 {
		return nil, fmt.Errorf("icmp: subnet %s too large to scan (max /16)", ipnet.String())
	}

	conn, network, err := openICMPConn()
	if err != nil {
		return nil, fmt.Errorf("icmp: cannot open socket: %w", err)
	}
	raw := network != "udp4"

	localIP := subnetutil.FindLocalIP(ipnet)
	targets := subnetutil.EnumerateTargets(ipnet, localIP)
	ch := make(chan engine.Sighting, len(targets))

	go func() {
		defer conn.Close()
		defer close(ch)

		deadline := time.After(3 * time.Second)
		readDone := make(chan struct{})

		go func() {
			defer close(readDone)

			seen := make(map[string]bool)
			buf := make([]byte, 1500)

			for {
				select {
				case <-ctx.Done():
					return
				case <-deadline:
					return
				default:
					conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
					n, peer, err := conn.ReadFrom(buf)
					if err != nil {
						continue
					}
					peerIP, ok := peerIPString(peer)
					if !ok {
						continue
					}
					if seen[peerIP] {
						continue
					}
					msg, err := icmp.ParseMessage(ipv4.ICMPTypeEcho.Protocol(), buf[:n])
					if err != nil {
						continue
					}
					if msg.Type != ipv4.ICMPTypeEchoReply {
						continue
					}
					seen[peerIP] = true
					select {
					case ch <- engine.Sighting{IP: peerIP, MAC: "", Technique: "icmp"}:
					case <-ctx.Done():
						return
					}
				}
			}
		}()

		for _, targetIP := range targets {
			msg := icmp.Message{
				Type: ipv4.ICMPTypeEcho,
				Code: 0,
				Body: &icmp.Echo{
					ID:   os.Getpid() & 0xffff,
					Seq:  1,
					Data: []byte("nats-scan"),
				},
			}
			bytes, err := msg.Marshal(nil)
			if err != nil {
				continue
			}
			conn.WriteTo(bytes, destAddr(raw, targetIP))
		}

		<-readDone
	}()

	return ch, nil
}
