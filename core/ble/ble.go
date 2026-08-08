package ble

import (
	"context"

	"nats/internal/strutil"
)

// unknownSkipReason keeps the "why" half of NL-FR-13's warning non-empty.
// A skip reason is always the scanner's own diagnosis, passed through
// verbatim and never replaced — but BLEScanner.Probe's contract (ports.go)
// permits ok=false with an empty reason, and cmd/cli's renderDiagnostic
// omits the "reason:" line entirely when Reason is "". That combination
// would print a bare "BLE scan skipped", naming what was skipped but never
// why. This stands in only for that case.
const unknownSkipReason = "the platform BLE scanner reported it could not start, without giving a reason"

// skipDiagnostic builds the one Diagnostic a skipped scan reports.
//
// Severity is always "warning", never "error": a permission gap (or an
// unavailable adapter) is a degraded-but-completed condition, mirroring the
// base spine's Story 1.5 precedent for a skipped privileged technique. The
// message names the whole BLE scan rather than "a technique" because only
// one scanner exists per platform build (the RegisterScanner/GetScanner
// registry) — unlike LAN scanning's arp/icmp/mdns/ssdp fan-out, "skipped"
// here is a total statement, not a partial one.
//
// Single-sourced across all three skip paths so their wording and severity
// can't drift apart.
// A whitespace-only reason counts as absent, not as a reason: renderDiagnostic
// only suppresses the "reason:" line when Reason is exactly "", so " " would
// print a blank one — the same bare warning unknownSkipReason exists to
// prevent, just spelled with spaces.
func skipDiagnostic(reason string) []Diagnostic {
	if strutil.IsBlank(reason) {
		reason = unknownSkipReason
	}
	return []Diagnostic{{
		Severity: "warning",
		Message:  "BLE scan skipped",
		Reason:   reason,
	}}
}

// Run is the BLE vertical's sole driving entrypoint (NL-AD-1). It never
// imports nats/core/engine, nats/cmd/..., or (once it exists)
// nats/core/wifimonitor.
//
// Deliberate divergence from core/engine.Run, in both of the cases that
// produce an empty Report:
//
//   - A scan that completes normally but observes zero advertisements
//     reports no Diagnostic at all. engine.Run appends an error-severity
//     "no devices discovered" in that case because LAN scanning can assume
//     at least a router is reachable; BLE can assume nothing of the sort —
//     being somewhere with nothing broadcasting nearby is an ordinary
//     outcome, not a failure worth alarming the user over.
//
//   - On the skip paths below, the single warning is the whole report. Note
//     that engine.Run would *not* behave this way: its hasError flag is set
//     only by error-severity paths, and its skipped-technique branch emits a
//     warning and continues without setting it, so a LAN run whose
//     techniques were all skipped gets the generic "no devices discovered"
//     error stacked on top of the warnings that already explain it. That is
//     tolerable there, where a skip is partial (one of arp/icmp/mdns/ssdp).
//     Here it would be actively misleading: only one scanner exists per
//     platform build, so "the scan was skipped" already fully explains "zero
//     devices" and a second generic diagnostic adds noise, not information.
func Run(ctx context.Context, opts Options) (<-chan Event, error) {
	ch := make(chan Event, 1)

	go func() {
		defer close(ch)

		// The same Diagnostic slice is carried on both the Event and the
		// Report: the Event copy is what cmd/cli renders to stderr, the
		// Report copy is what a machine-readable Writer serializes, and a
		// skipped scan must be visible through either channel.
		skipped := func(reason string) {
			diags := skipDiagnostic(reason)
			ch <- Event{
				Kind:        EventKindDone,
				Diagnostics: diags,
				Report:      Report{Diagnostics: diags},
			}
		}

		s, ok := GetScanner()
		if !ok {
			skipped("no BLEScanner registered")
			return
		}

		probeOK, reason := s.Probe()
		if !probeOK {
			skipped(reason)
			return
		}

		advCh, err := s.Scan(ctx, opts.Window)
		if err != nil {
			skipped(err.Error())
			return
		}

		// devices/seen merge repeated advertisements from the same physical
		// device by Address, preserving the pre-existing AD-12 statelessness
		// guarantee (both stay local to this goroutine closure — never
		// package-level — same as devices already was before this merge was
		// added). A real peripheral re-broadcasts roughly every 20ms-1.28s,
		// so without merging, a single device would produce dozens of rows
		// across one scan window. Capacity hint matches the spec's own
		// per-device packet-count estimate.
		var devices []BLEDeviceProfile
		seen := make(map[string]int, 32) // Address -> index into devices
		for adv := range advCh {
			if idx, ok := seen[adv.Address]; ok {
				// Repeat sighting of a known Address: merge into the
				// existing row instead of appending a new one, and don't
				// re-fire DeviceFound — it already fired on first sighting.
				// ctx is still honored here even though nothing is sent on
				// ch: without this check, a burst of repeat sightings for
				// one Address (the exact re-broadcast pattern this merge
				// exists to handle) would delay observing cancellation
				// until the next new Address or advCh's own close.
				select {
				case <-ctx.Done():
					return
				default:
				}
				existing := &devices[idx]
				// DistanceEstimate is live signal data, never a
				// placeholder — the latest reading is always the most
				// accurate "how close right now", so it's unconditionally
				// overwritten.
				existing.DistanceEstimate = FormatDistance(EstimateDistance(adv.RSSI, adv.TXPower))
				// Name/Vendor/DeviceType are identity fields: each has its
				// own "unresolved" placeholder ("" for Name, "unknown" for
				// Vendor/DeviceType, since DeriveVendor/ClassifyDeviceType
				// never return ""). Once a field resolves away from its
				// placeholder, a later, less-informative packet must never
				// blank it back — so the overwrite requires both that the
				// existing value is still the placeholder AND the freshly
				// derived one isn't (a whitespace-only Name must not count
				// as resolved, hence strutil.IsBlank rather than a bare "" check —
				// the same class of gap already fixed once for skip-reason
				// handling above).
				if strutil.IsBlank(existing.Name) && !strutil.IsBlank(adv.Name) {
					existing.Name = adv.Name
				}
				if vendor := DeriveVendor(adv); existing.Vendor == "unknown" && vendor != "unknown" {
					existing.Vendor = vendor
				}
				if deviceType := ClassifyDeviceType(adv); existing.DeviceType == DeviceTypeUnknown && deviceType != DeviceTypeUnknown {
					existing.DeviceType = deviceType
				}
				continue
			}

			profile := BLEDeviceProfile{
				Address: adv.Address,
				// Name is left exactly as adv.Name (possibly "") — an
				// absent/empty field is a Writer's job to render as an
				// explicit placeholder (AD-11, Story 4.7), not something
				// core/ble bakes in early. Vendor is the opposite: it
				// resolves its own "unknown" (AD-5) via DeriveVendor.
				Name:             adv.Name,
				Vendor:           DeriveVendor(adv),
				DistanceEstimate: FormatDistance(EstimateDistance(adv.RSSI, adv.TXPower)),
				DeviceType:       ClassifyDeviceType(adv),
			}
			seen[adv.Address] = len(devices)
			devices = append(devices, profile)
			select {
			case ch <- Event{Kind: EventKindDeviceFound, Device: profile}:
			case <-ctx.Done():
				return
			}
		}

		ch <- Event{
			Kind:   EventKindDone,
			Report: Report{Devices: devices},
		}
	}()

	return ch, nil
}
