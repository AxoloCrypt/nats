package plain

import (
	"strings"
	"testing"

	"nats/core/engine"
)

func TestName_ReturnsPlain(t *testing.T) {
	if got := (Writer{}).Name(); got != "plain" {
		t.Fatalf("expected Name() to return \"plain\", got %q", got)
	}
}

func TestSelfRegistersAsPlain(t *testing.T) {
	w, ok := engine.GetWriter("plain")
	if !ok {
		t.Fatal("expected \"plain\" writer to be registered via init()")
	}
	if w.Name() != "plain" {
		t.Fatalf("expected registered writer's Name() to be \"plain\", got %q", w.Name())
	}
}

func TestWrite_NoTableDrawingCharacters(t *testing.T) {
	report := engine.Report{
		Devices: []engine.Device{
			{IP: "192.168.1.10", MAC: "aa:bb:cc:dd:ee:ff", DeviceType: "printer"},
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
	for _, want := range []string{
		"IP: 192.168.1.10",
		"MAC: aa:bb:cc:dd:ee:ff",
		"Hostname: printer.local",
		"Vendor: Acme Corp",
		"Open Ports: 80/tcp,9100/tcp",
		"Device Type: printer",
	} {
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
	if !strings.Contains(s, "Device Type: unknown") {
		t.Fatalf("expected empty DeviceType to render as 'unknown', got: %q", s)
	}
	if !strings.Contains(s, "Hostname: \n") {
		t.Fatalf("expected empty Hostname to render blank, got: %q", s)
	}
}

func TestWrite_MultipleDevicesSeparatedByBlankLine(t *testing.T) {
	report := engine.Report{
		Devices: []engine.Device{
			{IP: "192.168.1.10", MAC: "aa:bb:cc:dd:ee:ff"},
			{IP: "192.168.1.20", MAC: "11:22:33:44:55:66"},
		},
	}

	out, err := (Writer{}).Write(report)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	s := string(out)
	if !strings.Contains(s, "IP: 192.168.1.10") || !strings.Contains(s, "IP: 192.168.1.20") {
		t.Fatalf("expected both devices to render, got: %q", s)
	}
	blocks := strings.Split(s, "\n\n")
	if len(blocks) != 2 {
		t.Fatalf("expected devices separated by a single blank line, got %d blocks: %q", len(blocks), s)
	}
}

func TestWrite_NoDevices(t *testing.T) {
	out, err := (Writer{}).Write(engine.Report{Devices: nil})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(out) != 0 {
		t.Fatalf("expected empty output for no devices, got: %q", out)
	}
}

func TestWrite_SanitizesEmbeddedNewlineInFields(t *testing.T) {
	report := engine.Report{
		Devices: []engine.Device{
			{IP: "192.168.1.10", MAC: "aa:bb:cc:dd:ee:ff", Hostname: "evil\nhostname", Vendor: "Acme\nCorp"},
			{IP: "192.168.1.20", MAC: "11:22:33:44:55:66"},
		},
	}

	out, err := (Writer{}).Write(report)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	s := string(out)
	if strings.Contains(s, "evil\nhostname") || strings.Contains(s, "Acme\nCorp") {
		t.Fatalf("expected embedded newlines in Hostname/Vendor to be sanitized, got: %q", s)
	}
	if !strings.Contains(s, "Hostname: evil hostname") || !strings.Contains(s, "Vendor: Acme Corp") {
		t.Fatalf("expected newline replaced by space, got: %q", s)
	}
	// A sanitized embedded newline must not be mistaken for the blank-line
	// separator between devices.
	blocks := strings.Split(s, "\n\n")
	if len(blocks) != 2 {
		t.Fatalf("expected exactly 2 devices separated by a single blank line, got %d blocks: %q", len(blocks), s)
	}
}
