package plain

import (
	"strings"
	"testing"

	"nats/core/ble"
)

func TestName_ReturnsPlain(t *testing.T) {
	if got := (Writer{}).Name(); got != "plain" {
		t.Fatalf("expected Name() to return \"plain\", got %q", got)
	}
}

func TestSelfRegistersAsPlain(t *testing.T) {
	w, ok := ble.GetWriter("plain")
	if !ok {
		t.Fatal("expected \"plain\" writer to be registered via init()")
	}
	if w.Name() != "plain" {
		t.Fatalf("expected registered writer's Name() to be \"plain\", got %q", w.Name())
	}
}

func TestWrite_NoTableDrawingCharacters(t *testing.T) {
	report := ble.Report{
		Devices: []ble.BLEDeviceProfile{
			{Address: "aa:bb:cc:dd:ee:ff", DeviceType: "wearable"},
		},
	}

	out, err := (Writer{}).Write(report)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	s := string(out)
	for _, forbidden := range []string{"|", "+---", "\t"} {
		if strings.Contains(s, forbidden) {
			t.Fatalf("expected no table-drawing characters (%q), got: %q", forbidden, s)
		}
	}
}

func TestWrite_FullyPopulatedDevice(t *testing.T) {
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
	for _, want := range []string{
		"Address: aa:bb:cc:dd:ee:ff",
		"Name: My Watch",
		"Vendor: Acme Corp",
		"Device Type: wearable",
		"Distance: near",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("expected output to contain %q, got: %q", want, s)
		}
	}
}

func TestWrite_EmptyDeviceRendersFallbacks(t *testing.T) {
	report := ble.Report{
		Devices: []ble.BLEDeviceProfile{
			{Address: "aa:bb:cc:dd:ee:ff", Vendor: "unknown"},
		},
	}

	out, err := (Writer{}).Write(report)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	s := string(out)
	// Every absent field, not just Name and DeviceType: the guarantee is that
	// no field is ever silently blank, and Distance was previously left to
	// whatever core/ble happened to supply.
	for _, want := range []string{"Name: unknown", "Vendor: unknown", "Device Type: unknown", "Distance: unknown"} {
		if !strings.Contains(s, want) {
			t.Fatalf("expected %q in the rendered block, got: %q", want, s)
		}
	}
	if !strings.Contains(s, "Address: aa:bb:cc:dd:ee:ff") {
		t.Fatalf("expected the Address to render, got: %q", s)
	}
}

// A blank-but-non-empty value must be treated as absent, otherwise "Name: "
// renders as an empty value and "no name broadcast" is indistinguishable from
// "name is one space".
func TestWrite_BlankValuesRenderPlaceholder(t *testing.T) {
	report := ble.Report{
		Devices: []ble.BLEDeviceProfile{
			{Address: "aa:bb:cc:dd:ee:ff", Name: " ", DeviceType: "\t", DistanceEstimate: "\n"},
		},
	}

	out, err := (Writer{}).Write(report)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	s := string(out)
	for _, want := range []string{"Name: unknown", "Device Type: unknown", "Distance: unknown"} {
		if !strings.Contains(s, want) {
			t.Fatalf("expected %q for a blank value, got: %q", want, s)
		}
	}
}

func TestWrite_MultipleDevicesSeparatedByBlankLine(t *testing.T) {
	report := ble.Report{
		Devices: []ble.BLEDeviceProfile{
			{Address: "aa:bb:cc:dd:ee:ff"},
			{Address: "11:22:33:44:55:66"},
		},
	}

	out, err := (Writer{}).Write(report)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	s := string(out)
	if !strings.Contains(s, "Address: aa:bb:cc:dd:ee:ff") || !strings.Contains(s, "Address: 11:22:33:44:55:66") {
		t.Fatalf("expected both devices to render, got: %q", s)
	}
	blocks := strings.Split(s, "\n\n")
	if len(blocks) != 2 {
		t.Fatalf("expected devices separated by a single blank line, got %d blocks: %q", len(blocks), s)
	}
}

func TestWrite_NoDevices(t *testing.T) {
	out, err := (Writer{}).Write(ble.Report{Devices: nil})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(out) != 0 {
		t.Fatalf("expected empty output for no devices, got: %q", out)
	}
}

func TestWrite_SanitizesEmbeddedNewlineInName(t *testing.T) {
	report := ble.Report{
		Devices: []ble.BLEDeviceProfile{
			{Address: "aa:bb:cc:dd:ee:ff", Name: "evil\nname"},
			{Address: "11:22:33:44:55:66"},
		},
	}

	out, err := (Writer{}).Write(report)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	s := string(out)
	if strings.Contains(s, "evil\nname") {
		t.Fatalf("expected embedded newline in Name to be sanitized, got: %q", s)
	}
	if !strings.Contains(s, "Name: evil name") {
		t.Fatalf("expected newline replaced by space, got: %q", s)
	}
	blocks := strings.Split(s, "\n\n")
	if len(blocks) != 2 {
		t.Fatalf("expected exactly 2 devices separated by a single blank line, got %d blocks: %q", len(blocks), s)
	}
}
