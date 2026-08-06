package ble

import "time"

// Options configures a single core/ble.Run call.
type Options struct {
	Window time.Duration
}

// DefaultOptions is the single source of truth for the default listening
// window (5s, NL-FR-7) — mirrors engine.DefaultOptions()'s role for "nats
// scan". cmd/cli/ble.go's --window flag defaults to this value and only
// overrides it when the user explicitly passes --window.
func DefaultOptions() Options {
	return Options{Window: 5 * time.Second}
}

type EventKind string

const (
	// EventKindDeviceFound fires once per compiled BLEDeviceProfile, as soon
	// as its Advertisement is drained — mirrors engine.EventKindDeviceFound
	// so a future driving adapter can stream live per-device discovery the
	// same way the LAN vertical does.
	EventKindDeviceFound EventKind = "DeviceFound"
	EventKindDone        EventKind = "Done"
)

// Event mirrors the shape of the base spine's engine.Event (Done always
// last, channel closes immediately after, no event follows Done) but is an
// independently-defined type — core/ble never imports core/engine.
type Event struct {
	Kind        EventKind
	Device      BLEDeviceProfile
	Diagnostics []Diagnostic
	Report      Report
}

// Diagnostic reuses the Severity/Message/Reason shape from base AD-8
// verbatim, but as its own independent struct — core/ble must never import
// engine.Diagnostic (NL-AD-1's import-boundary rule).
type Diagnostic struct {
	Severity string
	Message  string
	Reason   string
}

// Advertisement is the raw shape a BLEScanner emits per spine AD-5 — every
// field beyond Address/RSSI is optional and left zero-valued when the
// scanning platform doesn't expose it.
type Advertisement struct {
	Address          string
	Name             string
	RSSI             int
	TXPower          *int
	Appearance       string
	ServiceUUIDs     []string
	ManufacturerData []byte
	CompanyID        *uint16
}

// Report carries every BLEDeviceProfile compiled during a Run call.
type Report struct {
	Devices []BLEDeviceProfile
}

// BLEDeviceProfile is the compiled-from-Advertisement device row (spine
// Structural Seed). DeviceType's possible values are defined in classify.go.
type BLEDeviceProfile struct {
	Address          string
	Name             string
	Vendor           string
	DeviceType       string
	DistanceEstimate string
}
