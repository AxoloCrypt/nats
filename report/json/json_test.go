package json

import (
	"encoding/json"
	"strings"
	"testing"

	"nats/core/engine"
)

func TestName_ReturnsJSON(t *testing.T) {
	if got := (Writer{}).Name(); got != "json" {
		t.Fatalf("expected Name() to return \"json\", got %q", got)
	}
}

func TestSelfRegistersAsJSON(t *testing.T) {
	w, ok := engine.GetWriter("json")
	if !ok {
		t.Fatal("expected \"json\" writer to be registered via init()")
	}
	if w.Name() != "json" {
		t.Fatalf("expected registered writer's Name() to be \"json\", got %q", w.Name())
	}
}

func TestWrite_EmptyReportProducesValidJSON(t *testing.T) {
	out, err := (Writer{}).Write(engine.Report{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var decoded engine.Report
	if err := json.Unmarshal(out, &decoded); err != nil {
		t.Fatalf("expected valid JSON, got error %v decoding: %s", err, out)
	}
}

func TestWrite_FullyPopulatedDeviceRoundTrips(t *testing.T) {
	report := engine.Report{
		Devices: []engine.Device{
			{
				IP:         "192.168.1.10",
				MAC:        "aa:bb:cc:dd:ee:ff",
				Hostname:   "printer.local",
				Vendor:     "Acme Corp",
				DeviceType: "printer",
				OpenPorts: []engine.OpenPort{
					{Port: 80, Protocol: "tcp", State: "open"},
					{Port: 9100, Protocol: "tcp", State: "open"},
				},
				ServiceData: map[string]string{"type": "_printer._tcp"},
			},
		},
		Diagnostics: []engine.Diagnostic{
			{Severity: "warning", Message: "icmp skipped", Reason: "requires privilege"},
		},
	}

	out, err := (Writer{}).Write(report)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	s := string(out)
	for _, want := range []string{"192.168.1.10", "aa:bb:cc:dd:ee:ff", "printer.local", "Acme Corp", "printer", "9100", "icmp skipped"} {
		if !strings.Contains(s, want) {
			t.Fatalf("expected output to contain %q, got: %s", want, s)
		}
	}

	var decoded engine.Report
	if err := json.Unmarshal(out, &decoded); err != nil {
		t.Fatalf("expected valid JSON, got error %v decoding: %s", err, out)
	}
	if len(decoded.Devices) != 1 || decoded.Devices[0].IP != "192.168.1.10" {
		t.Fatalf("expected the decoded device to round-trip, got: %+v", decoded.Devices)
	}
	if len(decoded.Devices[0].OpenPorts) != 2 {
		t.Fatalf("expected 2 open ports to round-trip, got: %+v", decoded.Devices[0].OpenPorts)
	}
}

func TestWrite_EmptyEnrichmentFieldsStillSerialize(t *testing.T) {
	report := engine.Report{
		Devices: []engine.Device{
			{IP: "192.168.1.20", MAC: "11:22:33:44:55:66"},
		},
	}

	out, err := (Writer{}).Write(report)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var decoded engine.Report
	if err := json.Unmarshal(out, &decoded); err != nil {
		t.Fatalf("expected valid JSON, got error %v decoding: %s", err, out)
	}
	if len(decoded.Devices) != 1 || decoded.Devices[0].IP != "192.168.1.20" {
		t.Fatalf("expected the empty-enrichment device to round-trip, got: %+v", decoded.Devices)
	}
	if decoded.Devices[0].Hostname != "" || decoded.Devices[0].Vendor != "" {
		t.Fatalf("expected empty Hostname/Vendor to stay empty, got: %+v", decoded.Devices[0])
	}
}
