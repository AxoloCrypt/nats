package json

import (
	"encoding/json"
	"strings"
	"testing"

	"nats/core/ble"
)

func TestName_ReturnsJSON(t *testing.T) {
	if got := (Writer{}).Name(); got != "json" {
		t.Fatalf("expected Name() to return \"json\", got %q", got)
	}
}

func TestSelfRegistersAsJSON(t *testing.T) {
	w, ok := ble.GetWriter("json")
	if !ok {
		t.Fatal("expected \"json\" writer to be registered via init()")
	}
	if w.Name() != "json" {
		t.Fatalf("expected registered writer's Name() to be \"json\", got %q", w.Name())
	}
}

func TestWrite_EmptyReportProducesValidJSON(t *testing.T) {
	out, err := (Writer{}).Write(ble.Report{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var decoded ble.Report
	if err := json.Unmarshal(out, &decoded); err != nil {
		t.Fatalf("expected valid JSON, got error %v decoding: %s", err, out)
	}
}

// TestWrite_ZeroDevicesRendersEmptyArrayNotNull covers the rule that zero
// devices must still render valid, well-formed output (here, a valid empty
// JSON array) — a nil Devices slice must not surface to a scripting consumer
// as JSON "null".
func TestWrite_ZeroDevicesRendersEmptyArrayNotNull(t *testing.T) {
	out, err := (Writer{}).Write(ble.Report{Devices: nil})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(string(out), `"devices": []`) {
		t.Fatalf("expected \"devices\": [] for zero devices, got: %s", out)
	}
}

func TestWrite_FullyPopulatedDeviceRoundTrips(t *testing.T) {
	report := ble.Report{
		Devices: []ble.BLEDeviceProfile{
			{
				Address:          "aa:bb:cc:dd:ee:ff",
				Name:             "My Watch",
				Vendor:           "Acme Corp",
				DeviceType:       "wearable",
				DistanceEstimate: "near",
			},
		},
	}

	out, err := (Writer{}).Write(report)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	s := string(out)
	for _, want := range []string{"aa:bb:cc:dd:ee:ff", "My Watch", "Acme Corp", "wearable", "near"} {
		if !strings.Contains(s, want) {
			t.Fatalf("expected output to contain %q, got: %s", want, s)
		}
	}

	var decoded ble.Report
	if err := json.Unmarshal(out, &decoded); err != nil {
		t.Fatalf("expected valid JSON, got error %v decoding: %s", err, out)
	}
	if len(decoded.Devices) != 1 || decoded.Devices[0].Address != "aa:bb:cc:dd:ee:ff" {
		t.Fatalf("expected the decoded device to round-trip, got: %+v", decoded.Devices)
	}
}

// TestWrite_EveryKeyAlwaysPresentEvenWhenEmpty is the regression test for
// the deliberate "no omitempty" decision: an empty Name must still
// serialize as a present key ("name":"") rather than being dropped from the
// object entirely.
func TestWrite_EveryKeyAlwaysPresentEvenWhenEmpty(t *testing.T) {
	report := ble.Report{
		Devices: []ble.BLEDeviceProfile{
			{Address: "aa:bb:cc:dd:ee:ff", Vendor: "unknown"},
		},
	}

	out, err := (Writer{}).Write(report)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var decoded []map[string]any
	rawDevices, err := extractDevicesArray(out)
	if err != nil {
		t.Fatalf("failed to extract devices array: %v", err)
	}
	if err := json.Unmarshal(rawDevices, &decoded); err != nil {
		t.Fatalf("failed to decode devices as raw maps: %v", err)
	}
	if len(decoded) != 1 {
		t.Fatalf("expected 1 device, got %d", len(decoded))
	}
	for _, key := range []string{"address", "name", "vendor", "deviceType", "distanceEstimate"} {
		if _, present := decoded[0][key]; !present {
			t.Fatalf("expected key %q to be present even when empty, got: %v", key, decoded[0])
		}
	}
	if decoded[0]["name"] != "" {
		t.Fatalf("expected empty name to serialize as \"\", got: %v", decoded[0]["name"])
	}
}

// extractDevicesArray pulls the raw "devices" field out of the top-level
// Report object, so the present-key assertion above only asks the question
// through Go's own json.Unmarshal into an interface{} map — the same
// mechanism a scripting consumer's JSON parser would use.
func extractDevicesArray(out []byte) (json.RawMessage, error) {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(out, &top); err != nil {
		return nil, err
	}
	return top["devices"], nil
}

// TestWrite_DiagnosticsKeyAlwaysPresent extends the no-omitempty guarantee to
// Report.Diagnostics. The field exists so a skipped scan is distinguishable
// from an empty room in machine-readable output; a dropped or null key would
// put a scripting consumer right back where it started.
func TestWrite_DiagnosticsKeyAlwaysPresent(t *testing.T) {
	t.Run("absent diagnostics serialize as an empty array", func(t *testing.T) {
		out, err := (Writer{}).Write(ble.Report{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		var decoded map[string]any
		if err := json.Unmarshal(out, &decoded); err != nil {
			t.Fatalf("expected valid JSON, got %q: %v", out, err)
		}
		raw, present := decoded["diagnostics"]
		if !present {
			t.Fatalf("expected the \"diagnostics\" key to be present even when empty, got: %q", out)
		}
		arr, ok := raw.([]any)
		if !ok {
			t.Fatalf("expected \"diagnostics\" to be an array, not null, got: %q", out)
		}
		if len(arr) != 0 {
			t.Fatalf("expected an empty diagnostics array, got %d entries", len(arr))
		}
	})

	t.Run("a skipped scan is distinguishable from an empty room", func(t *testing.T) {
		skipped, err := (Writer{}).Write(ble.Report{
			Diagnostics: []ble.Diagnostic{{
				Severity: "warning",
				Message:  "BLE scan skipped",
				Reason:   "bluetooth permission not granted",
			}},
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		emptyRoom, err := (Writer{}).Write(ble.Report{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if string(skipped) == string(emptyRoom) {
			t.Fatalf("a skipped scan must not serialize identically to an empty room, both were: %q", skipped)
		}

		var decoded struct {
			Diagnostics []map[string]any `json:"diagnostics"`
		}
		if err := json.Unmarshal(skipped, &decoded); err != nil {
			t.Fatalf("expected valid JSON, got %q: %v", skipped, err)
		}
		if len(decoded.Diagnostics) != 1 {
			t.Fatalf("expected 1 diagnostic, got %d: %q", len(decoded.Diagnostics), skipped)
		}
		for _, key := range []string{"severity", "message", "reason"} {
			if _, present := decoded.Diagnostics[0][key]; !present {
				t.Fatalf("expected diagnostic key %q to be present, got: %v", key, decoded.Diagnostics[0])
			}
		}
	})
}
