package engine

type Options struct {
	Techniques    []string
	EnrichOptions []string
	OutputFormat  string
	OutputFile    string
	Subnet        string
}

type EventKind string

const (
	EventKindTechniqueStarted EventKind = "TechniqueStarted"
	EventKindAddressProbed    EventKind = "AddressProbed"
	EventKindDeviceFound      EventKind = "DeviceFound"
	EventKindDeviceUpdated    EventKind = "DeviceUpdated"
	EventKindTechniqueSkipped EventKind = "TechniqueSkipped"
	EventKindDone             EventKind = "Done"
)

type Event struct {
	Kind      EventKind
	Technique string
	Address   string
	Reason    string
	Device    Device
	// TotalAddresses is set on TechniqueStarted when the technique (or a
	// prior sweep-based technique in this scan) implements AddressEnumerator
	// — the running total of distinct addresses known to be swept so far,
	// so a driving adapter can compute how many are still pending.
	TotalAddresses int
	Diagnostics    []Diagnostic
	Report         Report
}

type Diagnostic struct {
	Severity string `json:"severity"`
	Message  string `json:"message"`
	Reason   string `json:"reason,omitempty"`
}

type Sighting struct {
	MAC         string
	IP          string
	Technique   string
	ServiceData map[string]string
}

type OpenPort struct {
	Port     int    `json:"port"`
	Protocol string `json:"protocol"`
	State    string `json:"state"`
	Banner   string `json:"banner,omitempty"`
}

type DeviceProfile struct {
	IP         string     `json:"ip"`
	MAC        string     `json:"mac"`
	Hostname   string     `json:"hostname,omitempty"`
	Vendor     string     `json:"vendor,omitempty"`
	DeviceType string     `json:"deviceType,omitempty"`
	OpenPorts  []OpenPort `json:"openPorts,omitempty"`
	// ServiceData carries the mDNS/SSDP service-type strings from
	// Sighting.ServiceData through merge, which identity merging alone
	// doesn't require, so core/engine's classifier has them available as a
	// signal, keyed the same way the contributing Sightings keyed them
	// (e.g. mdns's "hostname"/"name"/"info", ssdp's "type"/"usn"/"server"/
	// "location").
	ServiceData map[string]string `json:"serviceData,omitempty"`
}

type Device = DeviceProfile

// Upsert is the only mutation path for a Device's OpenPorts list: enrichers
// never append to the slice directly. A later write for an
// already-known (Port, Protocol) replaces the existing entry in place (e.g.
// adding a Banner) rather than appending a duplicate.
func (d *DeviceProfile) Upsert(p OpenPort) {
	for i, existing := range d.OpenPorts {
		if existing.Port == p.Port && existing.Protocol == p.Protocol {
			d.OpenPorts[i] = p
			return
		}
	}
	d.OpenPorts = append(d.OpenPorts, p)
}

type Report struct {
	Devices     []Device     `json:"devices"`
	Diagnostics []Diagnostic `json:"diagnostics,omitempty"`
}
