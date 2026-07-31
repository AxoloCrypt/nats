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
// permission check only (NL-AD-4) — it never checks os.Geteuid() or any
// privilege/root signal, categorically different from the base spine's
// RequiresPrivilege()/root-probing pattern.
func (s *scanner) Probe() (ok bool, reason string) {
	if err := enableAdapter(); err != nil {
		return false, err.Error()
	}
	return true, ""
}

// Scan wraps tinygo's scan-result callback API into a <-chan
// ble.Advertisement, stopping the scan once window elapses or ctx is
// cancelled. It never calls Connect or any GATT method — only the
// scan/advertisement path is observed (NL-AD-2).
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

// Appearance encoding (Story 4.4 Task 2, the one real design decision this
// story makes): a raw 16-bit GAP Appearance value would be formatted here
// as a 4-digit lowercase hex string with no "0x" prefix (e.g. "03c0"),
// matching what core/ble.appearanceCategory (the sole read site) expects.
// It is never actually written below: the pinned tinygo.org/x/bluetooth
// v0.15.0's AdvertisementPayload interface exposes LocalName/ServiceUUIDs/
// ManufacturerData/ServiceData but has no Appearance accessor, and its
// Linux (BlueZ D-Bus) ScanResult mapping doesn't surface the "Appearance"
// device property either — the raw value simply isn't obtainable from a
// central-role scan with this dependency today. Advertisement.Appearance
// is therefore left at its documented zero value ("" — spine AD-5 marks it
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

	if mfg := result.ManufacturerData(); len(mfg) > 0 {
		companyID := mfg[0].CompanyID
		adv.CompanyID = &companyID
		adv.ManufacturerData = mfg[0].Data
	}

	return adv
}
