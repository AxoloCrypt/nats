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
//
// The JSON tags exist because Report carries Diagnostics into the
// machine-readable Writer; they follow the same no-omitempty rule as every
// other tag in this package.
type Diagnostic struct {
	Severity string `json:"severity"`
	Message  string `json:"message"`
	Reason   string `json:"reason"`
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

// Report carries every BLEDeviceProfile compiled during a Run call, plus
// every Diagnostic the run produced.
//
// Diagnostics is carried on the Report — not only on the Done Event — so a
// machine-readable Writer can surface them too (spine AD-11): without it,
// "nats ble --format json" emits an identical empty device list for an empty
// room and for a scan that was skipped outright (permission gap, no adapter),
// leaving a scripting consumer unable to tell a successful nothing-nearby
// result from a degraded one. Note the divergence from engine.Report, whose
// equivalent field is tagged omitempty: AD-11's stronger never-dropped-key
// guarantee for this vertical applies here as it does to BLEDeviceProfile
// below, so the key is always present (report/ble/json normalizes nil to []).
type Report struct {
	Devices     []BLEDeviceProfile `json:"devices"`
	Diagnostics []Diagnostic       `json:"diagnostics"`
}

// BLEDeviceProfile is the compiled-from-Advertisement device row (spine
// Structural Seed). DeviceType's possible values are defined in classify.go.
//
// Deliberately no "omitempty" on any tag here (spine AD-11): the base
// spine's engine.Device tags do use omitempty, but AD-11 sets a stronger
// guarantee for this vertical — every field renders as an explicit
// placeholder rather than a dropped key, including in JSON. An omitempty'd
// key vanishing from the object entirely would be exactly the "silently
// dropped column" AD-11 forbids (report/ble/json's tests lock this in).
type BLEDeviceProfile struct {
	Address          string `json:"address"`
	Name             string `json:"name"`
	Vendor           string `json:"vendor"`
	DeviceType       string `json:"deviceType"`
	DistanceEstimate string `json:"distanceEstimate"`
}
