//go:build windows

package blescan

import (
	"syscall"
	"testing"
	"unsafe"

	"nats/core/ble"

	"github.com/go-ole/go-ole"
	"github.com/saltosystems/winrt-go/windows/devices/bluetooth/advertisement"
	"github.com/saltosystems/winrt-go/windows/foundation"
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

// --- COM interop test doubles ---
//
// txPowerFromArgs and handleReceived only ever touch their COM arguments
// through vtable slots reached via syscall.SyscallN against whatever
// RawVTable a pointer happens to point to — the same mechanism winrt-go's
// own generated wrappers use, and the only thing the real WinRT runtime
// itself would drive them through on a live host. A test double built from
// syscall.NewCallback stand-ins for exactly those slots satisfies the same
// ABI contract, so the code under test cannot tell it apart from a real
// WinRT object: this drives the real interop code — the vtable walk, the
// QueryInterface for interface 2, the IReference<Int16> unboxing — rather
// than re-implementing it. Unused vtable slots (AddRef, GetIIds, ...) are
// left as zero uintptrs and are never invoked by the paths under test.

// fakeInterface1 and fakeInterface1Vtbl mirror winrt-go's unexported
// iBluetoothLEAdvertisementReceivedEventArgs/...Vtbl (interface 1: RSSI,
// Address, AdvertisementType, Timestamp, Advertisement) closely enough to
// stand in for it: identical field order/sizes, so the real
// GetBluetoothAddress() wrapper in the advertisement package — which
// reinterprets RawVTable as that exact unexported type — walks the same
// memory layout. Only Release and GetBluetoothAddress ever get a live
// callback here.
type fakeInterface1 struct {
	ole.IInspectable
}

type fakeInterface1Vtbl struct {
	ole.IInspectableVtbl
	GetRawSignalStrengthInDBm uintptr
	GetBluetoothAddress       uintptr
	GetAdvertisementType      uintptr
	GetTimestamp              uintptr
	GetAdvertisement          uintptr
}

func newFakeInterface1(address uint64) *fakeInterface1 {
	vtbl := &fakeInterface1Vtbl{}
	vtbl.Release = syscall.NewCallback(func(this unsafe.Pointer) uintptr { return 0 })
	vtbl.GetBluetoothAddress = syscall.NewCallback(func(this, out unsafe.Pointer) uintptr {
		*(*uint64)(out) = address
		return 0
	})
	return &fakeInterface1{
		IInspectable: ole.IInspectable{IUnknown: ole.IUnknown{RawVTable: (*interface{})(unsafe.Pointer(vtbl))}},
	}
}

// newFakeInterface2 fakes IBluetoothLEAdvertisementReceivedEventArgs2 —
// the exact interface txPowerFromArgs QueryInterfaces for — reusing this
// package's own iBluetoothLEAdvertisementReceivedEventArgs2Vtbl (the real
// vtable-shaped struct blescan_windows.go calls through) rather than a
// re-declared copy. getTX supplies the GetTransmitPowerLevelInDBm result:
// the IReference pointer to write into the out-param (nil simulates "this
// advertisement doesn't carry TX power") and the HRESULT to return.
func newFakeInterface2(getTX func() (unsafe.Pointer, uintptr)) *iBluetoothLEAdvertisementReceivedEventArgs2 {
	vtbl := &iBluetoothLEAdvertisementReceivedEventArgs2Vtbl{}
	vtbl.Release = syscall.NewCallback(func(this unsafe.Pointer) uintptr { return 0 })
	vtbl.GetTransmitPowerLevelInDBm = syscall.NewCallback(func(this, out unsafe.Pointer) uintptr {
		ptr, hr := getTX()
		*(*unsafe.Pointer)(out) = ptr
		return hr
	})
	return &iBluetoothLEAdvertisementReceivedEventArgs2{
		IInspectable: ole.IInspectable{IUnknown: ole.IUnknown{RawVTable: (*interface{})(unsafe.Pointer(vtbl))}},
	}
}

// int16ABIWord is referenceInt16Value's inverse: it encodes a signed dBm
// value the same way the real IReference<Int16>.GetValue COM call does,
// into the raw ABI word newFakeIReference writes into its out-param. A
// runtime (non-constant) uint16 conversion is required here — converting a
// negative constant straight to uint16 is a compile error.
func int16ABIWord(dbm int16) uintptr {
	return uintptr(uint16(dbm))
}

// newFakeIReference fakes a Windows.Foundation.IReference<Int16> using
// winrt-go's own exported foundation.IReferenceVtbl. raw is written
// verbatim into GetValue's out-param as the raw ABI word — mirroring
// referenceInt16Value's own contract that only the low 16 bits carry the
// signed dBm payload.
func newFakeIReference(raw uintptr) *foundation.IReference {
	vtbl := &foundation.IReferenceVtbl{}
	vtbl.Release = syscall.NewCallback(func(this unsafe.Pointer) uintptr { return 0 })
	vtbl.GetValue = syscall.NewCallback(func(this, out unsafe.Pointer) uintptr {
		*(*uintptr)(out) = raw
		return 0
	})
	return &foundation.IReference{
		IInspectable: ole.IInspectable{IUnknown: ole.IUnknown{RawVTable: (*interface{})(unsafe.Pointer(vtbl))}},
	}
}

// newFakeArgs fakes a BluetoothLEAdvertisementReceivedEventArgs whose
// QueryInterface answers interface 1's real GUID (winrt-go's exported
// advertisement.GUIDiBluetoothLEAdvertisementReceivedEventArgs, backing
// GetBluetoothAddress) with iface1, and interface 2's GUID (this package's
// own guidBluetoothLEAdvertisementReceivedEventArgs2, backing
// GetTransmitPowerLevelInDBm) with iface2 — either may be nil to simulate a
// runtime that doesn't expose that interface, answered with E_NOINTERFACE.
func newFakeArgs(iface1 *fakeInterface1, iface2 *iBluetoothLEAdvertisementReceivedEventArgs2) *advertisement.BluetoothLEAdvertisementReceivedEventArgs {
	iid1 := *ole.NewGUID(advertisement.GUIDiBluetoothLEAdvertisementReceivedEventArgs)
	iid2 := *ole.NewGUID(guidBluetoothLEAdvertisementReceivedEventArgs2)

	vtbl := &ole.IUnknownVtbl{}
	vtbl.QueryInterface = syscall.NewCallback(func(this, iid, ppv unsafe.Pointer) uintptr {
		requested := *(*ole.GUID)(iid)
		switch {
		case requested == iid1 && iface1 != nil:
			*(*unsafe.Pointer)(ppv) = unsafe.Pointer(iface1)
			return 0
		case requested == iid2 && iface2 != nil:
			*(*unsafe.Pointer)(ppv) = unsafe.Pointer(iface2)
			return 0
		default:
			*(*unsafe.Pointer)(ppv) = nil
			return uintptr(ole.E_NOINTERFACE)
		}
	})

	return &advertisement.BluetoothLEAdvertisementReceivedEventArgs{
		IUnknown: ole.IUnknown{RawVTable: (*interface{})(unsafe.Pointer(vtbl))},
	}
}

// TestTxPowerFromArgs_PopulatesFromRealInteropPath is AC #9's proof at the
// COM-interop level (as opposed to TestToAdvertisement_..., which proves
// the tracker->toAdvertisement wiring but injects straight into the
// tracker, bypassing this code entirely): a simulated WinRT event args
// object exposing interface 2 and a populated TransmitPowerLevelInDBm
// drives the real QueryInterface + vtable-slot-call + IReference-unboxing
// path in txPowerFromArgs end to end.
func TestTxPowerFromArgs_PopulatesFromRealInteropPath(t *testing.T) {
	ref := newFakeIReference(int16ABIWord(-59))
	iface2 := newFakeInterface2(func() (unsafe.Pointer, uintptr) {
		return unsafe.Pointer(ref), 0
	})
	args := newFakeArgs(nil, iface2)

	tx := txPowerFromArgs(args)
	if tx == nil || *tx != -59 {
		t.Fatalf("txPowerFromArgs() = %v, want -59", tx)
	}
}

// TestTxPowerFromArgs_QueryInterfaceUnsupported simulates a Windows build
// where interface 2 isn't exposed at all (QueryInterface fails for every
// advertisement) — txPowerFromArgs must treat that the same as "no TX
// power" rather than propagating the COM error.
func TestTxPowerFromArgs_QueryInterfaceUnsupported(t *testing.T) {
	args := newFakeArgs(nil, nil)

	if tx := txPowerFromArgs(args); tx != nil {
		t.Fatalf("txPowerFromArgs() = %v, want nil when interface 2 isn't exposed", *tx)
	}
}

// TestTxPowerFromArgs_ReferenceAbsent simulates the ordinary per-device
// case AC #7 is about: interface 2 is exposed and GetTransmitPowerLevelInDBm
// succeeds, but the IReference itself is nil because this specific
// advertisement doesn't carry a TX Power Level AD element.
func TestTxPowerFromArgs_ReferenceAbsent(t *testing.T) {
	iface2 := newFakeInterface2(func() (unsafe.Pointer, uintptr) {
		return nil, 0
	})
	args := newFakeArgs(nil, iface2)

	if tx := txPowerFromArgs(args); tx != nil {
		t.Fatalf("txPowerFromArgs() = %v, want nil when the device doesn't advertise TX power", *tx)
	}
}

// TestHandleReceived_PopulatesTrackerFromSimulatedEvent drives
// txPowerTracker.handleReceived — the factored-out Received callback body —
// with a simulated event args object exposing both interfaces, proving the
// real Address-extraction and TX-power-extraction paths both run and land
// in the tracker's map, the same as a live watcher's callback would.
func TestHandleReceived_PopulatesTrackerFromSimulatedEvent(t *testing.T) {
	const rawAddr = 0xAABBCCDDEEFF
	ref := newFakeIReference(int16ABIWord(-42))
	iface2 := newFakeInterface2(func() (unsafe.Pointer, uintptr) {
		return unsafe.Pointer(ref), 0
	})
	iface1 := newFakeInterface1(rawAddr)
	args := newFakeArgs(iface1, iface2)

	tracker := newTXPowerTracker()
	tracker.handleReceived(args)

	got := tracker.lookup(addressFromRaw(rawAddr))
	if got == nil || *got != -42 {
		t.Fatalf("tracker.lookup() = %v, want -42", got)
	}
}
