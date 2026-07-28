// Package subnetutil holds subnet-enumeration and local-interface-resolution
// logic shared by discovery techniques that sweep a CIDR range (arp, icmp).
package subnetutil

import (
	"encoding/binary"
	"fmt"
	"net"
)

// EnumerateTargets returns every host address within ipnet, excluding the
// network address, broadcast address, and the optional exclude IP.
func EnumerateTargets(ipnet *net.IPNet, exclude net.IP) []net.IP {
	network := ipnet.IP.Mask(ipnet.Mask).To4()

	maskU32 := binary.BigEndian.Uint32(net.IP(ipnet.Mask).To4())
	start := binary.BigEndian.Uint32(network)
	end := start | ^maskU32

	var excludeU32 uint32
	if exclude != nil {
		excludeU32 = binary.BigEndian.Uint32(exclude.To4())
	}

	var targets []net.IP
	for addr := start + 1; addr < end; addr++ {
		if addr == excludeU32 {
			continue
		}
		ip := make(net.IP, 4)
		binary.BigEndian.PutUint32(ip, addr)
		targets = append(targets, ip)
	}

	return targets
}

// ResolveInterface returns the up network interface and its local IPv4
// address that fall within ipnet.
func ResolveInterface(ipnet *net.IPNet) (*net.Interface, net.IP, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, nil, fmt.Errorf("cannot list interfaces: %w", err)
	}
	return resolveInterface(ipnet, ifaces, func(iface net.Interface) ([]net.Addr, error) {
		return iface.Addrs()
	})
}

func resolveInterface(ipnet *net.IPNet, ifaces []net.Interface, getAddrs func(net.Interface) ([]net.Addr, error)) (*net.Interface, net.IP, error) {
	for i := range ifaces {
		if ifaces[i].Flags&net.FlagUp == 0 {
			continue
		}
		addrs, err := getAddrs(ifaces[i])
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			ipnet2, ok := addr.(*net.IPNet)
			if !ok {
				continue
			}
			if ipnet2.IP.To4() == nil {
				continue
			}
			if ipnet.Contains(ipnet2.IP) {
				return &ifaces[i], ipnet2.IP, nil
			}
		}
	}

	return nil, nil, fmt.Errorf("no interface found for subnet %s", ipnet.String())
}

// FindLocalIP returns the local IPv4 address, from any up interface, that
// falls within ipnet, or nil if none is found.
func FindLocalIP(ipnet *net.IPNet) net.IP {
	_, ip, err := ResolveInterface(ipnet)
	if err != nil {
		return nil
	}
	return ip
}
