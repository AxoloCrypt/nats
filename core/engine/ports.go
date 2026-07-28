package engine

import "context"

type DiscoveryTechnique interface {
	Name() string
	RequiresPrivilege() bool
	Run(ctx context.Context, target string) (<-chan Sighting, error)
}

// AddressEnumerator is an optional capability for sweep-based
// DiscoveryTechniques (e.g. arp, icmp) that probe a fixed, enumerable set of
// addresses. It lets core/engine report how many addresses a technique will
// sweep, so a driving adapter can show "still pending" progress (FR-10).
// Listen-based techniques (mdns, ssdp) have no fixed target set and do not
// implement this.
type AddressEnumerator interface {
	EnumerateAddresses(target string) ([]string, error)
}

// PrivilegeProber is an optional capability for DiscoveryTechniques whose
// RequiresPrivilege() probe can fail for reasons other than missing
// privilege (e.g. a missing pcap driver, an unsupported socket type, a
// device that doesn't exist in this environment). When RequiresPrivilege()
// returns true, core/engine checks for this interface to report the real
// underlying error instead of an assumed, possibly-misleading "requires
// privilege" message.
type PrivilegeProber interface {
	ProbePrivilege() (requiresPrivilege bool, err error)
}

type Enricher interface {
	Name() string
	RequiresPrivilege() bool
	Enrich(ctx context.Context, device Device) (Device, error)
}

// Writer renders a final Report as bytes for one output format (AD-7). Each
// implementation (report/table, report/json, report/markdown, report/plain)
// consumes only the Report — never engine internals — so a Writer stays
// decoupled from how a scan produced the devices it renders.
type Writer interface {
	Name() string
	Write(Report) ([]byte, error)
}
