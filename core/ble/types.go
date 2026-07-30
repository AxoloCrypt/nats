package ble

import "time"

// Options configures a single core/ble.Run call. Window is the only field
// this story needs to make Run compile — Story 4.5 owns the final CLI flag
// and default-window decision.
type Options struct {
	Window time.Duration
}

// DefaultOptions returns a placeholder default. Story 4.5 owns finalizing
// the real default/flag, mirroring how core/engine.DefaultOptions() started
// as a stub filled in by later stories.
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
// Structural Seed). DeviceType (Story 4.4) and DistanceEstimate (Story 4.3)
// are left as their Go zero value ("") until those stories populate them.
type BLEDeviceProfile struct {
	Address          string
	Name             string
	Vendor           string
	DeviceType       string
	DistanceEstimate string
}
