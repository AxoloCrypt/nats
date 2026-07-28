package engine

import "testing"

func TestUpsert_AppendsNewPort(t *testing.T) {
	d := DeviceProfile{IP: "10.0.0.5"}
	d.Upsert(OpenPort{Port: 22, Protocol: "tcp", State: "open"})

	if len(d.OpenPorts) != 1 {
		t.Fatalf("expected 1 port, got %d: %+v", len(d.OpenPorts), d.OpenPorts)
	}
	if d.OpenPorts[0] != (OpenPort{Port: 22, Protocol: "tcp", State: "open"}) {
		t.Fatalf("unexpected port entry: %+v", d.OpenPorts[0])
	}
}

func TestUpsert_SameKeyUpdatesRatherThanAppends(t *testing.T) {
	d := DeviceProfile{IP: "10.0.0.5"}
	d.Upsert(OpenPort{Port: 22, Protocol: "tcp", State: "open"})
	d.Upsert(OpenPort{Port: 22, Protocol: "tcp", State: "open", Banner: "SSH-2.0-OpenSSH"})

	if len(d.OpenPorts) != 1 {
		t.Fatalf("expected the second Upsert for the same (Port, Protocol) to update in place, not append; got %d entries: %+v", len(d.OpenPorts), d.OpenPorts)
	}
	if d.OpenPorts[0].Banner != "SSH-2.0-OpenSSH" {
		t.Fatalf("expected the updated entry to carry the new Banner, got %+v", d.OpenPorts[0])
	}
}

func TestUpsert_DifferentProtocolSamePortIsDistinctEntry(t *testing.T) {
	d := DeviceProfile{IP: "10.0.0.5"}
	d.Upsert(OpenPort{Port: 53, Protocol: "tcp", State: "open"})
	d.Upsert(OpenPort{Port: 53, Protocol: "udp", State: "open"})

	if len(d.OpenPorts) != 2 {
		t.Fatalf("expected (Port, Protocol) to be keyed on both fields, got %d entries: %+v", len(d.OpenPorts), d.OpenPorts)
	}
}

func TestUpsert_PreservesOrderOfOtherEntriesOnUpdate(t *testing.T) {
	d := DeviceProfile{IP: "10.0.0.5"}
	d.Upsert(OpenPort{Port: 22, Protocol: "tcp", State: "open"})
	d.Upsert(OpenPort{Port: 80, Protocol: "tcp", State: "open"})
	d.Upsert(OpenPort{Port: 22, Protocol: "tcp", State: "open", Banner: "SSH-2.0-OpenSSH"})

	if len(d.OpenPorts) != 2 {
		t.Fatalf("expected 2 entries, got %d: %+v", len(d.OpenPorts), d.OpenPorts)
	}
	if d.OpenPorts[0].Port != 22 || d.OpenPorts[0].Banner != "SSH-2.0-OpenSSH" {
		t.Fatalf("expected the updated port to stay at its original index, got %+v", d.OpenPorts)
	}
	if d.OpenPorts[1].Port != 80 {
		t.Fatalf("expected the second entry to remain port 80, got %+v", d.OpenPorts[1])
	}
}
