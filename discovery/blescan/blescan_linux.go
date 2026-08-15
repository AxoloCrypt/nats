//go:build linux

package blescan

import (
	"context"
	"strings"
	"sync"

	"github.com/godbus/dbus/v5"
)

// bluezDevice1Interface and bluezTxPowerProperty name the BlueZ D-Bus
// interface/property this tracker watches. TxPower is documented as
// `int16 TxPower [readonly, optional]` on org.bluez.Device1 — populated
// only once a device's advertisement has actually included the GAP TX Power
// Level AD type (0x0A), so its absence from a device's properties is a
// normal, common case, not an error.
const (
	bluezDevice1Interface = "org.bluez.Device1"
	bluezTxPowerProperty  = "TxPower"
)

func init() {
	tracker := newTXPowerTracker()
	startTXPowerTracking = tracker.start
	txPowerFor = tracker.lookup
}

// txPowerTracker maintains an Address -> TxPower map by watching BlueZ
// org.bluez.Device1 objects directly over D-Bus via
// github.com/godbus/dbus/v5, independently of tinygo.org/x/bluetooth's own
// Adapter.Scan: tinygo's ScanResult mapping (gap_linux.go's makeScanResult)
// reads Address, AddressType, Name, RSSI, ManufacturerData, ServiceData, and
// UUIDs off the same underlying device properties map but never TxPower, so
// that property has to be sourced independently.
//
// TxPower can populate after a device's first advertisement callback — BlueZ
// doesn't guarantee it's present on the very first sighting — so this
// watches PropertiesChanged/InterfacesAdded rather than doing a one-shot
// query per callback, which would risk a permanent nil for a device seen
// only once before TxPower arrived.
type txPowerTracker struct {
	mu     sync.Mutex
	values map[string]int // BlueZ Address string ("AA:BB:CC:DD:EE:FF") -> TxPower (dBm)
}

func newTXPowerTracker() *txPowerTracker {
	return &txPowerTracker{values: make(map[string]int)}
}

// start launches the tracker's goroutine and returns immediately — it never
// blocks the caller, dials D-Bus, or returns an error: a caller here has
// already had Probe()/enableAdapter succeed, so this is best-effort
// enrichment layered on top of an already-working scan, not a precondition
// for one. Scan derives txCtx from its own lifecycle and calls this
// synchronously before starting the real scan (see blescan.go), so the
// dial itself must happen inside run's goroutine, not here — a synchronous
// dbus.SystemBus() call here would block Scan's own start-up path (and,
// per Dev Notes, D-Bus/BlueZ activation can hang rather than fail fast) on
// a best-effort enrichment that's explicitly allowed to fail silently.
func (t *txPowerTracker) start(ctx context.Context) {
	go t.run(ctx)
}

func (t *txPowerTracker) lookup(address string) *int {
	t.mu.Lock()
	defer t.mu.Unlock()
	v, ok := t.values[address]
	if !ok {
		return nil
	}
	return &v
}

// run dials the system bus, seeds the map from BlueZ's already-known
// devices, then watches for updates until ctx is cancelled. The dial
// happens here rather than in start so start can return immediately — see
// start's doc comment.
func (t *txPowerTracker) run(ctx context.Context) {
	bus, err := dbus.SystemBus()
	if err != nil {
		return
	}

	signal := make(chan *dbus.Signal, 16)
	bus.Signal(signal)
	defer bus.RemoveSignal(signal)

	// WithMatchSender("org.bluez") is a well-known-name match: the bus
	// daemon resolves it to org.bluez's current unique connection name and
	// keeps the match current across a NameOwnerChanged, so this restricts
	// delivery to signals actually sent by BlueZ rather than any local
	// process that can reach the system bus and claims to speak for it.
	propsChanged := []dbus.MatchOption{
		dbus.WithMatchSender("org.bluez"),
		dbus.WithMatchInterface("org.freedesktop.DBus.Properties"),
		dbus.WithMatchMember("PropertiesChanged"),
		// arg0 of PropertiesChanged is the interface whose properties
		// changed — filtering on it server-side avoids waking this tracker
		// for every unrelated PropertiesChanged on the bus (NetworkManager,
		// UPower, logind, ...).
		dbus.WithMatchArg(0, bluezDevice1Interface),
	}
	if err := bus.AddMatchSignal(propsChanged...); err != nil {
		return
	}
	defer bus.RemoveMatchSignal(propsChanged...)

	interfacesAdded := []dbus.MatchOption{
		dbus.WithMatchSender("org.bluez"),
		dbus.WithMatchInterface("org.freedesktop.DBus.ObjectManager"),
		dbus.WithMatchMember("InterfacesAdded"),
	}
	if err := bus.AddMatchSignal(interfacesAdded...); err != nil {
		return
	}
	defer bus.RemoveMatchSignal(interfacesAdded...)

	// InterfacesRemoved: without this, a device BlueZ has forgotten (e.g.
	// evicted from its cache) would keep serving its last-known TxPower
	// from t.values forever, since nothing else ever clears an entry.
	interfacesRemoved := []dbus.MatchOption{
		dbus.WithMatchSender("org.bluez"),
		dbus.WithMatchInterface("org.freedesktop.DBus.ObjectManager"),
		dbus.WithMatchMember("InterfacesRemoved"),
	}
	if err := bus.AddMatchSignal(interfacesRemoved...); err != nil {
		return
	}
	defer bus.RemoveMatchSignal(interfacesRemoved...)

	// seed narrows, but does not fully close, the window where a device
	// already known to BlueZ shows TXPower as nil on its first advertisement
	// in this scan: seed runs after start has already returned, racing
	// against scanAdapter's own callback goroutine. Accepted per SPEC.md's
	// Open Question — a best-effort snapshot that may populate TXPower a
	// cycle late, rather than blocking Scan's start-up path to close the
	// race (see start's doc comment for why that trade was rejected).
	t.seed(bus)

	for {
		select {
		case <-ctx.Done():
			return
		case sig, ok := <-signal:
			if !ok {
				return
			}
			t.handleSignal(sig)
		}
	}
}

// seed populates the map from devices BlueZ already knows about before this
// tracker started watching. Without it, a device whose TxPower was already
// known to BlueZ before this Scan call began would show as nil until its
// next PropertiesChanged/InterfacesAdded signal, which may never come again
// for a device that doesn't repeat TxPower on every advertisement. See
// run's doc comment for why this narrows, but doesn't fully close, that gap.
func (t *txPowerTracker) seed(bus *dbus.Conn) {
	var managed map[dbus.ObjectPath]map[string]map[string]dbus.Variant
	err := bus.Object("org.bluez", dbus.ObjectPath("/")).
		Call("org.freedesktop.DBus.ObjectManager.GetManagedObjects", 0).
		Store(&managed)
	if err != nil {
		return
	}
	for path, ifaces := range managed {
		if device, ok := ifaces[bluezDevice1Interface]; ok {
			t.record(path, device)
		}
	}
}

func (t *txPowerTracker) handleSignal(sig *dbus.Signal) {
	switch sig.Name {
	case "org.freedesktop.DBus.ObjectManager.InterfacesAdded":
		if len(sig.Body) < 2 {
			return
		}
		path, ok := sig.Body[0].(dbus.ObjectPath)
		if !ok {
			return
		}
		ifaces, ok := sig.Body[1].(map[string]map[string]dbus.Variant)
		if !ok {
			return
		}
		if device, ok := ifaces[bluezDevice1Interface]; ok {
			t.record(path, device)
		}
	case "org.freedesktop.DBus.ObjectManager.InterfacesRemoved":
		if len(sig.Body) < 2 {
			return
		}
		path, ok := sig.Body[0].(dbus.ObjectPath)
		if !ok {
			return
		}
		ifaces, ok := sig.Body[1].([]string)
		if !ok {
			return
		}
		for _, iface := range ifaces {
			if iface == bluezDevice1Interface {
				t.forget(path)
				return
			}
		}
	case "org.freedesktop.DBus.Properties.PropertiesChanged":
		// Signature is (s interface, a{sv} changed_properties,
		// as invalidated_properties) — always three body members.
		if len(sig.Body) < 3 {
			return
		}
		iface, ok := sig.Body[0].(string)
		if !ok || iface != bluezDevice1Interface {
			return
		}
		// PropertiesChanged only carries the properties that actually
		// changed. Since Address never changes after a device is first
		// seen, a TxPower update arriving later typically won't repeat
		// Address in this map — sig.Path (the signal's own device object
		// path), not this changes map, is the reliable way to know which
		// device it belongs to.
		if changes, ok := sig.Body[1].(map[string]dbus.Variant); ok {
			t.record(sig.Path, changes)
		}
		// A property BlueZ explicitly invalidates (rather than omits) must
		// stop being served — otherwise a stale TxPower value lingers in
		// t.values after BlueZ itself no longer vouches for it.
		if invalidated, ok := sig.Body[2].([]string); ok {
			for _, prop := range invalidated {
				if prop == bluezTxPowerProperty {
					t.forget(sig.Path)
					break
				}
			}
		}
	}
}

// record extracts TxPower from a BlueZ device properties map, if present,
// keyed by the Address derived from the device's own D-Bus object path
// rather than any "Address" entry in props — see handleSignal for why props
// can't always be trusted to carry Address itself.
func (t *txPowerTracker) record(path dbus.ObjectPath, props map[string]dbus.Variant) {
	variant, ok := props[bluezTxPowerProperty]
	if !ok {
		return
	}
	tx, ok := variant.Value().(int16)
	if !ok {
		return
	}
	address, ok := addressFromDevicePath(path)
	if !ok {
		return
	}

	t.mu.Lock()
	t.values[address] = int(tx)
	t.mu.Unlock()
}

// forget removes a device's tracked TxPower, keyed by the same object-path
// convention record uses. Called when BlueZ reports the device gone
// (InterfacesRemoved) or its TxPower specifically invalidated
// (PropertiesChanged's invalidated_properties).
func (t *txPowerTracker) forget(path dbus.ObjectPath) {
	address, ok := addressFromDevicePath(path)
	if !ok {
		return
	}

	t.mu.Lock()
	delete(t.values, address)
	t.mu.Unlock()
}

// addressFromDevicePath extracts a device's Address (e.g.
// "AA:BB:CC:DD:EE:FF") from its BlueZ D-Bus object path (e.g.
// "/org/bluez/hci0/dev_AA_BB_CC_DD_EE_FF") — the reverse of the convention
// tinygo.org/x/bluetooth's own Adapter.Connect uses to build a device path
// from an Address (address.MAC.String() with ":" replaced by "_").
func addressFromDevicePath(path dbus.ObjectPath) (string, bool) {
	const prefix = "/dev_"
	s := string(path)
	idx := strings.LastIndex(s, prefix)
	if idx < 0 {
		return "", false
	}
	hex := s[idx+len(prefix):]
	address := strings.ToUpper(strings.ReplaceAll(hex, "_", ":"))
	if len(address) != len("AA:BB:CC:DD:EE:FF") {
		return "", false
	}
	return address, true
}
