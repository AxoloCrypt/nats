package markdown

import (
	"strings"
	"testing"

	"nats/core/engine"
)

func TestName_ReturnsMarkdown(t *testing.T) {
	if got := (Writer{}).Name(); got != "markdown" {
		t.Fatalf("expected Name() to return \"markdown\", got %q", got)
	}
}

func TestSelfRegistersAsMarkdown(t *testing.T) {
	w, ok := engine.GetWriter("markdown")
	if !ok {
		t.Fatal("expected \"markdown\" writer to be registered via init()")
	}
	if w.Name() != "markdown" {
		t.Fatalf("expected registered writer's Name() to be \"markdown\", got %q", w.Name())
	}
}

func TestWrite_RendersHeaderRow(t *testing.T) {
	out, err := (Writer{}).Write(engine.Report{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	s := string(out)
	if !strings.Contains(s, "| IP | MAC | Hostname | Vendor | Open Ports | Device Type |") {
		t.Fatalf("expected Markdown header row, got: %q", s)
	}
	if !strings.Contains(s, "| --- | --- | --- | --- | --- | --- |") {
		t.Fatalf("expected Markdown separator row, got: %q", s)
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

func TestWrite_EmptyDeviceRendersUnknownType(t *testing.T) {
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
	if !strings.Contains(s, "192.168.1.20") || !strings.Contains(s, "11:22:33:44:55:66") {
		t.Fatalf("expected IP/MAC to render, got: %q", s)
	}
}

func TestWrite_NoDevices(t *testing.T) {
	out, err := (Writer{}).Write(engine.Report{Devices: nil})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	lines := strings.Split(strings.TrimRight(string(out), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected only header + separator lines with no devices, got: %q", out)
	}
}

func TestWrite_EscapesPipeAndNewlineInFields(t *testing.T) {
	report := engine.Report{
		Devices: []engine.Device{
			{IP: "192.168.1.10", MAC: "aa:bb:cc:dd:ee:ff", Hostname: "evil|host\nname", Vendor: "Ac|me\nCorp"},
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
	if strings.Contains(s, "evil|host") || strings.Contains(s, "Ac|me") {
		t.Fatalf("expected unescaped \"|\" in field values to be escaped, got: %q", s)
	}
	if !strings.Contains(s, `evil\|host name`) || !strings.Contains(s, `Ac\|me Corp`) {
		t.Fatalf("expected escaped pipe and newline-replaced-by-space, got: %q", s)
	}
}
