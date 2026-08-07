package markdown

import (
	"strings"
	"testing"

	"nats/core/ble"
)

func TestName_ReturnsMarkdown(t *testing.T) {
	if got := (Writer{}).Name(); got != "markdown" {
		t.Fatalf("expected Name() to return \"markdown\", got %q", got)
	}
}

func TestSelfRegistersAsMarkdown(t *testing.T) {
	w, ok := ble.GetWriter("markdown")
	if !ok {
		t.Fatal("expected \"markdown\" writer to be registered via init()")
	}
	if w.Name() != "markdown" {
		t.Fatalf("expected registered writer's Name() to be \"markdown\", got %q", w.Name())
	}
}

func TestWrite_RendersHeaderRow(t *testing.T) {
	out, err := (Writer{}).Write(ble.Report{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	s := string(out)
	if !strings.Contains(s, "| Address | Name | Vendor | Device Type | Distance |") {
		t.Fatalf("expected Markdown header row, got: %q", s)
	}
	if !strings.Contains(s, "| --- | --- | --- | --- | --- |") {
		t.Fatalf("expected Markdown separator row, got: %q", s)
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
	for _, want := range []string{"aa:bb:cc:dd:ee:ff", "My Watch", "Acme Corp", "wearable", "near"} {
		if !strings.Contains(s, want) {
			t.Fatalf("expected output to contain %q, got: %q", want, s)
		}
	}
}

// deviceCells returns the single device row's cells, split on the delimiter.
// Assertions below check cells by position rather than with strings.Contains:
// a substring check for "unknown" is satisfied by any one column rendering it
// — including by a fixture that sets Vendor to "unknown" — so it cannot tell
// "every absent field got a placeholder" from "none did". Under the old
// substring form, deleting both fallbacks from Write left this suite green.
func deviceCells(t *testing.T, out []byte) []string {
	t.Helper()
	lines := strings.Split(strings.TrimRight(string(out), "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("expected header + separator + exactly 1 device row, got %d lines: %q", len(lines), out)
	}
	row := strings.TrimSuffix(strings.TrimPrefix(lines[2], "| "), " |")
	return strings.Split(row, " | ")
}

func TestWrite_EveryAbsentFieldRendersPlaceholder(t *testing.T) {
	report := ble.Report{
		Devices: []ble.BLEDeviceProfile{
			{Address: "aa:bb:cc:dd:ee:ff"},
		},
	}

	out, err := (Writer{}).Write(report)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	cells := deviceCells(t, out)
	want := []string{"aa:bb:cc:dd:ee:ff", "unknown", "unknown", "unknown", "unknown"}
	if len(cells) != len(want) {
		t.Fatalf("expected %d cells (every absent field placeheld, none dropped), got %d: %v", len(want), len(cells), cells)
	}
	for i, w := range want {
		if cells[i] != w {
			t.Fatalf("cell %d: expected %q, got %q (row: %v)", i, w, cells[i], cells)
		}
	}
	if want[3] != ble.DeviceTypeUnknown {
		t.Fatalf("DeviceType placeholder must stay in sync with ble.DeviceTypeUnknown (%q)", ble.DeviceTypeUnknown)
	}
}

// A trailing backslash defeats pipe-only escaping: "x\" + "|" becomes "x\\|",
// which CommonMark reads as an escaped backslash followed by a live cell
// delimiter, splitting the row into an extra column.
func TestWrite_BackslashBeforePipeDoesNotSplitRow(t *testing.T) {
	report := ble.Report{
		Devices: []ble.BLEDeviceProfile{
			{
				Address:          "aa:bb:cc:dd:ee:ff",
				Name:             `x\|SPOOF`,
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

	cells := deviceCells(t, out)
	if len(cells) != 5 {
		t.Fatalf("expected exactly 5 cells, got %d: %v", len(cells), cells)
	}
	if cells[1] != `x\\\|SPOOF` {
		t.Fatalf("expected backslash escaped before pipe, got %q", cells[1])
	}
	if cells[2] != "Acme Corp" || cells[4] != "near" {
		t.Fatalf("expected trailing columns undisplaced, got: %v", cells)
	}
}

func TestWrite_NoDevices(t *testing.T) {
	out, err := (Writer{}).Write(ble.Report{Devices: nil})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	lines := strings.Split(strings.TrimRight(string(out), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected only header + separator lines with no devices, got: %q", out)
	}
}

func TestWrite_EscapesPipeAndNewlineInName(t *testing.T) {
	report := ble.Report{
		Devices: []ble.BLEDeviceProfile{
			{Address: "aa:bb:cc:dd:ee:ff", Name: "evil|name\nhere"},
		},
	}

	out, err := (Writer{}).Write(report)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	s := string(out)
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("expected header + separator + exactly one device row, got %d lines: %q", len(lines), s)
	}
	if strings.Contains(s, "evil|name") {
		t.Fatalf("expected unescaped \"|\" in Name to be escaped, got: %q", s)
	}
	if !strings.Contains(s, `evil\|name here`) {
		t.Fatalf("expected escaped pipe and newline-replaced-by-space, got: %q", s)
	}
}
