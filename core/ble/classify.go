package ble

import (
	"strconv"
	"strings"
)

// v1 taxonomy of BLE Device Types (PRD §3 Glossary, `[ASSUMPTION]`).
// DeviceTypeUnknown is what ClassifyDeviceType returns when none of the
// three signals resolve a more specific type — same naming convention as
// core/engine.DeviceTypeUnknown, and same "no accuracy bar for v1" stance
// (PRD FR-5: "incorrect guesses are acceptable").
const (
	DeviceTypeWearable      = "wearable"
	DeviceTypeAudioDevice   = "audio device"
	DeviceTypePhone         = "phone"
	DeviceTypeComputer      = "computer"
	DeviceTypeSensorTag     = "sensor/tag"
	DeviceTypeHIDPeripheral = "HID peripheral"
	DeviceTypeSmartHome     = "smart-home device"
	DeviceTypeUnknown       = "unknown"
)

// appearanceCategoryTypes maps the Bluetooth SIG Assigned Numbers "GAP
// Appearance" characteristic's category field (the high 10 bits of the
// 16-bit Appearance value; category = value >> 6) to this story's v1
// taxonomy. Deliberately not exhaustive — the source documents set no
// accuracy bar, so only categories with an unambiguous mapping to one of
// the eight v1 types are included; every other category falls through to
// the next signal (ServiceUUIDs, then Name keywords).
var appearanceCategoryTypes = map[int]string{
	0x01: DeviceTypePhone,         // Phone (0x0040-0x007F)
	0x02: DeviceTypeComputer,      // Computer (0x0080-0x00BF)
	0x03: DeviceTypeWearable,      // Watch (0x00C0-0x00FF)
	0x08: DeviceTypeSensorTag,     // Tag (0x0200-0x023F)
	0x0D: DeviceTypeSensorTag,     // Heart Rate Sensor (0x0340-0x037F)
	0x0E: DeviceTypeSensorTag,     // Blood Pressure (0x0380-0x03BF)
	0x0F: DeviceTypeHIDPeripheral, // Human Interface Device (0x03C0-0x03FF)
	0x16: DeviceTypeSensorTag,     // Generic Sensor (0x0580-0x05BF)
	0x17: DeviceTypeSmartHome,     // Light Fixtures (0x05C0-0x05FF)
	0x18: DeviceTypeSmartHome,     // Fan (0x0600-0x063F)
	0x19: DeviceTypeSmartHome,     // HVAC (0x0640-0x067F)
	0x1A: DeviceTypeSmartHome,     // Air Conditioning (0x0680-0x06BF)
	0x1B: DeviceTypeSmartHome,     // Humidifier (0x06C0-0x06FF)
	0x1C: DeviceTypeSmartHome,     // Heating (0x0700-0x073F)
	0x1D: DeviceTypeSmartHome,     // Access Control (0x0740-0x077F, smart locks)
	0x22: DeviceTypeAudioDevice,   // Audio Sink (0x0880-0x08BF, speakers)
	0x23: DeviceTypeAudioDevice,   // Audio Source (0x08C0-0x08FF)
	0x26: DeviceTypeAudioDevice,   // Wearable Audio Device (0x0980-0x09BF, earbuds/headsets)
}

// appearanceCategory decodes Advertisement.Appearance and extracts the GAP
// Appearance category field. The encoding is this story's one real design
// decision (Task 2): discovery/blescan.toAdvertisement, the only write
// site, formats a raw 16-bit Appearance value as a 4-digit lowercase hex
// string with no "0x" prefix (e.g. "03c0") — documented identically there.
// Anything that isn't exactly that shape (including the "" zero value a
// platform that doesn't expose Appearance leaves behind, or a malformed
// value from some future adapter) reports ok=false so the caller degrades
// to the next signal instead of panicking or guessing.
func appearanceCategory(encoded string) (category int, ok bool) {
	if len(encoded) != 4 {
		return 0, false
	}
	v, err := strconv.ParseUint(encoded, 16, 16)
	if err != nil {
		return 0, false
	}
	return int(v >> 6), true
}

type classifyRule struct {
	deviceType string
	keywords   []string
}

// serviceUUIDRules maps a DeviceType to substrings looked for
// (case-insensitively) across an Advertisement's ServiceUUIDs (tinygo's
// full 128-bit lowercase string form, e.g.
// "0000180d-0000-1000-8000-00805f9b34fb" for the 16-bit Heart Rate
// Service). Kept small and defensible per the story's own guidance, not
// exhaustive. Checked in this fixed order — first match wins — so priority
// doesn't depend on slice/map iteration order.
var serviceUUIDRules = []classifyRule{
	{DeviceTypeHIDPeripheral, []string{"00001812-"}},          // HID Service
	{DeviceTypeSensorTag, []string{"0000180d-", "00001809-"}}, // Heart Rate, Health Thermometer
}

// nameKeywordRules maps a DeviceType to substrings looked for
// (case-insensitively) in an Advertisement's broadcast Name — the
// last-resort signal (PRD FR-5's "fuzzy keyword matching" fallback).
// Checked in this fixed order for the same reason as serviceUUIDRules.
var nameKeywordRules = []classifyRule{
	{DeviceTypeWearable, []string{"watch", "band", "fitbit"}},
	{DeviceTypeAudioDevice, []string{"buds", "airpods", "headphone", "headset", "earbud", "speaker"}},
	{DeviceTypePhone, []string{"phone", "galaxy"}},
	{DeviceTypeComputer, []string{"macbook", "laptop", "imac"}},
	{DeviceTypeHIDPeripheral, []string{"keyboard", "mouse", "gamepad"}},
	{DeviceTypeSmartHome, []string{"cam", "bulb", "plug", "light"}},
	{DeviceTypeSensorTag, []string{"sensor", "tag", "beacon"}},
}

// ClassifyDeviceType assigns a best-effort DeviceType (FR-5) to a fully
// parsed/normalized Advertisement. It is a pure function with no side
// effects, called exactly once per device by core/ble.Run after
// Name/Vendor/DistanceEstimate are already set (AD-6) — never inside
// discovery/blescan, which must only normalize raw platform data into
// Advertisement, never interpret it into a DeviceType.
//
// Signals are checked from most to least specific, mirroring
// core/engine.Classify's style: Appearance decides on its own when it maps
// to a known category, then ServiceUUIDs, then fuzzy Name keyword
// matching. DeviceTypeUnknown is returned when nothing matches — there is
// no accuracy bar for v1, so an "unknown" or an outright wrong guess are
// both acceptable outcomes; a panic or crash on malformed input is not.
func ClassifyDeviceType(adv Advertisement) string {
	if category, ok := appearanceCategory(adv.Appearance); ok {
		if t, ok := appearanceCategoryTypes[category]; ok {
			return t
		}
	}
	if t := matchServiceUUIDs(adv.ServiceUUIDs, serviceUUIDRules); t != "" {
		return t
	}
	if t := matchName(adv.Name, nameKeywordRules); t != "" {
		return t
	}
	return DeviceTypeUnknown
}

func matchServiceUUIDs(uuids []string, rules []classifyRule) string {
	for _, rule := range rules {
		for _, u := range uuids {
			lower := strings.ToLower(u)
			for _, kw := range rule.keywords {
				if strings.Contains(lower, kw) {
					return rule.deviceType
				}
			}
		}
	}
	return ""
}

func matchName(name string, rules []classifyRule) string {
	if name == "" {
		return ""
	}
	lower := strings.ToLower(name)
	for _, rule := range rules {
		for _, kw := range rule.keywords {
			if strings.Contains(lower, kw) {
				return rule.deviceType
			}
		}
	}
	return ""
}
