package table

import (
	"regexp"
	"strings"
	"testing"

	"nats/core/ble"
)

func TestName_ReturnsTable(t *testing.T) {
	if got := (Writer{}).Name(); got != "table" {
		t.Fatalf("expected Name() to return \"table\", got %q", got)
	}
}

func TestSelfRegistersAsTable(t *testing.T) {
	w, ok := ble.GetWriter("table")
	if !ok {
		t.Fatal("expected \"table\" writer to be registered via init()")
	}
	if w.Name() != "table" {
		t.Fatalf("expected registered writer's Name() to be \"table\", got %q", w.Name())
	}
}

func TestWrite_RendersHeader(t *testing.T) {
	out, err := (Writer{}).Write(ble.Report{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	s := string(out)
	for _, want := range []string{"ADDRESS", "NAME", "VENDOR", "DEVICE TYPE", "DISTANCE"} {
		if !strings.Contains(s, want) {
			t.Fatalf("expected header to contain %q, got: %q", want, s)
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
	for _, want := range []string{"aa:bb:cc:dd:ee:ff", "My Watch", "Acme Corp", "wearable", "near"} {
		if !strings.Contains(s, want) {
			t.Fatalf("expected output to contain %q, got: %q", want, s)
		}
	}
}

// deviceRow returns the single device row's whitespace-separated cells.
// Assertions below check cells by position rather than with strings.Contains:
// a substring check for "unknown" is satisfied by any one column rendering
// it, so it cannot tell "every absent field got a placeholder" from "one did
// and the rest are blank" — which is precisely the guarantee being made.
func deviceRow(t *testing.T, out []byte) []string {
	t.Helper()
	lines := strings.Split(strings.TrimRight(string(out), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected header + exactly 1 device row, got %d lines: %q", len(lines), out)
	}
	return strings.Fields(lines[1])
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

	fields := deviceRow(t, out)
	want := []string{"aa:bb:cc:dd:ee:ff", "unknown", "unknown", "unknown", "unknown"}
	if len(fields) != len(want) {
		t.Fatalf("expected %d columns (every absent field placeheld, none dropped), got %d: %v", len(want), len(fields), fields)
	}
	for i, w := range want {
		if fields[i] != w {
			t.Fatalf("column %d: expected %q, got %q (row: %v)", i, w, fields[i], fields)
		}
	}
	if want[3] != ble.DeviceTypeUnknown {
		t.Fatalf("DeviceType placeholder must stay in sync with ble.DeviceTypeUnknown (%q)", ble.DeviceTypeUnknown)
	}
}

// A blank-but-non-empty value is absent for rendering purposes: an == ""
// check would let it through and produce a silently empty cell, making "no
// name broadcast" indistinguishable from "name is one space".
func TestWrite_BlankNameAndDeviceTypeRenderPlaceholder(t *testing.T) {
	report := ble.Report{
		Devices: []ble.BLEDeviceProfile{
			{Address: "aa:bb:cc:dd:ee:ff", Name: " ", Vendor: "Acme Corp", DeviceType: "\t", DistanceEstimate: "near"},
		},
	}

	out, err := (Writer{}).Write(report)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	fields := deviceRow(t, out)
	want := []string{"aa:bb:cc:dd:ee:ff", "unknown", "Acme", "Corp", "unknown", "near"}
	if len(fields) != len(want) {
		t.Fatalf("expected blank Name and DeviceType to render as placeholders, got %v", fields)
	}
	if fields[1] != "unknown" || fields[4] != "unknown" {
		t.Fatalf("expected blank Name and DeviceType to render as \"unknown\", got %v", fields)
	}
}

// A tab in a broadcast name would otherwise forge whole columns: "\t" is this
// writer's own delimiter, so an unsanitized name can fill VENDOR and DEVICE
// TYPE with attacker-chosen text and push the real values past the header.
func TestWrite_TabInNameDoesNotForgeColumns(t *testing.T) {
	report := ble.Report{
		Devices: []ble.BLEDeviceProfile{
			{
				Address:          "aa:bb:cc:dd:ee:ff",
				Name:             "evil\tSPOOFVENDOR\tSPOOFTYPE",
				Vendor:           "RealVendor",
				DeviceType:       "wearable",
				DistanceEstimate: "~1.0m",
			},
		},
	}

	out, err := (Writer{}).Write(report)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Cells must be split on tabwriter's padding (2+ spaces), not with
	// strings.Fields: Fields treats a tab and a space identically, so it
	// cannot distinguish a sanitized name (one NAME cell containing
	// "evil SPOOFVENDOR SPOOFTYPE") from an unsanitized one (three separate
	// tabwriter cells) — the exact difference this test exists to catch.
	lines := strings.Split(strings.TrimRight(string(out), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected header + exactly 1 device row, got %d lines: %q", len(lines), out)
	}
	cellSplit := regexp.MustCompile(` {2,}`)
	cells := cellSplit.Split(strings.TrimRight(lines[1], " "), -1)
	want := []string{"aa:bb:cc:dd:ee:ff", "evil SPOOFVENDOR SPOOFTYPE", "RealVendor", "wearable", "~1.0m"}
	if len(cells) != len(want) {
		t.Fatalf("expected the tabbed Name to stay inside 1 cell for %d columns total, got %d: %v", len(want), len(cells), cells)
	}
	for i, w := range want {
		if cells[i] != w {
			t.Fatalf("column %d: expected %q, got %q (row: %v)", i, w, cells[i], cells)
		}
	}
	// The header must still have as many columns as the row: a forged cell
	// would push the real values into columns the header never declares.
	headerCells := cellSplit.Split(strings.TrimRight(lines[0], " "), -1)
	if len(headerCells) != len(cells) {
		t.Fatalf("row has %d columns but header has %d: %q", len(cells), len(headerCells), out)
	}
}

func TestWrite_NoDevices(t *testing.T) {
	out, err := (Writer{}).Write(ble.Report{Devices: nil})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	lines := strings.Split(strings.TrimRight(string(out), "\n"), "\n")
	if len(lines) != 1 {
		t.Fatalf("expected only the header line with no devices, got: %q", out)
	}
}

func TestWrite_SanitizesEmbeddedNewlineInName(t *testing.T) {
	report := ble.Report{
		Devices: []ble.BLEDeviceProfile{
			{Address: "aa:bb:cc:dd:ee:ff", Name: "evil\nname"},
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
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected header + exactly 1 device row, got %d lines: %q", len(lines), s)
	}
}
