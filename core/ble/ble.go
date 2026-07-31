package ble

import "context"

// Run is the BLE vertical's sole driving entrypoint (NL-AD-1). It never
// imports nats/core/engine, nats/cmd/..., or (once it exists)
// nats/core/wifimonitor.
func Run(ctx context.Context, opts Options) (<-chan Event, error) {
	ch := make(chan Event, 1)

	go func() {
		defer close(ch)

		s, ok := GetScanner()
		if !ok {
			ch <- Event{
				Kind: EventKindDone,
				Diagnostics: []Diagnostic{{
					Severity: "warning",
					Message:  "BLE scan skipped",
					Reason:   "no BLEScanner registered",
				}},
				Report: Report{},
			}
			return
		}

		probeOK, reason := s.Probe()
		if !probeOK {
			ch <- Event{
				Kind: EventKindDone,
				Diagnostics: []Diagnostic{{
					Severity: "warning",
					Message:  "BLE scan skipped",
					Reason:   reason,
				}},
				Report: Report{},
			}
			return
		}

		advCh, err := s.Scan(ctx, opts.Window)
		if err != nil {
			ch <- Event{
				Kind: EventKindDone,
				Diagnostics: []Diagnostic{{
					Severity: "warning",
					Message:  "BLE scan skipped",
					Reason:   err.Error(),
				}},
				Report: Report{},
			}
			return
		}

		var devices []BLEDeviceProfile
		for adv := range advCh {
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
			}
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
