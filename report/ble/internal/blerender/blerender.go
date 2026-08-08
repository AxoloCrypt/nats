// Package blerender holds the field-rendering rules shared by the three
// human-readable BLE writers (report/ble/table, markdown, plain), so the
// placeholder and sanitization logic lives in exactly one place rather than
// being restated — and drifting — in each of them.
//
// report/ble/json deliberately does not use this package: the
// explicit-placeholder guarantee is satisfied there by the key always being
// present in the object (no omitempty), and an absent Name is serialized as
// "" rather than having a human-facing placeholder substituted into
// machine-readable output.
package blerender

import (
	"nats/core/ble"
	"nats/internal/strutil"
	"nats/report/internal/render"
)

// Placeholder is the explicit stand-in rendered for any field that is absent
// for a given device. It matches ble.DeviceTypeUnknown's value, and
// the literal "unknown" that core/ble.DeriveVendor already resolves to, so a
// missing field reads identically no matter which field it was.
const Placeholder = "unknown"

// Fields returns every column of a BLEDeviceProfile as a display-ready
// string: sanitized, and with an explicit placeholder substituted for any
// value that is absent or blank.
//
// Every field is sanitized, not just Name. Name is the only one an attacker
// broadcasts directly today — Vendor comes from the static companyIDVendors
// table and DistanceEstimate from FormatDistance — but that is a property of
// the current BLEScanner implementation, not a guarantee of the type. A
// second scanner (such as the planned Android bridge) could widen any of
// these sources without touching this package, so sanitizing is done
// uniformly at the writer boundary rather than selected per field based on
// today's provenance.
//
// Blank-but-non-empty values are treated as absent. A value of " " (or a
// name broadcast as "\n", which sanitizes to " ") would otherwise slip past
// an == "" check and render as an empty cell, making "no name broadcast"
// indistinguishable from "name is one space" — exactly the silently-blank
// column this package exists to prevent. This matches how core/ble itself
// already tests for absence (skipDiagnostic's strutil.IsBlank check).
func Fields(d ble.BLEDeviceProfile) (address, name, vendor, deviceType, distance string) {
	return field(d.Address),
		field(d.Name),
		field(d.Vendor),
		field(d.DeviceType),
		field(d.DistanceEstimate)
}

func field(s string) string {
	s = render.SanitizeLine(s)
	if strutil.IsBlank(s) {
		return Placeholder
	}
	return s
}
