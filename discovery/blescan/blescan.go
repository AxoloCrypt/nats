package blescan

import (
	"context"
	"time"

	"nats/core/ble"

	"tinygo.org/x/bluetooth"
)

func init() {
	ble.RegisterScanner(&scanner{})
}

type scanner struct{}

// enableAdapter, scanAdapter, and stopScanAdapter are swappable so Probe()
// and Scan() can be exercised in tests without real Bluetooth hardware —
// mirrors discovery/arp/arp.go's netInterfaces/ifaceAddrs testability
// convention.
var enableAdapter = func() error {
	return bluetooth.DefaultAdapter.Enable()
}

var scanAdapter = func(callback func(bluetooth.ScanResult)) error {
	return bluetooth.DefaultAdapter.Scan(func(_ *bluetooth.Adapter, result bluetooth.ScanResult) {
		callback(result)
	})
}

var stopScanAdapter = func() error {
	return bluetooth.DefaultAdapter.StopScan()
}

// Probe empirically attempts to enable the adapter and reports ok=false
// with a human-readable reason on failure. This is an OS Bluetooth-
// permission check only — it never checks os.Geteuid() or any
// privilege/root signal, categorically different from the LAN vertical's
// RequiresPrivilege()/root-probing pattern.
func (s *scanner) Probe() (ok bool, reason string) {
	if err := enableAdapter(); err != nil {
		return false, err.Error()
	}
	return true, ""
}

// scanStartGrace bounds how long Scan waits to learn whether the scan
// actually started before handing the channel back to core/ble.Run.
//
// It exists because the permission and availability gaps that must be
// reported to the user do not surface from Probe(). In the pinned
// tinygo.org/x/bluetooth v0.15.0,
// Adapter.Enable() only opens the system D-Bus and reads
// org.bluez.Adapter1.Address on Linux — which succeeds when BlueZ is present
// but Bluetooth is powered off — and on Windows is nothing but
// ole.RoInitialize, never touching Bluetooth at all. "Bluetooth is switched
// off" and "scanning is denied by policy" are both reported by Adapter.Scan
// instead, which fails without ever reaching its event loop and so returns
// almost immediately. Waiting briefly for that failure is what lets it reach
// core/ble.Run's err != nil branch and be named in a warning Diagnostic.
// Without the wait the error was discarded and the user saw a zero-device
// "BLE scan complete." that is indistinguishable from an empty room — a
// skipped scan must never be silently dropped from the output.
//
// This is a bound, not a guarantee: a rejection slower than the grace is
// still missed and still degrades to a silent empty scan. An exact fix needs
// a start-confirmation signal, which v0.15.0's Scan does not expose — it
// fuses starting and running into one blocking call.
const scanStartGrace = 300 * time.Millisecond

// Scan wraps tinygo's scan-result callback API into a <-chan
// ble.Advertisement, stopping the scan once window elapses or ctx is
// cancelled. It never calls Connect or any GATT method — only the
// scan/advertisement path is observed, so scanning stays strictly passive.
//
// A scan that fails to start is reported as an error rather than as an empty
// channel, so core/ble.Run can name it (see scanStartGrace). The cost is that
// a successful scan takes scanStartGrace longer than window overall, since the
// handshake precedes the listening window rather than overlapping it.
func (s *scanner) Scan(ctx context.Context, window time.Duration) (<-chan ble.Advertisement, error) {
	ch := make(chan ble.Advertisement)
	scanDone := make(chan error, 1)

	go func() {
		scanDone <- scanAdapter(func(result bluetooth.ScanResult) {
			select {
			case ch <- toAdvertisement(result):
			case <-ctx.Done():
			}
		})
	}()

	startGrace := time.NewTimer(scanStartGrace)
	defer startGrace.Stop()

	select {
	case err := <-scanDone:
		// scanAdapter returned before the scan could plausibly still be
		// running, so it never got going. Closing ch here is safe precisely
		// because scanAdapter has returned: no further callback can fire, so
		// nothing can send on a closed channel.
		close(ch)
		if err != nil {
			return nil, err
		}
		// A nil error this early means the scan started and stopped on its
		// own without observing anything — unusual, but not a failure, and
		// an already-closed ch drains to zero advertisements.
		return ch, nil
	case <-ctx.Done():
		_ = stopScanAdapter()
		go func() {
			defer close(ch)
			<-scanDone
		}()
		return ch, nil
	case <-startGrace.C:
		// Still running after the grace: treat the scan as started and let
		// the lifecycle goroutine below own the window.
	}

	go func() {
		// Waiting on scanDone a second time after stopping ensures the
		// underlying Scan callback goroutine has fully returned before ch is
		// closed, so a callback already in flight can never send on a
		// closed channel.
		defer close(ch)
		timer := time.NewTimer(window)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			_ = stopScanAdapter()
		case <-timer.C:
			_ = stopScanAdapter()
		case <-scanDone:
			return
		}
		<-scanDone
	}()

	return ch, nil
}

// Appearance encoding: a raw 16-bit GAP Appearance value would be formatted
// here as a 4-digit lowercase hex string with no "0x" prefix (e.g. "03c0"),
// matching what core/ble.appearanceCategory (the sole read site) expects.
// It is never actually written below: the pinned tinygo.org/x/bluetooth
// v0.15.0's AdvertisementPayload interface exposes LocalName/ServiceUUIDs/
// ManufacturerData/ServiceData but has no Appearance accessor, and its
// Linux (BlueZ D-Bus) ScanResult mapping doesn't surface the "Appearance"
// device property either — the raw value simply isn't obtainable from a
// central-role scan with this dependency today. Advertisement.Appearance
// is therefore left at its documented zero value ("" — the field is
// optional); core/ble.ClassifyDeviceType degrades to its next signal
// (ServiceUUIDs, then Name) whenever Appearance doesn't decode, so this
// gap doesn't block classification. If a future tinygo release (or
// platform-specific adapter) exposes the raw value, populate it here using
// the encoding documented above.
func toAdvertisement(result bluetooth.ScanResult) ble.Advertisement {
	adv := ble.Advertisement{
		Address: result.Address.String(),
		Name:    result.LocalName(),
		RSSI:    int(result.RSSI),
	}

	for _, uuid := range result.ServiceUUIDs() {
		adv.ServiceUUIDs = append(adv.ServiceUUIDs, uuid.String())
	}

	// An advertisement may legitimately carry several manufacturer-data
	// elements (dual-stack beacons commonly pair an Apple entry with a vendor
	// one), but Advertisement holds a single CompanyID, so exactly one has to
	// be chosen. The lowest CompanyID wins — an arbitrary rule, but a *stable*
	// one, and stability is the whole point: tinygo builds this slice by
	// ranging a Go map (gap_linux.go's makeScanResult), whose iteration order
	// is randomized per range. Taking mfg[0] therefore resolved the same
	// physical device to a different Vendor on different callbacks within a
	// single scan. The remaining elements are dropped; carrying all of them
	// would need a shape change to Advertisement, whose layout is pinned.
	if mfg := result.ManufacturerData(); len(mfg) > 0 {
		chosen := mfg[0]
		for _, el := range mfg[1:] {
			if el.CompanyID < chosen.CompanyID {
				chosen = el
			}
		}
		companyID := chosen.CompanyID
		adv.CompanyID = &companyID
		adv.ManufacturerData = chosen.Data
	}

	return adv
}
