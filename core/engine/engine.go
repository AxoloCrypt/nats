package engine

import (
	"context"
	"fmt"
	"reflect"
	"time"
)

func DefaultOptions() Options {
	return Options{
		Techniques: []string{"arp"},
		// The default set of enrichers (FR-5, AD-12): "dns" and "oui" from
		// Story 2.1, "tcpconnect" from Story 2.2.
		EnrichOptions: []string{"dns", "oui", "tcpconnect"},
		// OutputFormat default carried over from Story 1.7, when "table" was
		// the only format (AD-7).
		OutputFormat: "table",
	}
}

// safetyNetTimeout is a defect-only backstop applied to listen-based
// techniques (mdns, ssdp). Those techniques are responsible for closing
// their own Sighting channel once a quiescence period with no new sighting
// has elapsed (AD-13); this timeout exists only to bound a regression where
// that self-termination logic fails to fire, never as part of normal
// completion. Sweep-based techniques (those implementing AddressEnumerator)
// close their own channel once every address has been probed and are never
// wrapped with this timeout — they keep running on the raw parent ctx,
// exactly as before this change, so a large/lossy subnet is never silently
// truncated.
var safetyNetTimeout = 5 * time.Minute

// enrichTimeout bounds the entire post-merge enrichment step (all enrichers,
// all devices). Enrichment runs on ctx with no deadline of its own otherwise
// (cmd/cli passes context.Background()), so a slow/unreachable DNS resolver
// could otherwise stall a scan that would have finished in under a second.
var enrichTimeout = 30 * time.Second

// deviceKey identifies a Device the same way Merge resolves identity (AD-4):
// MAC when present, otherwise IP.
func deviceKey(d Device) string {
	if d.MAC != "" {
		return "mac:" + d.MAC
	}
	return "ip:" + d.IP
}

// recordDeviceEvent emits DeviceFound the first time a device's identity is
// seen and DeviceUpdated whenever a later merge changes an already-known
// device's fields. A device first observed without a MAC (e.g. by a
// listen-based technique) that later gets one resolved (e.g. by arp) keeps
// the same physical identity even though its key changes — that transition
// is reported as an update, not a second DeviceFound.
func recordDeviceEvent(ch chan<- Event, known map[string]Device, d Device, technique string) {
	key := deviceKey(d)

	if prev, ok := known[key]; ok {
		if !reflect.DeepEqual(prev, d) {
			known[key] = d
			ch <- Event{Kind: EventKindDeviceUpdated, Technique: technique, Device: d}
		}
		return
	}

	if d.MAC != "" {
		ipOnlyKey := "ip:" + d.IP
		if _, hadIPOnly := known[ipOnlyKey]; hadIPOnly {
			delete(known, ipOnlyKey)
			known[key] = d
			ch <- Event{Kind: EventKindDeviceUpdated, Technique: technique, Device: d}
			return
		}
	}

	known[key] = d
	ch <- Event{Kind: EventKindDeviceFound, Technique: technique, Device: d}
}

// enrichDevices runs every enricher named in enrichNames, in order, against
// every Device (FR-5, AD-12). Each enricher receives the previous enricher's
// output, so a later write to the same field (e.g. Hostname) overrides an
// earlier one — the keep-vs-overwrite decision for a given field is made
// inside each enricher (only a successful resolution overwrites), not here;
// this loop just applies enrichers in order (AD-10). A name with no matching
// registry entry is skipped with a warning diagnostic (not silently) — this
// is how a real registration regression (e.g. a dropped blank import) gets
// caught.
func enrichDevices(ctx context.Context, devices []Device, enrichNames []string, ch chan<- Event) ([]Device, []Diagnostic) {
	var diags []Diagnostic
	result := make([]Device, len(devices))
	copy(result, devices)

	for _, name := range enrichNames {
		enricher, ok := GetEnricher(name)
		if !ok {
			diags = append(diags, Diagnostic{
				Severity: "warning",
				Message:  fmt.Sprintf("enricher %q not found in registry", name),
			})
			continue
		}

		if enricher.RequiresPrivilege() {
			reason := fmt.Sprintf("enricher %q requires privilege not available to current process", name)
			if prober, ok := enricher.(PrivilegeProber); ok {
				if _, err := prober.ProbePrivilege(); err != nil {
					reason = fmt.Sprintf("enricher %q: %s", name, err.Error())
				}
			}
			ch <- Event{Kind: EventKindTechniqueSkipped, Technique: name, Reason: reason}
			diags = append(diags, Diagnostic{
				Severity: "warning",
				Message:  fmt.Sprintf("%s skipped", name),
				Reason:   reason,
			})
			continue
		}

		// Reuses TechniqueStarted (AD-3 pins the Event.Kind enumeration) so a
		// driving adapter's existing progress display keeps moving while an
		// enricher runs, rather than going static for the whole enrichment
		// phase.
		ch <- Event{Kind: EventKindTechniqueStarted, Technique: name}

		for i, d := range result {
			enriched, err := enricher.Enrich(ctx, d)
			if err != nil {
				diags = append(diags, Diagnostic{
					Severity: "warning",
					Message:  fmt.Sprintf("%s enrichment failed for device %s", name, deviceKey(d)),
					Reason:   err.Error(),
				})
				continue
			}
			result[i] = enriched
		}
	}

	return result, diags
}

func Run(ctx context.Context, opts Options) (<-chan Event, error) {
	subnet, diag := resolveSubnet(opts)
	ch := make(chan Event, 100)

	go func() {
		defer close(ch)

		diags := []Diagnostic{}
		hasError := false
		if diag != nil {
			diags = append(diags, *diag)
			hasError = true
		}

		techniques := opts.Techniques
		if len(techniques) == 0 {
			techniques = DefaultOptions().Techniques
		}

		var sightings []Sighting
		probedAddresses := make(map[string]bool)
		knownDevices := make(map[string]Device)
		sweptAddresses := make(map[string]bool)
		var devices []Device
		var mergeDiags []Diagnostic

		for _, name := range techniques {
			tech, ok := GetTechnique(name)
			if !ok {
				diags = append(diags, Diagnostic{
					Severity: "error",
					Message:  fmt.Sprintf("technique %q not found in registry", name),
				})
				hasError = true
				continue
			}

			if tech.RequiresPrivilege() {
				reason := fmt.Sprintf("technique %q requires privilege not available to current process", name)
				if prober, ok := tech.(PrivilegeProber); ok {
					if _, err := prober.ProbePrivilege(); err != nil {
						reason = fmt.Sprintf("technique %q: %s", name, err.Error())
					}
				}
				ch <- Event{Kind: EventKindTechniqueSkipped, Technique: name, Reason: reason}
				diags = append(diags, Diagnostic{
					Severity: "warning",
					Message:  fmt.Sprintf("%s skipped", name),
					Reason:   reason,
				})
				continue
			}

			enumerator, isSweepBased := tech.(AddressEnumerator)
			if isSweepBased {
				if addrs, err := enumerator.EnumerateAddresses(subnet); err == nil {
					for _, a := range addrs {
						sweptAddresses[a] = true
					}
				}
			}
			ch <- Event{Kind: EventKindTechniqueStarted, Technique: name, TotalAddresses: len(sweptAddresses)}

			techCtx := ctx
			var cancel context.CancelFunc
			if !isSweepBased {
				techCtx, cancel = context.WithTimeout(ctx, safetyNetTimeout)
			}

			sightingCh, err := tech.Run(techCtx, subnet)
			if err != nil {
				if cancel != nil {
					cancel()
				}
				reason := fmt.Sprintf("technique %q: %s", name, err.Error())
				ch <- Event{Kind: EventKindTechniqueSkipped, Technique: name, Reason: reason}
				diags = append(diags, Diagnostic{
					Severity: "warning",
					Message:  reason,
				})
				continue
			}

			for s := range sightingCh {
				sightings = append(sightings, s)
				if !probedAddresses[s.IP] {
					probedAddresses[s.IP] = true
					ch <- Event{Kind: EventKindAddressProbed, Technique: name, Address: s.IP}
				}
			}
			if cancel != nil {
				cancel()
			}

			// Re-resolve devices now that this technique's Sightings are in,
			// so DeviceFound/DeviceUpdated can be emitted live rather than in
			// one batch at the very end (AC1/AC2, AD-13).
			devices, mergeDiags = Merge(sightings)
			for _, d := range devices {
				recordDeviceEvent(ch, knownDevices, d, "merge")
			}
		}

		diags = append(diags, mergeDiags...)
		if len(mergeDiags) > 0 {
			hasError = true
		}

		enrichNames := opts.EnrichOptions
		if len(enrichNames) == 0 {
			enrichNames = DefaultOptions().EnrichOptions
		}
		enrichCtx, enrichCancel := context.WithTimeout(ctx, enrichTimeout)
		var enrichDiags []Diagnostic
		devices, enrichDiags = enrichDevices(enrichCtx, devices, enrichNames, ch)
		enrichCancel()
		diags = append(diags, enrichDiags...)
		for _, d := range devices {
			recordDeviceEvent(ch, knownDevices, d, "enrich")
		}

		// Classification runs exactly once per Device, strictly after all
		// requested enrichment has completed and strictly before Done fires
		// (AD-9, AD-13) — never inside a discovery/* or enrich/* adapter, so
		// it always sees the complete, fully-merged, fully-enriched signal
		// set. Gated into the Done wait the same way enrichment is above:
		// recordDeviceEvent surfaces the now-populated DeviceType as a
		// DeviceUpdated before Done, rather than only in the final Report.
		for i, d := range devices {
			devices[i].DeviceType = Classify(d)
		}
		for _, d := range devices {
			recordDeviceEvent(ch, knownDevices, d, "classify")
		}

		// Only surfaced when nothing else has already reported an error —
		// e.g. "no active network interface found" or an unknown technique
		// name already explains the empty Report, and a second, more
		// generic error would be redundant and potentially misleading.
		if !hasError && len(devices) == 0 {
			diags = append(diags, Diagnostic{
				Severity: "error",
				Message:  "no devices discovered",
				Reason:   "the scan completed without any technique reporting a reachable device; check network connectivity or try different --techniques",
			})
		}

		ch <- Event{
			Kind:        EventKindDone,
			Diagnostics: diags,
			Report: Report{
				Devices:     devices,
				Diagnostics: diags,
			},
		}
	}()

	return ch, nil
}
