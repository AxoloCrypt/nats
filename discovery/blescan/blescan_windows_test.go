//go:build windows

package blescan

import (
	"testing"

	"nats/core/ble"

	"tinygo.org/x/bluetooth"
)

func TestAddressFromRaw(t *testing.T) {
	cases := []struct {
		name string
		raw  uint64
		want string
	}{
		{"typical address", 0xAABBCCDDEEFF, "AA:BB:CC:DD:EE:FF"},
		{"lowercase-looking bytes render uppercase", 0x001A7DDA7113, "00:1A:7D:DA:71:13"},
		{"zero address", 0, "00:00:00:00:00:00"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := addressFromRaw(c.raw); got != c.want {
				t.Fatalf("addressFromRaw(%#x) = %q, want %q", c.raw, got, c.want)
			}
		})
	}
}

func TestReferenceInt16Value(t *testing.T) {
	cases := []struct {
		name       string
		value      int16
		upperNoise uintptr
		want       int
	}{
		{"positive value", 59, 0, 59},
		{"negative value", -59, 0, -59},
		{"zero", 0, 0, 0},
		{"upper bits beyond the low 16 are ignored", -4, 0xFFFF0000, -4},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			raw := c.upperNoise | uintptr(uint16(c.value))
			if got := referenceInt16Value(raw); got != c.want {
				t.Fatalf("referenceInt16Value(%#x) = %d, want %d", raw, got, c.want)
			}
		})
	}
}

func TestTXPowerTracker_RecordAndLookup(t *testing.T) {
	tracker := newTXPowerTracker()
	tracker.record("AA:BB:CC:DD:EE:FF", -59)

	got := tracker.lookup("AA:BB:CC:DD:EE:FF")
	if got == nil || *got != -59 {
		t.Fatalf("expected TxPower -59, got %v", got)
	}
}

func TestTXPowerTracker_LookupMissesUnknownAddress(t *testing.T) {
	tracker := newTXPowerTracker()
	if got := tracker.lookup("AA:BB:CC:DD:EE:FF"); got != nil {
		t.Fatalf("expected nil for an address never recorded, got %v", got)
	}
}

// TestToAdvertisement_PopulatesTXPowerFromTrackerAndNarrowsUncertainty is
// the end-to-end correlation proof AC #1 and #7 require, mirroring
// blescan_linux_test.go's test of the same name: a raw Bluetooth address +
// TX power recorded into the tracker (as the watcher's Received callback
// would) flows through txPowerFor into Advertisement.TXPower for a
// tinygo.org/x/bluetooth ScanResult carrying the identical address, and
// that populated value takes core/ble.EstimateDistance's tighter
// knownTXUncertaintyFactor branch rather than the wide
// assumedTXUncertaintyFactor fallback.
func TestToAdvertisement_PopulatesTXPowerFromTrackerAndNarrowsUncertainty(t *testing.T) {
	tracker := newTXPowerTracker()
	const rawAddr = 0xAABBCCDDEEFF
	tracker.record(addressFromRaw(rawAddr), -59)

	origLookup := txPowerFor
	defer func() { txPowerFor = origLookup }()
	txPowerFor = tracker.lookup

	mac, err := bluetooth.ParseMAC("AA:BB:CC:DD:EE:FF")
	if err != nil {
		t.Fatalf("ParseMAC: %v", err)
	}
	result := bluetooth.ScanResult{
		Address:              bluetooth.Address{MACAddress: bluetooth.MACAddress{MAC: mac}},
		RSSI:                 -70,
		AdvertisementPayload: fakePayload{},
	}

	adv := toAdvertisement(result)
	if adv.TXPower == nil || *adv.TXPower != -59 {
		t.Fatalf("expected Advertisement.TXPower -59, got %v", adv.TXPower)
	}

	metersKnown, uncertaintyKnown := ble.EstimateDistance(adv.RSSI, adv.TXPower)
	metersAssumed, uncertaintyAssumed := ble.EstimateDistance(adv.RSSI, nil)

	proportionKnown := uncertaintyKnown / metersKnown
	proportionAssumed := uncertaintyAssumed / metersAssumed
	if proportionAssumed <= proportionKnown {
		t.Fatalf("expected the tracked TXPower to take the tighter knownTXUncertaintyFactor branch, got proportion %v (known) vs %v (assumed fallback)", proportionKnown, proportionAssumed)
	}
}

// TestToAdvertisement_NoTXPowerLeavesTXPowerNil is AC #7's proof at the
// toAdvertisement level: a device the tracker never recorded (its
// advertisement never carried TransmitPowerLevelInDBm) must still leave
// Advertisement.TXPower nil — no fabricated value, no regression to the
// assumedTXUncertaintyFactor fallback path.
func TestToAdvertisement_NoTXPowerLeavesTXPowerNil(t *testing.T) {
	tracker := newTXPowerTracker()

	origLookup := txPowerFor
	defer func() { txPowerFor = origLookup }()
	txPowerFor = tracker.lookup

	mac, err := bluetooth.ParseMAC("11:22:33:44:55:66")
	if err != nil {
		t.Fatalf("ParseMAC: %v", err)
	}
	result := bluetooth.ScanResult{
		Address:              bluetooth.Address{MACAddress: bluetooth.MACAddress{MAC: mac}},
		RSSI:                 -70,
		AdvertisementPayload: fakePayload{},
	}

	adv := toAdvertisement(result)
	if adv.TXPower != nil {
		t.Fatalf("expected nil TXPower for a device the tracker never recorded, got %v", *adv.TXPower)
	}
}
