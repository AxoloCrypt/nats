//go:build linux

package blescan

import (
	"testing"

	"github.com/godbus/dbus/v5"

	"nats/core/ble"

	"tinygo.org/x/bluetooth"
)

func TestAddressFromDevicePath(t *testing.T) {
	cases := []struct {
		name string
		path string
		want string
		ok   bool
	}{
		{"uppercase hex", "/org/bluez/hci0/dev_AA_BB_CC_DD_EE_FF", "AA:BB:CC:DD:EE:FF", true},
		{"lowercase hex normalizes to uppercase", "/org/bluez/hci0/dev_00_1a_7d_da_71_13", "00:1A:7D:DA:71:13", true},
		{"no dev_ segment", "/org/bluez/hci0", "", false},
		{"too short to be a MAC", "/org/bluez/hci0/dev_AA_BB", "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := addressFromDevicePath(dbus.ObjectPath(c.path))
			if ok != c.ok || got != c.want {
				t.Fatalf("addressFromDevicePath(%q) = (%q, %v), want (%q, %v)", c.path, got, ok, c.want, c.ok)
			}
		})
	}
}

// TestTXPowerTracker_RecordAndLookup simulates the BlueZ device D-Bus
// properties map (org.bluez.Device1) that a real GetManagedObjects call or
// PropertiesChanged/InterfacesAdded signal would carry, and confirms a
// populated TxPower flows into a lookup keyed by the Address derived from
// the device's object path.
func TestTXPowerTracker_RecordAndLookup(t *testing.T) {
	tracker := newTXPowerTracker()
	path := dbus.ObjectPath("/org/bluez/hci0/dev_AA_BB_CC_DD_EE_FF")
	props := map[string]dbus.Variant{
		"Address":     dbus.MakeVariant("AA:BB:CC:DD:EE:FF"),
		"AddressType": dbus.MakeVariant("public"),
		"RSSI":        dbus.MakeVariant(int16(-70)),
		"TxPower":     dbus.MakeVariant(int16(-59)),
	}

	tracker.record(path, props)

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

// TestTXPowerTracker_RecordIgnoresPropsWithoutTxPower is AC #5's proof at the
// tracker level: a device properties map with no TxPower key (the common
// case — TxPower is an optional BlueZ property) must never fabricate a
// value.
func TestTXPowerTracker_RecordIgnoresPropsWithoutTxPower(t *testing.T) {
	tracker := newTXPowerTracker()
	path := dbus.ObjectPath("/org/bluez/hci0/dev_AA_BB_CC_DD_EE_FF")
	props := map[string]dbus.Variant{
		"Address": dbus.MakeVariant("AA:BB:CC:DD:EE:FF"),
		"RSSI":    dbus.MakeVariant(int16(-70)),
	}

	tracker.record(path, props)

	if got := tracker.lookup("AA:BB:CC:DD:EE:FF"); got != nil {
		t.Fatalf("expected nil when TxPower isn't in props, got %v", got)
	}
}

// TestTXPowerTracker_RecordFromPropertiesChangedWithoutAddressKey covers the
// case the Dev Notes flag as the real-world common one: a PropertiesChanged
// signal's changes map carries only the properties that actually changed
// (TxPower arriving after the device was already known), with no "Address"
// entry at all. record must still resolve the device via the object path.
func TestTXPowerTracker_RecordFromPropertiesChangedWithoutAddressKey(t *testing.T) {
	tracker := newTXPowerTracker()
	path := dbus.ObjectPath("/org/bluez/hci0/dev_00_1A_7D_DA_71_13")
	changes := map[string]dbus.Variant{
		"TxPower": dbus.MakeVariant(int16(-4)),
	}

	tracker.record(path, changes)

	got := tracker.lookup("00:1A:7D:DA:71:13")
	if got == nil || *got != -4 {
		t.Fatalf("expected TxPower -4, got %v", got)
	}
}

// TestTXPowerTracker_HandleSignal_PropertiesChangedRecordsTxPower exercises
// handleSignal's dispatch directly against a constructed *dbus.Signal, the
// same shape bus.Signal delivers over the channel in run — no real D-Bus
// connection is needed since handleSignal never touches bus itself.
func TestTXPowerTracker_HandleSignal_PropertiesChangedRecordsTxPower(t *testing.T) {
	tracker := newTXPowerTracker()
	sig := &dbus.Signal{
		Name: "org.freedesktop.DBus.Properties.PropertiesChanged",
		Path: dbus.ObjectPath("/org/bluez/hci0/dev_AA_BB_CC_DD_EE_FF"),
		Body: []interface{}{
			"org.bluez.Device1",
			map[string]dbus.Variant{"TxPower": dbus.MakeVariant(int16(-59))},
			[]string{},
		},
	}

	tracker.handleSignal(sig)

	got := tracker.lookup("AA:BB:CC:DD:EE:FF")
	if got == nil || *got != -59 {
		t.Fatalf("expected TxPower -59, got %v", got)
	}
}

func TestTXPowerTracker_HandleSignal_PropertiesChangedIgnoresOtherInterface(t *testing.T) {
	tracker := newTXPowerTracker()
	sig := &dbus.Signal{
		Name: "org.freedesktop.DBus.Properties.PropertiesChanged",
		Path: dbus.ObjectPath("/org/bluez/hci0/dev_AA_BB_CC_DD_EE_FF"),
		Body: []interface{}{
			"org.bluez.Battery1",
			map[string]dbus.Variant{"TxPower": dbus.MakeVariant(int16(-59))},
			[]string{},
		},
	}

	tracker.handleSignal(sig)

	if got := tracker.lookup("AA:BB:CC:DD:EE:FF"); got != nil {
		t.Fatalf("expected nil for a PropertiesChanged on an unrelated interface, got %v", got)
	}
}

func TestTXPowerTracker_HandleSignal_PropertiesChangedShortBodyIgnored(t *testing.T) {
	tracker := newTXPowerTracker()
	sig := &dbus.Signal{
		Name: "org.freedesktop.DBus.Properties.PropertiesChanged",
		Path: dbus.ObjectPath("/org/bluez/hci0/dev_AA_BB_CC_DD_EE_FF"),
		Body: []interface{}{
			"org.bluez.Device1",
			map[string]dbus.Variant{"TxPower": dbus.MakeVariant(int16(-59))},
		},
	}

	tracker.handleSignal(sig)

	if got := tracker.lookup("AA:BB:CC:DD:EE:FF"); got != nil {
		t.Fatalf("expected nil for a PropertiesChanged body missing invalidated_properties, got %v", got)
	}
}

// TestTXPowerTracker_HandleSignal_PropertiesChangedInvalidatesTxPower is the
// invalidated_properties proof: BlueZ can signal that TxPower is no longer
// valid without repeating it in changed_properties, and the tracker must
// stop serving the stale cached value when that happens.
func TestTXPowerTracker_HandleSignal_PropertiesChangedInvalidatesTxPower(t *testing.T) {
	tracker := newTXPowerTracker()
	path := dbus.ObjectPath("/org/bluez/hci0/dev_AA_BB_CC_DD_EE_FF")
	tracker.record(path, map[string]dbus.Variant{"TxPower": dbus.MakeVariant(int16(-59))})

	sig := &dbus.Signal{
		Name: "org.freedesktop.DBus.Properties.PropertiesChanged",
		Path: path,
		Body: []interface{}{
			"org.bluez.Device1",
			map[string]dbus.Variant{},
			[]string{"TxPower"},
		},
	}
	tracker.handleSignal(sig)

	if got := tracker.lookup("AA:BB:CC:DD:EE:FF"); got != nil {
		t.Fatalf("expected nil after TxPower was invalidated, got %v", got)
	}
}

func TestTXPowerTracker_HandleSignal_InterfacesAddedRecordsTxPower(t *testing.T) {
	tracker := newTXPowerTracker()
	sig := &dbus.Signal{
		Name: "org.freedesktop.DBus.ObjectManager.InterfacesAdded",
		Body: []interface{}{
			dbus.ObjectPath("/org/bluez/hci0/dev_AA_BB_CC_DD_EE_FF"),
			map[string]map[string]dbus.Variant{
				"org.bluez.Device1": {"TxPower": dbus.MakeVariant(int16(-42))},
			},
		},
	}

	tracker.handleSignal(sig)

	got := tracker.lookup("AA:BB:CC:DD:EE:FF")
	if got == nil || *got != -42 {
		t.Fatalf("expected TxPower -42, got %v", got)
	}
}

// TestTXPowerTracker_HandleSignal_InterfacesRemovedForgetsDevice is
// InterfacesRemoved's proof: a device BlueZ reports as gone must stop
// serving its last-known TxPower rather than caching it forever.
func TestTXPowerTracker_HandleSignal_InterfacesRemovedForgetsDevice(t *testing.T) {
	tracker := newTXPowerTracker()
	path := dbus.ObjectPath("/org/bluez/hci0/dev_AA_BB_CC_DD_EE_FF")
	tracker.record(path, map[string]dbus.Variant{"TxPower": dbus.MakeVariant(int16(-59))})

	sig := &dbus.Signal{
		Name: "org.freedesktop.DBus.ObjectManager.InterfacesRemoved",
		Body: []interface{}{
			path,
			[]string{"org.bluez.Device1"},
		},
	}
	tracker.handleSignal(sig)

	if got := tracker.lookup("AA:BB:CC:DD:EE:FF"); got != nil {
		t.Fatalf("expected nil after InterfacesRemoved for org.bluez.Device1, got %v", got)
	}
}

func TestTXPowerTracker_HandleSignal_InterfacesRemovedIgnoresOtherInterface(t *testing.T) {
	tracker := newTXPowerTracker()
	path := dbus.ObjectPath("/org/bluez/hci0/dev_AA_BB_CC_DD_EE_FF")
	tracker.record(path, map[string]dbus.Variant{"TxPower": dbus.MakeVariant(int16(-59))})

	sig := &dbus.Signal{
		Name: "org.freedesktop.DBus.ObjectManager.InterfacesRemoved",
		Body: []interface{}{
			path,
			[]string{"org.bluez.Battery1"},
		},
	}
	tracker.handleSignal(sig)

	got := tracker.lookup("AA:BB:CC:DD:EE:FF")
	if got == nil || *got != -59 {
		t.Fatalf("expected TxPower to survive an InterfacesRemoved not naming org.bluez.Device1, got %v", got)
	}
}

func TestTXPowerTracker_HandleSignal_UnknownSignalNameIgnored(t *testing.T) {
	tracker := newTXPowerTracker()
	sig := &dbus.Signal{
		Name: "org.freedesktop.DBus.NameOwnerChanged",
		Body: []interface{}{"org.bluez", "", ":1.23"},
	}

	tracker.handleSignal(sig)
}

// TestToAdvertisement_PopulatesTXPowerFromTrackerAndNarrowsUncertainty is the
// end-to-end correlation proof AC #1 and #7 require: a BlueZ device
// properties map carrying TxPower flows through the tracker into
// Advertisement.TXPower, and that populated value takes
// core/ble.EstimateDistance's tighter knownTXUncertaintyFactor branch rather
// than the wide assumedTXUncertaintyFactor fallback — mirrors
// core/ble/distance_test.go's TestEstimateDistance_NilTXPowerWidensUncertainty
// table style.
func TestToAdvertisement_PopulatesTXPowerFromTrackerAndNarrowsUncertainty(t *testing.T) {
	tracker := newTXPowerTracker()
	path := dbus.ObjectPath("/org/bluez/hci0/dev_AA_BB_CC_DD_EE_FF")
	tracker.record(path, map[string]dbus.Variant{
		"TxPower": dbus.MakeVariant(int16(-59)),
	})

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

// TestToAdvertisement_NoTXPowerLeavesTXPowerNil is AC #5's proof at the
// toAdvertisement level: a device the tracker never recorded (never
// advertised TxPower) must still leave Advertisement.TXPower nil, exactly as
// before this tracker existed — no fabricated value, no regression to the
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
