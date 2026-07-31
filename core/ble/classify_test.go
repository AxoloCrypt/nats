package ble

import "testing"

func TestClassifyDeviceType_AppearanceWinsRegardlessOfName(t *testing.T) {
	// "00c0" -> category 0x03 (Watch, 0x00C0-0x00FF) -> wearable. Name
	// would otherwise resolve to audio device via keyword matching, but
	// Appearance is checked first and must win outright.
	adv := Advertisement{Appearance: "00c0", Name: "Bob's Headphones"}

	got := ClassifyDeviceType(adv)
	if got != DeviceTypeWearable {
		t.Fatalf("expected %q (Appearance signal), got %q", DeviceTypeWearable, got)
	}
}

func TestClassifyDeviceType_ServiceUUIDFallback(t *testing.T) {
	// No Appearance, but a Heart Rate Service (0x180D) UUID is present.
	adv := Advertisement{ServiceUUIDs: []string{"0000180d-0000-1000-8000-00805f9b34fb"}, Name: "unrelated"}

	got := ClassifyDeviceType(adv)
	if got != DeviceTypeSensorTag {
		t.Fatalf("expected %q (ServiceUUIDs signal), got %q", DeviceTypeSensorTag, got)
	}
}

func TestClassifyDeviceType_ServiceUUIDWinsOverConflictingName(t *testing.T) {
	// No Appearance, but a Heart Rate Service (0x180D) UUID is present.
	// Name would otherwise resolve to audio device via keyword matching,
	// but ServiceUUIDs is checked first and must win outright.
	adv := Advertisement{ServiceUUIDs: []string{"0000180d-0000-1000-8000-00805f9b34fb"}, Name: "Bob's AirPods"}

	got := ClassifyDeviceType(adv)
	if got != DeviceTypeSensorTag {
		t.Fatalf("expected %q (ServiceUUIDs signal), got %q", DeviceTypeSensorTag, got)
	}
}

func TestClassifyDeviceType_NameKeywordFallback(t *testing.T) {
	// No Appearance, no ServiceUUIDs — falls all the way back to fuzzy
	// keyword matching against Name.
	adv := Advertisement{Name: "Bob's AirPods"}

	got := ClassifyDeviceType(adv)
	if got != DeviceTypeAudioDevice {
		t.Fatalf("expected %q (Name fallback), got %q", DeviceTypeAudioDevice, got)
	}
}

func TestClassifyDeviceType_UnknownWhenNoSignalResolves(t *testing.T) {
	adv := Advertisement{Address: "aa:bb:cc:dd:ee:ff", RSSI: -60}

	got := ClassifyDeviceType(adv)
	if got != DeviceTypeUnknown {
		t.Fatalf("expected %q, got %q", DeviceTypeUnknown, got)
	}
}

func TestClassifyDeviceType_MalformedAppearanceNeverPanicsAndDegrades(t *testing.T) {
	cases := []struct {
		name       string
		appearance string
	}{
		{"empty (platform doesn't expose it)", ""},
		{"too short", "1"},
		{"too long", "0012345"},
		{"non-hex garbage", "zzzz"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("ClassifyDeviceType panicked on Appearance %q: %v", tc.appearance, r)
				}
			}()

			adv := Advertisement{Appearance: tc.appearance, Name: "Bob's AirPods"}
			got := ClassifyDeviceType(adv)
			if got != DeviceTypeAudioDevice {
				t.Fatalf("expected malformed Appearance %q to degrade to the Name signal (%q), got %q", tc.appearance, DeviceTypeAudioDevice, got)
			}
		})
	}
}

func TestClassifyDeviceType_UnknownAppearanceCategoryDegrades(t *testing.T) {
	// "0000" decodes fine (category 0) but 0 isn't a mapped category, so
	// this must fall through to the next signal rather than resolving to
	// Unknown outright when a later signal could still match.
	adv := Advertisement{Appearance: "0000", Name: "Bob's AirPods"}

	got := ClassifyDeviceType(adv)
	if got != DeviceTypeAudioDevice {
		t.Fatalf("expected unmapped Appearance category to degrade to the Name signal (%q), got %q", DeviceTypeAudioDevice, got)
	}
}
