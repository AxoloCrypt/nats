package engine

import (
	"net"
)

var netInterfaces = net.Interfaces

func resolveSubnet(opts Options) (string, *Diagnostic) {
	if opts.Subnet != "" {
		return opts.Subnet, nil
	}

	ifaces, err := netInterfaces()
	if err != nil {
		return "", &Diagnostic{
			Severity: "error",
			Message:  "no active network interface found",
			Reason:   err.Error(),
		}
	}

	return findSubnet(ifaces, func(iface net.Interface) ([]net.Addr, error) {
		return iface.Addrs()
	})
}

func findSubnet(ifaces []net.Interface, getAddrs func(net.Interface) ([]net.Addr, error)) (string, *Diagnostic) {
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 {
			continue
		}
		if iface.Flags&net.FlagLoopback != 0 {
			continue
		}

		addrs, err := getAddrs(iface)
		if err != nil {
			continue
		}

		for _, addr := range addrs {
			ipnet, ok := addr.(*net.IPNet)
			if !ok {
				continue
			}
			if ipnet.IP.To4() == nil {
				continue
			}

			_, bits := ipnet.Mask.Size()
			if bits != 32 {
				continue
			}

			network := ipnet.IP.Mask(ipnet.Mask)
			return (&net.IPNet{IP: network, Mask: ipnet.Mask}).String(), nil
		}
	}

	return "", &Diagnostic{
		Severity: "error",
		Message:  "no active network interface found",
		Reason:   "no active IPv4 interface with a valid subnet was detected; check that your network cable is connected or your Wi-Fi is enabled",
	}
}
