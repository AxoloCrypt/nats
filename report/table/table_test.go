package table

import (
	"strings"
	"testing"

	"nats/core/engine"
)

func TestName_ReturnsTable(t *testing.T) {
	if got := (Writer{}).Name(); got != "table" {
		t.Fatalf("expected Name() to return \"table\", got %q", got)
	}
}

func TestSelfRegistersAsTable(t *testing.T) {
	w, ok := engine.GetWriter("table")
	if !ok {
		t.Fatal("expected \"table\" writer to be registered via init()")
	}
	if w.Name() != "table" {
		t.Fatalf("expected registered writer's Name() to be \"table\", got %q", w.Name())
	}
}

func TestWrite_RendersHeader(t *testing.T) {
	out, err := (Writer{}).Write(engine.Report{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(string(out), "IP") || !strings.Contains(string(out), "DEVICE TYPE") {
		t.Fatalf("expected header row, got: %q", out)
	}
}

func TestWrite_FullyPopulatedDevice(t *testing.T) {
	report := engine.Report{
		Devices: []engine.Device{
			{
				IP:         "192.168.1.10",
				MAC:        "aa:bb:cc:dd:ee:ff",
				Hostname:   "printer.local",
				Vendor:     "Acme Corp",
				DeviceType: "printer",
				OpenPorts: []engine.OpenPort{
					{Port: 80, Protocol: "tcp"},
					{Port: 9100, Protocol: "tcp"},
				},
			},
		},
	}

	out, err := (Writer{}).Write(report)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	s := string(out)
	for _, want := range []string{"192.168.1.10", "aa:bb:cc:dd:ee:ff", "printer.local", "Acme Corp", "80/tcp,9100/tcp", "printer"} {
		if !strings.Contains(s, want) {
			t.Fatalf("expected output to contain %q, got: %q", want, s)
		}
	}
}

func TestWrite_EmptyDeviceRendersUnknownTypeAndBlankFields(t *testing.T) {
	report := engine.Report{
		Devices: []engine.Device{
			{IP: "192.168.1.20", MAC: "11:22:33:44:55:66"},
		},
	}

	out, err := (Writer{}).Write(report)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	s := string(out)
	if !strings.Contains(s, "unknown") {
		t.Fatalf("expected empty DeviceType to render as 'unknown', got: %q", s)
	}

	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected header + 1 device row, got %d lines: %q", len(lines), s)
	}
	fields := strings.Fields(lines[1])
	// IP, MAC, DEVICE TYPE(unknown) present; Hostname/Vendor/OpenPorts columns
	// collapse to nothing under tabwriter, so only 3 fields should appear.
	if len(fields) != 3 {
		t.Fatalf("expected 3 non-empty fields (IP, MAC, unknown), got %v from line %q", fields, lines[1])
	}
}

func TestWrite_MixOfEmptyAndPopulatedDevices(t *testing.T) {
	report := engine.Report{
		Devices: []engine.Device{
			{IP: "192.168.1.10", MAC: "aa:bb:cc:dd:ee:ff"},
			{IP: "192.168.1.20", MAC: "11:22:33:44:55:66", Hostname: "host.local", Vendor: "Vendor Inc", DeviceType: "router"},
		},
	}

	out, err := (Writer{}).Write(report)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	s := string(out)
	if !strings.Contains(s, "unknown") {
		t.Fatalf("expected the empty device's row to render 'unknown', got: %q", s)
	}
	if !strings.Contains(s, "router") {
		t.Fatalf("expected the populated device's row to render 'router', got: %q", s)
	}
}

func TestWrite_NoDevices(t *testing.T) {
	out, err := (Writer{}).Write(engine.Report{Devices: nil})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	lines := strings.Split(strings.TrimRight(string(out), "\n"), "\n")
	if len(lines) != 1 {
		t.Fatalf("expected only the header line with no devices, got: %q", out)
	}
}
