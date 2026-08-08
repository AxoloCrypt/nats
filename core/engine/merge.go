package engine

import (
	"fmt"
	"sort"
)

func Merge(sightings []Sighting) ([]Device, []Diagnostic) {
	var diags []Diagnostic

	var valid []Sighting
	for _, s := range sightings {
		if s.IP == "" {
			diags = append(diags, Diagnostic{
				Severity: "error",
				Message:  "sighting missing required IP",
				Reason:   fmt.Sprintf("sighting from technique %q has no IP address", s.Technique),
			})
			continue
		}
		valid = append(valid, s)
	}

	macGroups := make(map[string][]Sighting)
	var noMACSightings []Sighting

	for _, s := range valid {
		if s.MAC != "" {
			macGroups[s.MAC] = append(macGroups[s.MAC], s)
		} else {
			noMACSightings = append(noMACSightings, s)
		}
	}

	ipToNonEmptyMACs := make(map[string]map[string]bool)
	for _, s := range valid {
		if s.MAC == "" {
			continue
		}
		if ipToNonEmptyMACs[s.IP] == nil {
			ipToNonEmptyMACs[s.IP] = make(map[string]bool)
		}
		ipToNonEmptyMACs[s.IP][s.MAC] = true
	}

	conflictedIPs := make(map[string]bool)
	for ip, macs := range ipToNonEmptyMACs {
		if len(macs) > 1 {
			conflictedIPs[ip] = true
		}
	}

	var devices []Device
	ipClaimedByMAC := make(map[string]int)

	for mac, group := range macGroups {
		dev := Device{
			MAC: mac,
			IP:  group[0].IP,
		}
		applyServiceData(&dev, group)
		devices = append(devices, dev)
		idx := len(devices) - 1
		// Every IP this MAC was seen at (not just the first) belongs to this
		// Device, so a later no-MAC sighting at any of them still merges in.
		for _, s := range group {
			if !conflictedIPs[s.IP] {
				ipClaimedByMAC[s.IP] = idx
			}
		}
	}

	noMACIPsDone := make(map[string]bool)
	for _, s := range noMACSightings {
		if conflictedIPs[s.IP] {
			// The IP-match clause only applies when no Sighting in the scan
			// asserts a conflicting MAC for that IP. A conflict here means the
			// IP-match rule doesn't apply to *any* pair sharing this IP, so
			// no-MAC sightings must not be deduped against each other either.
			dev := Device{IP: s.IP}
			applyServiceData(&dev, []Sighting{s})
			devices = append(devices, dev)
			continue
		}
		if noMACIPsDone[s.IP] {
			continue
		}
		if _, ok := ipClaimedByMAC[s.IP]; ok {
			continue
		}
		noMACIPsDone[s.IP] = true
		dev := Device{IP: s.IP}
		applyServiceData(&dev, []Sighting{s})
		devices = append(devices, dev)
	}

	sort.Slice(devices, func(i, j int) bool {
		if devices[i].IP != devices[j].IP {
			return devices[i].IP < devices[j].IP
		}
		return devices[i].MAC < devices[j].MAC
	})

	return devices, diags
}

// applyServiceData copies a discovery-sourced Hostname/Vendor from a group of
// Sightings into dev, so that enrichment has a discovery-sourced value to
// potentially override. It also carries each Sighting's ServiceData through
// onto dev.ServiceData, key by key, since identity merging alone doesn't
// require preserving it and Classify is the first consumer.
// The first non-empty value found wins, for both the named fields and each
// ServiceData key; a Device's own value is never overwritten with an empty
// one. The "hostname"/"vendor" keys are excluded from the generic
// ServiceData copy since they already land on the dedicated Hostname/Vendor
// fields above — duplicating them into ServiceData would let the
// classifier's service-type keyword match run against free-text
// hostname/vendor content it isn't designed to interpret.
func applyServiceData(dev *Device, sightings []Sighting) {
	for _, s := range sightings {
		if dev.Hostname == "" && s.ServiceData["hostname"] != "" {
			dev.Hostname = s.ServiceData["hostname"]
		}
		if dev.Vendor == "" && s.ServiceData["vendor"] != "" {
			dev.Vendor = s.ServiceData["vendor"]
		}
		for k, v := range s.ServiceData {
			if v == "" || k == "hostname" || k == "vendor" {
				continue
			}
			if dev.ServiceData == nil {
				dev.ServiceData = make(map[string]string)
			}
			if dev.ServiceData[k] == "" {
				dev.ServiceData[k] = v
			}
		}
	}
}
