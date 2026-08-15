//go:build windows

package blescan

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"github.com/go-ole/go-ole"
	winrt "github.com/saltosystems/winrt-go"
	"github.com/saltosystems/winrt-go/windows/devices/bluetooth/advertisement"
	"github.com/saltosystems/winrt-go/windows/foundation"
)

// guidBluetoothLEAdvertisementReceivedEventArgs2 is WinRT's
// IBluetoothLEAdvertisementReceivedEventArgs2 — the second COM interface on
// a received-advertisement event args object, the one that carries
// TransmitPowerLevelInDBm. winrt-go's generated
// bluetoothleadvertisementreceivedeventargs.go defines this interface's
// vtable layout (as an unexported type, so it can't be referenced directly
// from this package) but never generated a caller-side wrapper for this
// particular slot — every other method on interface 1
// (GetRawSignalStrengthInDBm, GetBluetoothAddress, GetAdvertisement) has
// one. iBluetoothLEAdvertisementReceivedEventArgs2 below reproduces that
// same generated shape locally so the missing slot can be called the same
// way winrt-go calls every other one: QueryInterface for this GUID, cast
// the resulting pointer to the interface's vtable-shaped struct, then
// syscall.SyscallN the slot directly.
const guidBluetoothLEAdvertisementReceivedEventArgs2 = "12d9c87b-0399-5f0e-a348-53b02b6b162e"

type iBluetoothLEAdvertisementReceivedEventArgs2 struct {
	ole.IInspectable
}

type iBluetoothLEAdvertisementReceivedEventArgs2Vtbl struct {
	ole.IInspectableVtbl

	GetBluetoothAddressType    uintptr
	GetTransmitPowerLevelInDBm uintptr
	GetIsAnonymous             uintptr
	GetIsConnectable           uintptr
	GetIsScannable             uintptr
	GetIsDirected              uintptr
	GetIsScanResponse          uintptr
}

func (v *iBluetoothLEAdvertisementReceivedEventArgs2) VTable() *iBluetoothLEAdvertisementReceivedEventArgs2Vtbl {
	return (*iBluetoothLEAdvertisementReceivedEventArgs2Vtbl)(unsafe.Pointer(v.RawVTable))
}

// GetTransmitPowerLevelInDBm calls the un-wrapped vtable slot directly,
// mirroring winrt-go's own generated pattern (e.g.
// iBluetoothLEAdvertisementReceivedEventArgs.GetRawSignalStrengthInDBm).
// TransmitPowerLevelInDBm is WinRT-typed as Windows.Foundation.IReference<
// Int16> (a boxed nullable Int16) rather than a plain Int16, because not
// every advertisement carries a TX Power Level AD element — the out
// parameter is therefore an interface pointer that itself can be nil,
// which is exactly how a missing TX power surfaces here, one layer before
// any unboxing happens.
func (v *iBluetoothLEAdvertisementReceivedEventArgs2) GetTransmitPowerLevelInDBm() (*foundation.IReference, error) {
	var out *foundation.IReference
	hr, _, _ := syscall.SyscallN(
		v.VTable().GetTransmitPowerLevelInDBm,
		uintptr(unsafe.Pointer(v)),    // this
		uintptr(unsafe.Pointer(&out)), // out foundation.IReference (nil when absent)
	)

	if hr != 0 {
		return nil, ole.NewError(hr)
	}

	return out, nil
}

// referenceInt16Value unboxes the raw ABI word an IReference<Int16>.GetValue
// call writes its Int16 payload into. The COM callee only ever writes the
// low 2 bytes of that pointer-sized out-parameter (the property's actual
// wire type), leaving the rest of the word exactly as GetValue's own
// zero-initialized local left it, so truncating straight to int16 recovers
// the signed dBm value regardless of what (if anything) occupies the upper
// bits — kept as a pure function, taking the raw word rather than an
// unsafe.Pointer, so it can be exercised by a test without constructing a
// real (or fake) unsafe pointer.
func referenceInt16Value(raw uintptr) int {
	return int(int16(raw))
}

// txPowerFromArgs sources TX power for a single received-advertisement
// event: QueryInterface for interface 2 (nil/error if this runtime doesn't
// expose it — treated as "no TX power" rather than a fatal condition, since
// this is best-effort enrichment layered on top of an already-working
// scan), then GetTransmitPowerLevelInDBm, then unbox the IReference if one
// came back. A nil *int at any step (including hr indicating the interface
// or the reference itself is absent) means exactly what it means throughout
// this package: this advertisement didn't carry TX power.
func txPowerFromArgs(args *advertisement.BluetoothLEAdvertisementReceivedEventArgs) *int {
	itf, err := args.QueryInterface(ole.NewGUID(guidBluetoothLEAdvertisementReceivedEventArgs2))
	if err != nil || itf == nil {
		return nil
	}
	defer itf.Release()

	v2 := (*iBluetoothLEAdvertisementReceivedEventArgs2)(unsafe.Pointer(itf))
	ref, err := v2.GetTransmitPowerLevelInDBm()
	if err != nil || ref == nil {
		return nil
	}
	defer ref.Release()

	val, err := ref.GetValue()
	if err != nil {
		return nil
	}

	tx := referenceInt16Value(uintptr(val))
	return &tx
}

// addressFromRaw formats a raw 48-bit Bluetooth address the same way
// tinygo.org/x/bluetooth's own gap_windows.go builds one (byte i of the
// little-endian uint64 becomes MAC[i]) and MAC.String() renders it
// (MAC[5]:MAC[4]:...:MAC[0], uppercase hex) — so a device's address as
// tracked here is always the identical string toAdvertisement's
// result.Address.String() produces for the same device, which is what
// makes txPowerFor's Address-keyed lookup actually correlate the two
// independent watchers' events.
func addressFromRaw(addr uint64) string {
	var b [6]byte
	for i := range b {
		b[i] = byte(addr)
		addr >>= 8
	}
	return fmt.Sprintf("%02X:%02X:%02X:%02X:%02X:%02X", b[5], b[4], b[3], b[2], b[1], b[0])
}

// txPowerTracker maintains an Address -> TXPower map populated by a second,
// independent BluetoothLEAdvertisementWatcher run directly against
// winrt-go, in parallel to tinygo.org/x/bluetooth's own Adapter.Scan (see
// blescan.go's startTXPowerTracking/txPowerFor doc comment) — the Windows
// counterpart to blescan_linux.go's txPowerTracker, same map+mutex shape,
// different source event.
type txPowerTracker struct {
	mu     sync.Mutex
	values map[string]int
}

func newTXPowerTracker() *txPowerTracker {
	return &txPowerTracker{values: make(map[string]int)}
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

func (t *txPowerTracker) record(address string, tx int) {
	t.mu.Lock()
	t.values[address] = tx
	t.mu.Unlock()
}

// start launches the tracker's watcher goroutine and returns immediately,
// mirroring blescan_linux.go's txPowerTracker.start: the dial/setup happens
// inside run so a caller (blescan.go's Scan) is never blocked on this
// best-effort enrichment.
func (t *txPowerTracker) start(ctx context.Context) {
	go t.run(ctx)
}

// stopWaitTimeout bounds how long run() waits for the Stopped event after
// calling watcher.Stop(), on both the success and error paths — Stop()
// failing synchronously doesn't mean the watcher never started stopping, so
// waiting (bounded) rather than returning immediately avoids releasing the
// watcher/handlers out from under a callback that could still be in
// flight. The bound exists only so a Stopped event that's genuinely never
// delivered can't block run() (and its deferred releases) forever.
const stopWaitTimeout = 5 * time.Second

// handleReceived is the Received callback's body, factored out of run() so
// it can be driven directly by a test double in blescan_windows_test.go
// without standing up a real WinRT watcher: it extracts Address and TX
// power from a single received-advertisement event and records them into
// the tracker, mirroring how a real device's advertisement flows through.
func (t *txPowerTracker) handleReceived(args *advertisement.BluetoothLEAdvertisementReceivedEventArgs) {
	rawAddr, err := args.GetBluetoothAddress()
	if err != nil {
		return
	}

	tx := txPowerFromArgs(args)
	if tx == nil {
		return
	}

	t.record(addressFromRaw(rawAddr), *tx)
}

// run stands up the second BluetoothLEAdvertisementWatcher and feeds every
// received event's Address+TXPower into the tracker's map until ctx is
// cancelled, following the exact same watcher setup
// tinygo.org/x/bluetooth's own gap_windows.go Adapter.Scan uses (see that
// file's Scan method) — NewBluetoothLEAdvertisementWatcher,
// SetScanningMode(Active) so scan responses are included, a
// TypedEventHandler<Watcher, ReceivedEventArgs> computed via
// winrt.ParameterizedInstanceGUID exactly as Scan computes its own, then
// Start. It also mirrors Scan's Stopped handshake: Windows transitions the
// watcher through an intermediate Stopping state, so this waits (bounded by
// stopWaitTimeout) for the Stopped event after calling Stop before
// releasing the watcher/handlers — releasing earlier could free a COM
// object out from under a callback still in flight. Unlike Scan's callback,
// this watcher's callback extracts only Address and TX power rather than a
// full ScanResult, and nothing here ever calls Connect or any GATT method.
func (t *txPowerTracker) run(ctx context.Context) {
	// Every OS thread that makes COM calls must itself join the process's
	// COM apartment first — tinygo's own Adapter.Enable() (adapter_windows.go)
	// does this once via ole.RoInitialize(1) on whatever thread happens to
	// call Enable(). run() has no such guarantee on its own: it's a freshly
	// spawned goroutine that later blocks on <-ctx.Done() and <-stopped,
	// either of which the Go scheduler may resume on a different OS thread
	// than the one that started it. LockOSThread pins this goroutine to one
	// OS thread for its entire COM-touching lifetime, and RoInitialize(1)
	// joins that thread to the same multithreaded apartment Enable() already
	// joined its own thread to. Without both, a COM call below can fail with
	// CO_E_NOTINITIALIZED — every call site here already treats any error as
	// "stop trying", so that failure would otherwise degrade silently into
	// "TX power never populated" with nothing surfaced anywhere.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if err := ole.RoInitialize(1); err != nil {
		return
	}

	watcher, err := advertisement.NewBluetoothLEAdvertisementWatcher()
	if err != nil {
		return
	}
	defer func() {
		_ = watcher.Release()
	}()

	if err := watcher.SetScanningMode(advertisement.BluetoothLEScanningModeActive); err != nil {
		return
	}

	receivedGUID := winrt.ParameterizedInstanceGUID(
		foundation.GUIDTypedEventHandler,
		advertisement.SignatureBluetoothLEAdvertisementWatcher,
		advertisement.SignatureBluetoothLEAdvertisementReceivedEventArgs,
	)
	receivedHandler := foundation.NewTypedEventHandler(ole.NewGUID(receivedGUID), func(_ *foundation.TypedEventHandler, _, arg unsafe.Pointer) {
		t.handleReceived((*advertisement.BluetoothLEAdvertisementReceivedEventArgs)(arg))
	})
	defer receivedHandler.Release()

	receivedToken, err := watcher.AddReceived(receivedHandler)
	if err != nil {
		return
	}
	defer watcher.RemoveReceived(receivedToken)

	stopped := make(chan struct{})
	var stopOnce sync.Once
	stoppedGUID := winrt.ParameterizedInstanceGUID(
		foundation.GUIDTypedEventHandler,
		advertisement.SignatureBluetoothLEAdvertisementWatcher,
		advertisement.SignatureBluetoothLEAdvertisementWatcherStoppedEventArgs,
	)
	stoppedHandler := foundation.NewTypedEventHandler(ole.NewGUID(stoppedGUID), func(_ *foundation.TypedEventHandler, _, _ unsafe.Pointer) {
		stopOnce.Do(func() { close(stopped) })
	})
	defer stoppedHandler.Release()

	stoppedToken, err := watcher.AddStopped(stoppedHandler)
	if err != nil {
		return
	}
	defer watcher.RemoveStopped(stoppedToken)

	if err := watcher.Start(); err != nil {
		return
	}

	<-ctx.Done()
	_ = watcher.Stop()
	select {
	case <-stopped:
	case <-time.After(stopWaitTimeout):
	}
}

func init() {
	tracker := newTXPowerTracker()
	startTXPowerTracking = tracker.start
	txPowerFor = tracker.lookup
}
