package engine

import (
	"context"
	"errors"
	"net"
	"reflect"
	"strings"
	"testing"
	"time"
)

var errRunFailed = errors.New("technique failed")

type mockTechnique struct {
	name              string
	sightings         []Sighting
	runErr            error
	requiresPrivilege bool
}

func (m *mockTechnique) Name() string            { return m.name }
func (m *mockTechnique) RequiresPrivilege() bool { return m.requiresPrivilege }
func (m *mockTechnique) Run(ctx context.Context, target string) (<-chan Sighting, error) {
	if m.runErr != nil {
		return nil, m.runErr
	}
	ch := make(chan Sighting, len(m.sightings))
	for _, s := range m.sightings {
		ch <- s
	}
	close(ch)
	return ch, nil
}

// sweepMockTechnique additionally implements AddressEnumerator, simulating a
// sweep-based technique (arp/icmp) for tests that need TotalAddresses.
type sweepMockTechnique struct {
	mockTechnique
	addresses    []string
	enumerateErr error
}

func (m *sweepMockTechnique) EnumerateAddresses(target string) ([]string, error) {
	if m.enumerateErr != nil {
		return nil, m.enumerateErr
	}
	return m.addresses, nil
}

// privilegeProbeMockTechnique additionally implements PrivilegeProber,
// simulating a technique whose privilege probe can report why it failed.
type privilegeProbeMockTechnique struct {
	mockTechnique
	probeErr error
}

func (m *privilegeProbeMockTechnique) ProbePrivilege() (bool, error) {
	return m.requiresPrivilege, m.probeErr
}

func TestRun_EmitsDoneAndClosesChannel(t *testing.T) {
	orig := netInterfaces
	defer func() { netInterfaces = orig }()

	netInterfaces = func() ([]net.Interface, error) {
		iface := net.Interface{Index: 1, Name: "eth0", Flags: net.FlagUp}
		return []net.Interface{iface}, nil
	}

	ctx := context.Background()
	events, err := Run(ctx, Options{Subnet: "192.168.1.0/24"})
	if err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}

	count := 0
	for evt := range events {
		if evt.Kind == EventKindDone {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("expected exactly 1 Done event, got %d", count)
	}
}

func TestRun_ReportsDiagnosticOnNoInterface(t *testing.T) {
	orig := netInterfaces
	defer func() { netInterfaces = orig }()

	netInterfaces = func() ([]net.Interface, error) {
		return nil, nil
	}

	ctx := context.Background()
	events, err := Run(ctx, Options{})
	if err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}

	foundNetworkError := false
	for evt := range events {
		if evt.Kind != EventKindDone {
			continue
		}
		for _, d := range evt.Diagnostics {
			if d.Severity == "error" && d.Message == "no active network interface found" {
				foundNetworkError = true
				if d.Reason == "" {
					t.Fatal("expected non-empty reason")
				}
			}
		}
	}
	if !foundNetworkError {
		t.Fatal("expected a Diagnostic with Severity error for missing interface in Done event")
	}
}

func TestRun_ReturnsEmptyReport(t *testing.T) {
	orig := netInterfaces
	defer func() { netInterfaces = orig }()

	netInterfaces = func() ([]net.Interface, error) {
		iface := net.Interface{Index: 1, Name: "eth0", Flags: net.FlagUp}
		return []net.Interface{iface}, nil
	}

	ctx := context.Background()
	events, err := Run(ctx, Options{Subnet: "192.168.1.0/24"})
	if err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}

	for evt := range events {
		if evt.Kind == EventKindDone {
			if len(evt.Report.Devices) != 0 {
				t.Fatal("expected empty devices in report")
			}
		}
	}
}

func TestRun_ReturnsEmptyReportOnNoInterface(t *testing.T) {
	orig := netInterfaces
	defer func() { netInterfaces = orig }()

	netInterfaces = func() ([]net.Interface, error) {
		return nil, nil
	}

	ctx := context.Background()
	events, err := Run(ctx, Options{})
	if err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}

	foundError := false
	for evt := range events {
		if evt.Kind != EventKindDone {
			continue
		}
		if len(evt.Report.Devices) != 0 {
			t.Fatal("expected empty devices in report when no interface")
		}
		for _, d := range evt.Diagnostics {
			if d.Severity == "error" {
				foundError = true
			}
		}
	}
	if !foundError {
		t.Fatal("expected at least one error diagnostic")
	}
}

func TestDefaultOptions_ReturnsArpTechnique(t *testing.T) {
	opts := DefaultOptions()
	if len(opts.Techniques) != 1 || opts.Techniques[0] != "arp" {
		t.Fatalf("expected Techniques [\"arp\"], got %v", opts.Techniques)
	}
	if opts.Subnet != "" {
		t.Fatalf("expected empty subnet, got %s", opts.Subnet)
	}
}

func TestRun_DefaultTechniqueExecutesArp(t *testing.T) {
	orig := netInterfaces
	defer func() { netInterfaces = orig }()

	origArp := registry["arp"]
	defer func() {
		if origArp != nil {
			registry["arp"] = origArp
		} else {
			delete(registry, "arp")
		}
	}()

	netInterfaces = func() ([]net.Interface, error) {
		iface := net.Interface{Index: 1, Name: "eth0", Flags: net.FlagUp}
		return []net.Interface{iface}, nil
	}

	RegisterTechnique(&mockTechnique{
		name: "arp",
		sightings: []Sighting{
			{IP: "192.168.1.10", MAC: "aa:bb:cc:dd:ee:ff", Technique: "arp"},
		},
	})

	ctx := context.Background()
	events, err := Run(ctx, DefaultOptions())
	if err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}

	var startedTechniques []string
	var devices []Device
	for evt := range events {
		switch evt.Kind {
		case EventKindTechniqueStarted:
			startedTechniques = append(startedTechniques, evt.Technique)
		case EventKindDone:
			devices = evt.Report.Devices
		}
	}

	if len(startedTechniques) != 1 || startedTechniques[0] != "arp" {
		t.Fatalf("expected exactly 'arp' technique started, got %v", startedTechniques)
	}
	if len(devices) != 1 {
		t.Fatalf("expected 1 device, got %d", len(devices))
	}
	if devices[0].IP != "192.168.1.10" {
		t.Fatalf("expected device IP 192.168.1.10, got %s", devices[0].IP)
	}
	if devices[0].MAC != "aa:bb:cc:dd:ee:ff" {
		t.Fatalf("expected device MAC aa:bb:cc:dd:ee:ff, got %s", devices[0].MAC)
	}
}

func TestRun_MergesSightingsAcrossTechniques(t *testing.T) {
	orig := netInterfaces
	defer func() { netInterfaces = orig }()

	origArp := registry["arp"]
	origMdns := registry["mdns"]
	defer func() {
		if origArp != nil {
			registry["arp"] = origArp
		} else {
			delete(registry, "arp")
		}
		if origMdns != nil {
			registry["mdns"] = origMdns
		} else {
			delete(registry, "mdns")
		}
	}()

	netInterfaces = func() ([]net.Interface, error) {
		iface := net.Interface{Index: 1, Name: "eth0", Flags: net.FlagUp}
		return []net.Interface{iface}, nil
	}

	RegisterTechnique(&mockTechnique{
		name: "arp",
		sightings: []Sighting{
			{IP: "192.168.1.10", MAC: "aa:bb:cc:dd:ee:ff", Technique: "arp"},
		},
	})
	RegisterTechnique(&mockTechnique{
		name: "mdns",
		sightings: []Sighting{
			{IP: "192.168.1.10", Technique: "mdns"},
		},
	})

	ctx := context.Background()
	events, err := Run(ctx, Options{Techniques: []string{"arp", "mdns"}})
	if err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}

	var devices []Device
	for evt := range events {
		if evt.Kind == EventKindDone {
			devices = evt.Report.Devices
		}
	}

	if len(devices) != 1 {
		t.Fatalf("expected the arp and mdns sightings to merge into 1 device, got %d", len(devices))
	}
	if devices[0].IP != "192.168.1.10" {
		t.Fatalf("expected device IP 192.168.1.10, got %s", devices[0].IP)
	}
	if devices[0].MAC != "aa:bb:cc:dd:ee:ff" {
		t.Fatalf("expected device MAC preserved from arp sighting, got %s", devices[0].MAC)
	}
}

func TestRun_SkipsUnregisteredTechnique(t *testing.T) {
	orig := netInterfaces
	defer func() { netInterfaces = orig }()

	netInterfaces = func() ([]net.Interface, error) {
		iface := net.Interface{Index: 1, Name: "eth0", Flags: net.FlagUp}
		return []net.Interface{iface}, nil
	}

	opts := Options{
		Subnet:     "192.168.1.0/24",
		Techniques: []string{"nonexistent"},
	}

	ctx := context.Background()
	events, err := Run(ctx, opts)
	if err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}

	foundError := false
	for evt := range events {
		if evt.Kind != EventKindDone {
			continue
		}
		for _, d := range evt.Diagnostics {
			if d.Severity == "error" && d.Message == "technique \"nonexistent\" not found in registry" {
				foundError = true
			}
		}
	}
	if !foundError {
		t.Fatal("expected error diagnostic for unregistered technique")
	}
}

func TestRun_TechniqueErrorReported(t *testing.T) {
	orig := netInterfaces
	defer func() { netInterfaces = orig }()

	origArp := registry["arp"]
	defer func() {
		if origArp != nil {
			registry["arp"] = origArp
		} else {
			delete(registry, "arp")
		}
	}()

	netInterfaces = func() ([]net.Interface, error) {
		iface := net.Interface{Index: 1, Name: "eth0", Flags: net.FlagUp}
		return []net.Interface{iface}, nil
	}

	RegisterTechnique(&mockTechnique{
		name:   "arp",
		runErr: errRunFailed,
	})

	ctx := context.Background()
	events, err := Run(ctx, DefaultOptions())
	if err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}

	foundWarning := false
	for evt := range events {
		if evt.Kind != EventKindDone {
			continue
		}
		for _, d := range evt.Diagnostics {
			if d.Severity == "warning" && d.Message == "technique \"arp\": technique failed" {
				foundWarning = true
			}
		}
	}
	if !foundWarning {
		t.Fatal("expected warning diagnostic for technique error")
	}
}

type delayedCloseTechnique struct {
	name              string
	requiresPrivilege bool
	sightings         []Sighting
	done              chan struct{}
}

func (m *delayedCloseTechnique) Name() string            { return m.name }
func (m *delayedCloseTechnique) RequiresPrivilege() bool { return m.requiresPrivilege }
func (m *delayedCloseTechnique) Run(ctx context.Context, target string) (<-chan Sighting, error) {
	ch := make(chan Sighting, len(m.sightings))
	go func() {
		for _, s := range m.sightings {
			ch <- s
		}
		<-m.done
		close(ch)
	}()
	return ch, nil
}

// EnumerateAddresses marks delayedCloseTechnique as sweep-based (like the
// real arp/icmp techniques it stands in for in tests), so it is not bound by
// engine's safetyNetTimeout the way a listen-based technique would be.
func (m *delayedCloseTechnique) EnumerateAddresses(target string) ([]string, error) {
	addrs := make([]string, len(m.sightings))
	for i, s := range m.sightings {
		addrs[i] = s.IP
	}
	return addrs, nil
}

type panicOnRunTechnique struct {
	name              string
	requiresPrivilege bool
}

func (m *panicOnRunTechnique) Name() string            { return m.name }
func (m *panicOnRunTechnique) RequiresPrivilege() bool { return m.requiresPrivilege }
func (m *panicOnRunTechnique) Run(ctx context.Context, target string) (<-chan Sighting, error) {
	panic("Run should not be called on a skipped technique")
}

// neverEndingTechnique simulates a listen-based technique (mdns, ssdp): it
// never closes its Sighting channel on its own and only exits via ctx.Done().
// It deliberately does not implement AddressEnumerator.
type neverEndingTechnique struct {
	name string
}

func (m *neverEndingTechnique) Name() string            { return m.name }
func (m *neverEndingTechnique) RequiresPrivilege() bool { return false }
func (m *neverEndingTechnique) Run(ctx context.Context, target string) (<-chan Sighting, error) {
	ch := make(chan Sighting)
	go func() {
		defer close(ch)
		<-ctx.Done()
	}()
	return ch, nil
}

// registerNoOpDefaultEnrichers temporarily registers no-op fakes under
// DefaultOptions()'s enricher names ("dns", "oui", "tcpconnect") so tests
// unrelated to enrichment aren't affected by the "enricher not found"
// warning diagnostic once EnrichOptions falls back to the default list. The
// real "dns"/"oui" enrichers only self-register via the enrich/dns and
// enrich/oui packages, which core/engine cannot import (they import
// core/engine), so they're never registered inside this package's tests.
func registerNoOpDefaultEnrichers(t *testing.T) {
	t.Helper()
	for _, name := range DefaultOptions().EnrichOptions {
		orig, hadOrig := enricherRegistry[name]
		RegisterEnricher(&fakeEnricher{name: name})
		t.Cleanup(func() {
			if hadOrig {
				enricherRegistry[name] = orig
			} else {
				delete(enricherRegistry, name)
			}
		})
	}
}

func TestRun_SkipsTechniqueWhenPrivilegeRequired(t *testing.T) {
	orig := netInterfaces
	defer func() { netInterfaces = orig }()
	registerNoOpDefaultEnrichers(t)

	origPrivileged := registry["privileged"]
	defer func() {
		if origPrivileged != nil {
			registry["privileged"] = origPrivileged
		} else {
			delete(registry, "privileged")
		}
	}()

	netInterfaces = func() ([]net.Interface, error) {
		iface := net.Interface{Index: 1, Name: "eth0", Flags: net.FlagUp}
		return []net.Interface{iface}, nil
	}

	RegisterTechnique(&mockTechnique{
		name:              "privileged",
		requiresPrivilege: true,
	})

	ctx := context.Background()
	events, err := Run(ctx, Options{Techniques: []string{"privileged"}})
	if err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}

	var skippedEvents []string
	var skippedReason string
	var warningDiags []Diagnostic
	for evt := range events {
		if evt.Kind == EventKindTechniqueSkipped {
			skippedEvents = append(skippedEvents, evt.Technique)
			skippedReason = evt.Reason
		}
		if evt.Kind == EventKindDone {
			for _, d := range evt.Diagnostics {
				if d.Severity == "warning" {
					warningDiags = append(warningDiags, d)
				}
			}
		}
	}

	if len(skippedEvents) != 1 || skippedEvents[0] != "privileged" {
		t.Fatalf("expected 1 TechniqueSkipped event for 'privileged', got %v", skippedEvents)
	}
	if skippedReason == "" {
		t.Fatal("expected non-empty reason on the TechniqueSkipped event")
	}

	if len(warningDiags) != 1 {
		t.Fatalf("expected 1 warning diagnostic, got %d", len(warningDiags))
	}
	if warningDiags[0].Message != "privileged skipped" {
		t.Fatalf("expected diagnostic message 'privileged skipped', got %q", warningDiags[0].Message)
	}
	if warningDiags[0].Reason == "" {
		t.Fatal("expected non-empty reason on the diagnostic")
	}
}

func TestRun_TechniqueSkippedReasonSurfacesUnderlyingProbeError(t *testing.T) {
	orig := netInterfaces
	defer func() { netInterfaces = orig }()

	origPrivileged := registry["flaky"]
	defer func() {
		if origPrivileged != nil {
			registry["flaky"] = origPrivileged
		} else {
			delete(registry, "flaky")
		}
	}()

	netInterfaces = func() ([]net.Interface, error) {
		iface := net.Interface{Index: 1, Name: "eth0", Flags: net.FlagUp}
		return []net.Interface{iface}, nil
	}

	probeErr := errors.New("no such device exists")
	RegisterTechnique(&privilegeProbeMockTechnique{
		mockTechnique: mockTechnique{name: "flaky", requiresPrivilege: true},
		probeErr:      probeErr,
	})

	ctx := context.Background()
	events, err := Run(ctx, Options{Techniques: []string{"flaky"}})
	if err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}

	var reason string
	for evt := range events {
		if evt.Kind == EventKindTechniqueSkipped && evt.Technique == "flaky" {
			reason = evt.Reason
		}
	}

	if !strings.Contains(reason, probeErr.Error()) {
		t.Fatalf("expected skip reason to surface the underlying probe error %q, got %q", probeErr.Error(), reason)
	}
	if strings.Contains(reason, "requires privilege not available") {
		t.Fatalf("expected the generic fallback message to be replaced by the real error, got %q", reason)
	}
}

func TestRun_CompletesWithUnprivilegedWhenOneSkipped(t *testing.T) {
	orig := netInterfaces
	defer func() { netInterfaces = orig }()
	registerNoOpDefaultEnrichers(t)

	origArp := registry["arp"]
	origPrivileged := registry["privileged"]
	defer func() {
		if origArp != nil {
			registry["arp"] = origArp
		} else {
			delete(registry, "arp")
		}
		if origPrivileged != nil {
			registry["privileged"] = origPrivileged
		} else {
			delete(registry, "privileged")
		}
	}()

	netInterfaces = func() ([]net.Interface, error) {
		iface := net.Interface{Index: 1, Name: "eth0", Flags: net.FlagUp}
		return []net.Interface{iface}, nil
	}

	RegisterTechnique(&mockTechnique{
		name:              "privileged",
		requiresPrivilege: true,
	})
	RegisterTechnique(&mockTechnique{
		name: "arp",
		sightings: []Sighting{
			{IP: "192.168.1.10", MAC: "aa:bb:cc:dd:ee:ff", Technique: "arp"},
		},
	})

	ctx := context.Background()
	events, err := Run(ctx, Options{Techniques: []string{"privileged", "arp"}})
	if err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}

	var devices []Device
	var skippedEvents []string
	var warningDiags []Diagnostic
	for evt := range events {
		switch evt.Kind {
		case EventKindTechniqueSkipped:
			skippedEvents = append(skippedEvents, evt.Technique)
		case EventKindDone:
			devices = evt.Report.Devices
			for _, d := range evt.Diagnostics {
				if d.Severity == "warning" {
					warningDiags = append(warningDiags, d)
				}
			}
		}
	}

	if len(skippedEvents) != 1 || skippedEvents[0] != "privileged" {
		t.Fatalf("expected 1 TechniqueSkipped for 'privileged', got %v", skippedEvents)
	}
	if len(warningDiags) != 1 {
		t.Fatalf("expected 1 warning diagnostic, got %d", len(warningDiags))
	}
	if len(devices) != 1 {
		t.Fatalf("expected 1 device from unprivileged technique, got %d", len(devices))
	}
	if devices[0].IP != "192.168.1.10" {
		t.Fatalf("expected device IP 192.168.1.10, got %s", devices[0].IP)
	}
}

func TestRun_EmitsAddressProbedPerSighting(t *testing.T) {
	orig := netInterfaces
	defer func() { netInterfaces = orig }()

	origArp := registry["arp"]
	defer func() {
		if origArp != nil {
			registry["arp"] = origArp
		} else {
			delete(registry, "arp")
		}
	}()

	netInterfaces = func() ([]net.Interface, error) {
		iface := net.Interface{Index: 1, Name: "eth0", Flags: net.FlagUp}
		return []net.Interface{iface}, nil
	}

	RegisterTechnique(&mockTechnique{
		name: "arp",
		sightings: []Sighting{
			{IP: "192.168.1.10", MAC: "aa:bb:cc:dd:ee:ff", Technique: "arp"},
			{IP: "192.168.1.20", MAC: "11:22:33:44:55:66", Technique: "arp"},
		},
	})

	ctx := context.Background()
	events, err := Run(ctx, Options{Subnet: "192.168.1.0/24"})
	if err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}

	var probed []string
	for evt := range events {
		if evt.Kind == EventKindAddressProbed {
			probed = append(probed, evt.Address)
		}
	}

	if len(probed) != 2 {
		t.Fatalf("expected 2 AddressProbed events, got %d", len(probed))
	}
	if probed[0] != "192.168.1.10" {
		t.Fatalf("expected first probed address 192.168.1.10, got %s", probed[0])
	}
	if probed[1] != "192.168.1.20" {
		t.Fatalf("expected second probed address 192.168.1.20, got %s", probed[1])
	}
}

func TestRun_AddressProbedDedupsAcrossTechniques(t *testing.T) {
	orig := netInterfaces
	defer func() { netInterfaces = orig }()

	origArp := registry["arp"]
	origIcmp := registry["icmp"]
	defer func() {
		for name, tech := range map[string]DiscoveryTechnique{"arp": origArp, "icmp": origIcmp} {
			if tech != nil {
				registry[name] = tech
			} else {
				delete(registry, name)
			}
		}
	}()

	netInterfaces = func() ([]net.Interface, error) {
		iface := net.Interface{Index: 1, Name: "eth0", Flags: net.FlagUp}
		return []net.Interface{iface}, nil
	}

	// Both techniques see the same address; it must only count once.
	RegisterTechnique(&mockTechnique{
		name: "arp",
		sightings: []Sighting{
			{IP: "192.168.1.10", MAC: "aa:bb:cc:dd:ee:ff", Technique: "arp"},
		},
	})
	RegisterTechnique(&mockTechnique{
		name: "icmp",
		sightings: []Sighting{
			{IP: "192.168.1.10", Technique: "icmp"},
		},
	})

	ctx := context.Background()
	events, err := Run(ctx, Options{Techniques: []string{"arp", "icmp"}, Subnet: "192.168.1.0/24"})
	if err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}

	probedCount := 0
	for evt := range events {
		if evt.Kind == EventKindAddressProbed {
			probedCount++
		}
	}

	if probedCount != 1 {
		t.Fatalf("expected 1 AddressProbed event for a shared address, got %d", probedCount)
	}
}

func TestRun_TechniqueStartedCarriesTotalAddresses(t *testing.T) {
	orig := netInterfaces
	defer func() { netInterfaces = orig }()

	origArp := registry["arp"]
	origMdns := registry["mdns"]
	defer func() {
		for name, tech := range map[string]DiscoveryTechnique{"arp": origArp, "mdns": origMdns} {
			if tech != nil {
				registry[name] = tech
			} else {
				delete(registry, name)
			}
		}
	}()

	netInterfaces = func() ([]net.Interface, error) {
		iface := net.Interface{Index: 1, Name: "eth0", Flags: net.FlagUp}
		return []net.Interface{iface}, nil
	}

	// mdns has no fixed target set (no AddressEnumerator); arp does.
	RegisterTechnique(&mockTechnique{name: "mdns"})
	RegisterTechnique(&sweepMockTechnique{
		mockTechnique: mockTechnique{name: "arp"},
		addresses:     []string{"192.168.1.10", "192.168.1.11", "192.168.1.12"},
	})

	ctx := context.Background()
	events, err := Run(ctx, Options{Techniques: []string{"mdns", "arp"}, Subnet: "192.168.1.0/24"})
	if err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}

	var totals []int
	for evt := range events {
		if evt.Kind == EventKindTechniqueStarted {
			totals = append(totals, evt.TotalAddresses)
		}
	}

	if len(totals) != 2 {
		t.Fatalf("expected 2 TechniqueStarted events, got %d", len(totals))
	}
	if totals[0] != 0 {
		t.Fatalf("expected mdns's TechniqueStarted to carry TotalAddresses 0 (no enumerator), got %d", totals[0])
	}
	if totals[1] != 3 {
		t.Fatalf("expected arp's TechniqueStarted to carry TotalAddresses 3, got %d", totals[1])
	}
}

func TestRun_EmitsDeviceFoundAfterMerge(t *testing.T) {
	orig := netInterfaces
	defer func() { netInterfaces = orig }()

	origArp := registry["arp"]
	defer func() {
		if origArp != nil {
			registry["arp"] = origArp
		} else {
			delete(registry, "arp")
		}
	}()

	netInterfaces = func() ([]net.Interface, error) {
		iface := net.Interface{Index: 1, Name: "eth0", Flags: net.FlagUp}
		return []net.Interface{iface}, nil
	}

	RegisterTechnique(&mockTechnique{
		name: "arp",
		sightings: []Sighting{
			{IP: "192.168.1.10", MAC: "aa:bb:cc:dd:ee:ff", Technique: "arp"},
		},
	})

	ctx := context.Background()
	events, err := Run(ctx, Options{Subnet: "192.168.1.0/24"})
	if err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}

	var foundDevices []Device
	var doneEvents int
	for evt := range events {
		switch evt.Kind {
		case EventKindDeviceFound:
			foundDevices = append(foundDevices, evt.Device)
		case EventKindDone:
			doneEvents++
		}
	}

	if len(foundDevices) != 1 {
		t.Fatalf("expected 1 DeviceFound event, got %d", len(foundDevices))
	}
	if foundDevices[0].IP != "192.168.1.10" {
		t.Fatalf("expected device IP 192.168.1.10, got %s", foundDevices[0].IP)
	}
	if foundDevices[0].MAC != "aa:bb:cc:dd:ee:ff" {
		t.Fatalf("expected device MAC aa:bb:cc:dd:ee:ff, got %s", foundDevices[0].MAC)
	}
	if doneEvents != 1 {
		t.Fatalf("expected exactly 1 Done event, got %d", doneEvents)
	}
}

func TestRun_EmitsDeviceUpdatedWhenMACResolvesForKnownIP(t *testing.T) {
	orig := netInterfaces
	defer func() { netInterfaces = orig }()

	origArp := registry["arp"]
	origMdns := registry["mdns"]
	defer func() {
		for name, tech := range map[string]DiscoveryTechnique{"arp": origArp, "mdns": origMdns} {
			if tech != nil {
				registry[name] = tech
			} else {
				delete(registry, name)
			}
		}
	}()

	netInterfaces = func() ([]net.Interface, error) {
		iface := net.Interface{Index: 1, Name: "eth0", Flags: net.FlagUp}
		return []net.Interface{iface}, nil
	}

	// mdns sees the IP first with no MAC; arp resolves the MAC afterwards.
	// The device's identity key changes ("ip:..." -> "mac:...") but it's the
	// same physical device, so this must surface as DeviceUpdated, not a
	// second DeviceFound.
	RegisterTechnique(&mockTechnique{
		name: "mdns",
		sightings: []Sighting{
			{IP: "192.168.1.20", Technique: "mdns"},
		},
	})
	RegisterTechnique(&mockTechnique{
		name: "arp",
		sightings: []Sighting{
			{IP: "192.168.1.20", MAC: "aa:bb:cc:dd:ee:ff", Technique: "arp"},
		},
	})

	ctx := context.Background()
	events, err := Run(ctx, Options{Techniques: []string{"mdns", "arp"}, Subnet: "192.168.1.0/24"})
	if err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}

	var found []Device
	var mergeUpdated []Device
	for evt := range events {
		switch evt.Kind {
		case EventKindDeviceFound:
			found = append(found, evt.Device)
		case EventKindDeviceUpdated:
			// Story 2.4's classification stage also fires a DeviceUpdated
			// for this device (Technique "classify", populating DeviceType)
			// once enrichment/classification complete; this test is only
			// about the merge-caused update from the MAC resolving.
			if evt.Technique == "merge" {
				mergeUpdated = append(mergeUpdated, evt.Device)
			}
		}
	}

	if len(found) != 1 {
		t.Fatalf("expected exactly 1 DeviceFound event, got %d: %+v", len(found), found)
	}
	if found[0].IP != "192.168.1.20" || found[0].MAC != "" {
		t.Fatalf("expected initial no-MAC DeviceFound for 192.168.1.20, got %+v", found[0])
	}

	if len(mergeUpdated) != 1 {
		t.Fatalf("expected exactly 1 merge-caused DeviceUpdated event, got %d: %+v", len(mergeUpdated), mergeUpdated)
	}
	if mergeUpdated[0].IP != "192.168.1.20" || mergeUpdated[0].MAC != "aa:bb:cc:dd:ee:ff" {
		t.Fatalf("expected DeviceUpdated to carry the resolved MAC, got %+v", mergeUpdated[0])
	}
}

func TestRun_NoSpuriousDeviceUpdatedFromUnrelatedSighting(t *testing.T) {
	orig := netInterfaces
	defer func() { netInterfaces = orig }()

	origArp := registry["arp"]
	origMdns := registry["mdns"]
	defer func() {
		for name, tech := range map[string]DiscoveryTechnique{"arp": origArp, "mdns": origMdns} {
			if tech != nil {
				registry[name] = tech
			} else {
				delete(registry, name)
			}
		}
	}()

	netInterfaces = func() ([]net.Interface, error) {
		iface := net.Interface{Index: 1, Name: "eth0", Flags: net.FlagUp}
		return []net.Interface{iface}, nil
	}

	// arp resolves the device (with MAC) first; mdns's later, unrelated
	// sighting at a different IP must not cause a spurious DeviceUpdated for
	// the already-known device.
	RegisterTechnique(&mockTechnique{
		name: "arp",
		sightings: []Sighting{
			{IP: "192.168.1.10", MAC: "aa:bb:cc:dd:ee:ff", Technique: "arp"},
		},
	})
	RegisterTechnique(&mockTechnique{
		name: "mdns",
		sightings: []Sighting{
			{IP: "192.168.1.20", Technique: "mdns"},
		},
	})

	ctx := context.Background()
	events, err := Run(ctx, Options{Techniques: []string{"arp", "mdns"}, Subnet: "192.168.1.0/24"})
	if err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}

	var found []Device
	var spuriousUpdated []Device
	for evt := range events {
		switch evt.Kind {
		case EventKindDeviceFound:
			found = append(found, evt.Device)
		case EventKindDeviceUpdated:
			// Story 2.4's classification stage fires an expected
			// DeviceUpdated (Technique "classify") for every device once it
			// populates DeviceType — this test is only about merge not
			// producing a spurious update for an unrelated sighting.
			if evt.Technique != "classify" {
				spuriousUpdated = append(spuriousUpdated, evt.Device)
			}
		}
	}

	if len(found) != 2 {
		t.Fatalf("expected 2 DeviceFound events (one per distinct device), got %d: %+v", len(found), found)
	}
	if len(spuriousUpdated) != 0 {
		t.Fatalf("expected no non-classification DeviceUpdated events, got %d: %+v", len(spuriousUpdated), spuriousUpdated)
	}
}

func TestRun_DeviceFoundPrecedesDone(t *testing.T) {
	orig := netInterfaces
	defer func() { netInterfaces = orig }()

	origArp := registry["arp"]
	defer func() {
		if origArp != nil {
			registry["arp"] = origArp
		} else {
			delete(registry, "arp")
		}
	}()

	netInterfaces = func() ([]net.Interface, error) {
		iface := net.Interface{Index: 1, Name: "eth0", Flags: net.FlagUp}
		return []net.Interface{iface}, nil
	}

	RegisterTechnique(&mockTechnique{
		name: "arp",
		sightings: []Sighting{
			{IP: "192.168.1.10", MAC: "aa:bb:cc:dd:ee:ff", Technique: "arp"},
		},
	})

	ctx := context.Background()
	events, err := Run(ctx, Options{Subnet: "192.168.1.0/24"})
	if err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}

	var lastKindBeforeClose EventKind
	var lastTechniqueBeforeClose string
	var sawDone bool
	for evt := range events {
		if evt.Kind == EventKindDone {
			sawDone = true
		}
		if !sawDone {
			lastKindBeforeClose = evt.Kind
			lastTechniqueBeforeClose = evt.Technique
		}
	}

	if !sawDone {
		t.Fatal("expected a Done event")
	}
	// Story 2.4's classification stage always fires its own DeviceUpdated
	// (Technique "classify", populating DeviceType, since this device has no
	// signal and classifies to DeviceTypeUnknown, still a change from the
	// zero value) strictly after DeviceFound and strictly before Done, so
	// the last device event before Done is now guaranteed to be that
	// classify-caused DeviceUpdated rather than DeviceFound itself.
	if lastKindBeforeClose != EventKindDeviceUpdated || lastTechniqueBeforeClose != "classify" {
		t.Fatalf("expected the classify-caused DeviceUpdated to be last before Done, got kind %s technique %q", lastKindBeforeClose, lastTechniqueBeforeClose)
	}
}

func TestRun_DoneIsLastEventAndChannelCloses(t *testing.T) {
	orig := netInterfaces
	defer func() { netInterfaces = orig }()

	origArp := registry["arp"]
	origMdns := registry["mdns"]
	defer func() {
		for name, tech := range map[string]DiscoveryTechnique{"arp": origArp, "mdns": origMdns} {
			if tech != nil {
				registry[name] = tech
			} else {
				delete(registry, name)
			}
		}
	}()

	netInterfaces = func() ([]net.Interface, error) {
		iface := net.Interface{Index: 1, Name: "eth0", Flags: net.FlagUp}
		return []net.Interface{iface}, nil
	}

	RegisterTechnique(&mockTechnique{
		name: "arp",
		sightings: []Sighting{
			{IP: "192.168.1.10", MAC: "aa:bb:cc:dd:ee:ff", Technique: "arp"},
		},
	})
	RegisterTechnique(&mockTechnique{
		name: "mdns",
		sightings: []Sighting{
			{IP: "192.168.1.20", Technique: "mdns"},
		},
	})

	ctx := context.Background()
	events, err := Run(ctx, Options{Techniques: []string{"arp", "mdns"}, Subnet: "192.168.1.0/24"})
	if err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}

	var kinds []EventKind
	for evt := range events {
		kinds = append(kinds, evt.Kind)
	}

	if len(kinds) == 0 {
		t.Fatal("expected at least one event")
	}

	last := kinds[len(kinds)-1]
	if last != EventKindDone {
		t.Fatalf("expected Done to be the last event, got %s", last)
	}

	deviceFoundAfterDone := false
	sawDone := false
	for _, k := range kinds {
		if k == EventKindDone {
			sawDone = true
			continue
		}
		if sawDone && (k == EventKindDeviceFound || k == EventKindDeviceUpdated) {
			deviceFoundAfterDone = true
		}
	}

	if !sawDone {
		t.Fatal("expected a Done event")
	}
	if deviceFoundAfterDone {
		t.Fatal("no DeviceFound or DeviceUpdated event should follow Done")
	}
}

func TestRun_DoneWaitsForAllTechniqueChannels(t *testing.T) {
	orig := netInterfaces
	defer func() { netInterfaces = orig }()

	origArp := registry["arp"]
	origMdns := registry["mdns"]
	defer func() {
		for name, tech := range map[string]DiscoveryTechnique{"arp": origArp, "mdns": origMdns} {
			if tech != nil {
				registry[name] = tech
			} else {
				delete(registry, name)
			}
		}
	}()

	netInterfaces = func() ([]net.Interface, error) {
		iface := net.Interface{Index: 1, Name: "eth0", Flags: net.FlagUp}
		return []net.Interface{iface}, nil
	}

	RegisterTechnique(&mockTechnique{
		name: "arp",
		sightings: []Sighting{
			{IP: "192.168.1.10", MAC: "aa:bb:cc:dd:ee:ff", Technique: "arp"},
		},
	})

	done := make(chan struct{})
	RegisterTechnique(&delayedCloseTechnique{
		name: "mdns",
		sightings: []Sighting{
			{IP: "192.168.1.20", Technique: "mdns"},
		},
		done: done,
	})

	ctx := context.Background()
	events, err := Run(ctx, Options{Techniques: []string{"arp", "mdns"}, Subnet: "192.168.1.0/24"})
	if err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}

	result := make(chan []EventKind, 1)
	go func() {
		var kinds []EventKind
		for evt := range events {
			kinds = append(kinds, evt.Kind)
		}
		result <- kinds
	}()

	select {
	case <-result:
		t.Fatal("Run completed before delayed technique channel was closed")
	case <-time.After(50 * time.Millisecond):
	}

	close(done)

	select {
	case kinds := <-result:
		last := kinds[len(kinds)-1]
		if last != EventKindDone {
			t.Fatalf("expected Done to be last event, got %s", last)
		}
	case <-time.After(time.Second):
		t.Fatal("Run did not complete after delayed technique channel was closed")
	}
}

func TestRun_SkipNeverCallsRun(t *testing.T) {
	orig := netInterfaces
	defer func() { netInterfaces = orig }()

	origPrivileged := registry["privileged"]
	defer func() {
		if origPrivileged != nil {
			registry["privileged"] = origPrivileged
		} else {
			delete(registry, "privileged")
		}
	}()

	netInterfaces = func() ([]net.Interface, error) {
		iface := net.Interface{Index: 1, Name: "eth0", Flags: net.FlagUp}
		return []net.Interface{iface}, nil
	}

	RegisterTechnique(&panicOnRunTechnique{
		name:              "privileged",
		requiresPrivilege: true,
	})

	ctx := context.Background()
	events, err := Run(ctx, Options{Techniques: []string{"privileged"}})
	if err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}

	var skipped bool
	for evt := range events {
		if evt.Kind == EventKindTechniqueSkipped && evt.Technique == "privileged" {
			skipped = true
		}
	}
	if !skipped {
		t.Fatal("expected TechniqueSkipped event for 'privileged'")
	}
}

func TestRun_BoundsListenBasedTechniqueWithSafetyNetTimeout(t *testing.T) {
	orig := netInterfaces
	defer func() { netInterfaces = orig }()

	origTimeout := safetyNetTimeout
	defer func() { safetyNetTimeout = origTimeout }()
	safetyNetTimeout = 50 * time.Millisecond

	origMdns := registry["mdns"]
	defer func() {
		if origMdns != nil {
			registry["mdns"] = origMdns
		} else {
			delete(registry, "mdns")
		}
	}()

	netInterfaces = func() ([]net.Interface, error) {
		iface := net.Interface{Index: 1, Name: "eth0", Flags: net.FlagUp}
		return []net.Interface{iface}, nil
	}

	// Simulates the real mdns/ssdp bug: a technique that never closes its
	// channel on its own. Without core/engine imposing safetyNetTimeout via
	// ctx, Run would hang here forever waiting for Done.
	RegisterTechnique(&neverEndingTechnique{name: "mdns"})

	ctx := context.Background()
	events, err := Run(ctx, Options{Techniques: []string{"mdns"}, Subnet: "192.168.1.0/24"})
	if err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}

	done := make(chan struct{})
	go func() {
		for range events {
		}
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not complete within a bounded time for a listen-based technique; safetyNetTimeout was not applied")
	}
}

func TestRun_SweepBasedTechniqueNotBoundBySafetyNetTimeout(t *testing.T) {
	orig := netInterfaces
	defer func() { netInterfaces = orig }()

	origTimeout := safetyNetTimeout
	defer func() { safetyNetTimeout = origTimeout }()
	safetyNetTimeout = 10 * time.Millisecond

	origArp := registry["arp"]
	defer func() {
		if origArp != nil {
			registry["arp"] = origArp
		} else {
			delete(registry, "arp")
		}
	}()

	netInterfaces = func() ([]net.Interface, error) {
		iface := net.Interface{Index: 1, Name: "eth0", Flags: net.FlagUp}
		return []net.Interface{iface}, nil
	}

	// delayedCloseTechnique implements AddressEnumerator (sweep-based) and
	// takes longer than safetyNetTimeout to close its channel — it must still
	// complete normally, since safetyNetTimeout only bounds listen-based
	// techniques.
	done := make(chan struct{})
	RegisterTechnique(&delayedCloseTechnique{
		name: "arp",
		sightings: []Sighting{
			{IP: "192.168.1.10", MAC: "aa:bb:cc:dd:ee:ff", Technique: "arp"},
		},
		done: done,
	})

	ctx := context.Background()
	events, err := Run(ctx, Options{Techniques: []string{"arp"}, Subnet: "192.168.1.0/24"})
	if err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}

	time.Sleep(50 * time.Millisecond) // well past safetyNetTimeout
	close(done)

	var sawDone bool
	for evt := range events {
		if evt.Kind == EventKindDone {
			sawDone = true
		}
	}
	if !sawDone {
		t.Fatal("expected scan to complete once the sweep-based technique closed its channel")
	}
}

func TestRun_ReportsErrorDiagnosticWhenNoDevicesDiscovered(t *testing.T) {
	orig := netInterfaces
	defer func() { netInterfaces = orig }()

	origArp := registry["arp"]
	defer func() {
		if origArp != nil {
			registry["arp"] = origArp
		} else {
			delete(registry, "arp")
		}
	}()

	netInterfaces = func() ([]net.Interface, error) {
		iface := net.Interface{Index: 1, Name: "eth0", Flags: net.FlagUp}
		return []net.Interface{iface}, nil
	}

	RegisterTechnique(&mockTechnique{name: "arp"})

	ctx := context.Background()
	events, err := Run(ctx, DefaultOptions())
	if err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}

	foundError := false
	for evt := range events {
		if evt.Kind != EventKindDone {
			continue
		}
		if len(evt.Report.Devices) != 0 {
			t.Fatalf("expected empty devices, got %d", len(evt.Report.Devices))
		}
		for _, d := range evt.Diagnostics {
			if d.Severity == "error" && d.Message == "no devices discovered" {
				foundError = true
				if d.Reason == "" {
					t.Fatal("expected non-empty reason")
				}
			}
		}
	}
	if !foundError {
		t.Fatal("expected a Diagnostic with Severity error for no devices discovered in Done event")
	}
}

func TestRun_NoDevicesDiagnosticSuppressedWhenNoInterfaceAlreadyReported(t *testing.T) {
	orig := netInterfaces
	defer func() { netInterfaces = orig }()

	netInterfaces = func() ([]net.Interface, error) {
		return nil, nil
	}

	ctx := context.Background()
	events, err := Run(ctx, Options{})
	if err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}

	var errorMessages []string
	for evt := range events {
		if evt.Kind != EventKindDone {
			continue
		}
		for _, d := range evt.Diagnostics {
			if d.Severity == "error" {
				errorMessages = append(errorMessages, d.Message)
			}
		}
	}

	foundNoInterface := false
	for _, msg := range errorMessages {
		if msg == "no devices discovered" {
			t.Fatalf("did not expect a redundant 'no devices discovered' error alongside the no-interface error, got: %v", errorMessages)
		}
		if msg == "no active network interface found" {
			foundNoInterface = true
		}
	}
	if !foundNoInterface {
		t.Fatalf("expected the no-interface error, got: %v", errorMessages)
	}
}

func TestRun_NoDevicesDiagnosticAbsentWhenDevicesFound(t *testing.T) {
	orig := netInterfaces
	defer func() { netInterfaces = orig }()

	origArp := registry["arp"]
	defer func() {
		if origArp != nil {
			registry["arp"] = origArp
		} else {
			delete(registry, "arp")
		}
	}()

	netInterfaces = func() ([]net.Interface, error) {
		iface := net.Interface{Index: 1, Name: "eth0", Flags: net.FlagUp}
		return []net.Interface{iface}, nil
	}

	RegisterTechnique(&mockTechnique{
		name: "arp",
		sightings: []Sighting{
			{IP: "192.168.1.10", MAC: "aa:bb:cc:dd:ee:ff", Technique: "arp"},
		},
	})

	ctx := context.Background()
	events, err := Run(ctx, DefaultOptions())
	if err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}

	for evt := range events {
		if evt.Kind != EventKindDone {
			continue
		}
		for _, d := range evt.Diagnostics {
			if d.Message == "no devices discovered" {
				t.Fatal("did not expect 'no devices discovered' diagnostic when devices were found")
			}
		}
	}
}

// deterministicEnricher is a fake Enricher (Story 2.1) with a fixed return
// value, used to test enrichDevices/Run's wiring without depending on real
// DNS resolution or an OUI dataset.
type deterministicEnricher struct {
	name              string
	requiresPrivilege bool
	hostname          string
	vendor            string
	enrichErr         error
}

func (e *deterministicEnricher) Name() string            { return e.name }
func (e *deterministicEnricher) RequiresPrivilege() bool { return e.requiresPrivilege }
func (e *deterministicEnricher) Enrich(ctx context.Context, device Device) (Device, error) {
	if e.enrichErr != nil {
		return device, e.enrichErr
	}
	if e.hostname != "" {
		device.Hostname = e.hostname
	}
	if e.vendor != "" {
		device.Vendor = e.vendor
	}
	return device, nil
}

// privilegeProbeMockEnricher additionally implements PrivilegeProber,
// mirroring privilegeProbeMockTechnique for the enricher path.
type privilegeProbeMockEnricher struct {
	fakeEnricher
	probeErr error
}

func (e *privilegeProbeMockEnricher) ProbePrivilege() (bool, error) {
	return e.requiresPrivilege, e.probeErr
}

func TestEnrichDevices_EnricherValueOverridesDiscoverySourcedValue(t *testing.T) {
	orig := enricherRegistry["fakeenrich"]
	defer func() {
		if orig != nil {
			enricherRegistry["fakeenrich"] = orig
		} else {
			delete(enricherRegistry, "fakeenrich")
		}
	}()

	RegisterEnricher(&deterministicEnricher{name: "fakeenrich", hostname: "resolved.example.com", vendor: "Resolved Vendor"})

	// Simulates a Device whose Hostname/Vendor were already set from a
	// discovery Sighting's ServiceData (e.g. mDNS) before enrichment ran.
	devices := []Device{{IP: "10.0.0.5", MAC: "aa:bb:cc:dd:ee:ff", Hostname: "from-mdns", Vendor: "Old Vendor"}}
	ch := make(chan Event, 10)

	result, diags := enrichDevices(context.Background(), devices, []string{"fakeenrich"}, ch)

	if len(diags) != 0 {
		t.Fatalf("expected no diagnostics, got %v", diags)
	}
	if result[0].Hostname != "resolved.example.com" {
		t.Fatalf("expected enricher's hostname to override discovery-sourced value (AD-10), got %q", result[0].Hostname)
	}
	if result[0].Vendor != "Resolved Vendor" {
		t.Fatalf("expected enricher's vendor to override discovery-sourced value (AD-10), got %q", result[0].Vendor)
	}
}

func TestEnrichDevices_UnknownEnricherNameReportsWarningDiagnostic(t *testing.T) {
	devices := []Device{{IP: "10.0.0.5"}}
	ch := make(chan Event, 10)

	result, diags := enrichDevices(context.Background(), devices, []string{"tcpconnect"}, ch)

	if !reflect.DeepEqual(result[0], devices[0]) {
		t.Fatalf("expected device unchanged for an unregistered enricher name, got %+v", result[0])
	}
	if len(diags) != 1 || diags[0].Severity != "warning" {
		t.Fatalf("expected 1 warning diagnostic for an unregistered enricher name, got %v", diags)
	}
	if diags[0].Message != `enricher "tcpconnect" not found in registry` {
		t.Fatalf("expected diagnostic to name the missing enricher, got %q", diags[0].Message)
	}
	select {
	case evt := <-ch:
		t.Fatalf("expected no event for an unregistered enricher name, got %+v", evt)
	default:
	}
}

func TestEnrichDevices_SkipsEnricherWhenPrivilegeRequired(t *testing.T) {
	orig := enricherRegistry["privileged-enrich"]
	defer func() {
		if orig != nil {
			enricherRegistry["privileged-enrich"] = orig
		} else {
			delete(enricherRegistry, "privileged-enrich")
		}
	}()

	RegisterEnricher(&fakeEnricher{name: "privileged-enrich", requiresPrivilege: true})

	devices := []Device{{IP: "10.0.0.5"}}
	ch := make(chan Event, 10)

	result, diags := enrichDevices(context.Background(), devices, []string{"privileged-enrich"}, ch)

	if !reflect.DeepEqual(result[0], devices[0]) {
		t.Fatalf("expected device unchanged when enricher skipped, got %+v", result[0])
	}
	if len(diags) != 1 || diags[0].Severity != "warning" {
		t.Fatalf("expected 1 warning diagnostic, got %v", diags)
	}

	evt := <-ch
	if evt.Kind != EventKindTechniqueSkipped || evt.Technique != "privileged-enrich" {
		t.Fatalf("expected TechniqueSkipped event for privileged-enrich, got %+v", evt)
	}
	if evt.Reason == "" {
		t.Fatal("expected non-empty reason on the skip event")
	}
}

func TestEnrichDevices_SkipReasonSurfacesUnderlyingProbeError(t *testing.T) {
	orig := enricherRegistry["flaky-enrich"]
	defer func() {
		if orig != nil {
			enricherRegistry["flaky-enrich"] = orig
		} else {
			delete(enricherRegistry, "flaky-enrich")
		}
	}()

	probeErr := errors.New("no oui dataset loaded")
	RegisterEnricher(&privilegeProbeMockEnricher{
		fakeEnricher: fakeEnricher{name: "flaky-enrich", requiresPrivilege: true},
		probeErr:     probeErr,
	})

	devices := []Device{{IP: "10.0.0.5"}}
	ch := make(chan Event, 10)

	_, diags := enrichDevices(context.Background(), devices, []string{"flaky-enrich"}, ch)

	if len(diags) != 1 || !strings.Contains(diags[0].Reason, probeErr.Error()) {
		t.Fatalf("expected diagnostic reason to surface underlying probe error %q, got %v", probeErr.Error(), diags)
	}
}

func TestEnrichDevices_AppliesMultipleEnrichersInOrder(t *testing.T) {
	origDNS := enricherRegistry["fakedns"]
	origOUI := enricherRegistry["fakeoui"]
	defer func() {
		if origDNS != nil {
			enricherRegistry["fakedns"] = origDNS
		} else {
			delete(enricherRegistry, "fakedns")
		}
		if origOUI != nil {
			enricherRegistry["fakeoui"] = origOUI
		} else {
			delete(enricherRegistry, "fakeoui")
		}
	}()

	RegisterEnricher(&deterministicEnricher{name: "fakedns", hostname: "host.local"})
	RegisterEnricher(&deterministicEnricher{name: "fakeoui", vendor: "Acme Corp"})

	devices := []Device{{IP: "10.0.0.5", MAC: "aa:bb:cc:dd:ee:ff"}}
	ch := make(chan Event, 10)

	result, diags := enrichDevices(context.Background(), devices, []string{"fakedns", "fakeoui"}, ch)

	if len(diags) != 0 {
		t.Fatalf("expected no diagnostics, got %v", diags)
	}
	if result[0].Hostname != "host.local" || result[0].Vendor != "Acme Corp" {
		t.Fatalf("expected both enrichers' fields applied, got %+v", result[0])
	}
}

func TestRun_DefaultEnrichersInvokedForEveryDeviceWithNoFlag(t *testing.T) {
	orig := netInterfaces
	defer func() { netInterfaces = orig }()

	origArp := registry["arp"]
	origDNS := enricherRegistry["dns"]
	origOUI := enricherRegistry["oui"]
	defer func() {
		if origArp != nil {
			registry["arp"] = origArp
		} else {
			delete(registry, "arp")
		}
		if origDNS != nil {
			enricherRegistry["dns"] = origDNS
		} else {
			delete(enricherRegistry, "dns")
		}
		if origOUI != nil {
			enricherRegistry["oui"] = origOUI
		} else {
			delete(enricherRegistry, "oui")
		}
	}()

	netInterfaces = func() ([]net.Interface, error) {
		iface := net.Interface{Index: 1, Name: "eth0", Flags: net.FlagUp}
		return []net.Interface{iface}, nil
	}

	RegisterTechnique(&mockTechnique{
		name: "arp",
		sightings: []Sighting{
			{IP: "192.168.1.10", MAC: "aa:bb:cc:dd:ee:ff", Technique: "arp"},
		},
	})
	RegisterEnricher(&deterministicEnricher{name: "dns", hostname: "host.local"})
	RegisterEnricher(&deterministicEnricher{name: "oui", vendor: "Acme Corp"})

	ctx := context.Background()
	// DefaultOptions(), not a custom Options — this is the "no flag required" case (AC1).
	events, err := Run(ctx, DefaultOptions())
	if err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}

	var devices []Device
	for evt := range events {
		if evt.Kind == EventKindDone {
			devices = evt.Report.Devices
		}
	}

	if len(devices) != 1 {
		t.Fatalf("expected 1 device, got %d", len(devices))
	}
	if devices[0].Hostname != "host.local" {
		t.Fatalf("expected default 'dns' enricher to run with no flag required, got Hostname %q", devices[0].Hostname)
	}
	if devices[0].Vendor != "Acme Corp" {
		t.Fatalf("expected default 'oui' enricher to run with no flag required, got Vendor %q", devices[0].Vendor)
	}
}

func TestRun_EmitsDeviceUpdatedAfterEnrichmentBeforeDone(t *testing.T) {
	orig := netInterfaces
	defer func() { netInterfaces = orig }()

	origArp := registry["arp"]
	origEnricher := enricherRegistry["custom-enrich"]
	defer func() {
		if origArp != nil {
			registry["arp"] = origArp
		} else {
			delete(registry, "arp")
		}
		if origEnricher != nil {
			enricherRegistry["custom-enrich"] = origEnricher
		} else {
			delete(enricherRegistry, "custom-enrich")
		}
	}()

	netInterfaces = func() ([]net.Interface, error) {
		iface := net.Interface{Index: 1, Name: "eth0", Flags: net.FlagUp}
		return []net.Interface{iface}, nil
	}

	RegisterTechnique(&mockTechnique{
		name: "arp",
		sightings: []Sighting{
			{IP: "192.168.1.10", MAC: "aa:bb:cc:dd:ee:ff", Technique: "arp"},
		},
	})
	RegisterEnricher(&deterministicEnricher{name: "custom-enrich", hostname: "enriched.local"})

	ctx := context.Background()
	events, err := Run(ctx, Options{
		Techniques:    []string{"arp"},
		EnrichOptions: []string{"custom-enrich"},
		Subnet:        "192.168.1.0/24",
	})
	if err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}

	var kinds []EventKind
	// Story 2.4's classification stage fires its own DeviceUpdated
	// (Technique "classify") right after enrichment's; track both by index
	// to prove the full ordering (enrich, then classify, then Done) rather
	// than just that each occurs somewhere before Done.
	enrichIdx, classifyIdx, doneIdx := -1, -1, -1
	var enrichUpdatedDevice Device
	for evt := range events {
		kinds = append(kinds, evt.Kind)
		idx := len(kinds) - 1
		switch {
		case evt.Kind == EventKindDeviceUpdated && evt.Technique == "enrich":
			enrichIdx = idx
			enrichUpdatedDevice = evt.Device
		case evt.Kind == EventKindDeviceUpdated && evt.Technique == "classify":
			classifyIdx = idx
		case evt.Kind == EventKindDone:
			doneIdx = idx
		}
	}

	if len(kinds) == 0 || kinds[len(kinds)-1] != EventKindDone {
		t.Fatalf("expected Done to be the last event, got %v", kinds)
	}
	if enrichIdx == -1 {
		t.Fatalf("expected a DeviceUpdated event with Technique 'enrich', got kinds %v", kinds)
	}
	if enrichUpdatedDevice.Hostname != "enriched.local" {
		t.Fatalf("expected DeviceUpdated to carry the enriched hostname, got %+v", enrichUpdatedDevice)
	}
	if classifyIdx == -1 {
		t.Fatalf("expected a DeviceUpdated event with Technique 'classify', got kinds %v", kinds)
	}
	if !(enrichIdx < classifyIdx && classifyIdx < doneIdx) {
		t.Fatalf("expected event order enrich(%d) < classify(%d) < Done(%d)", enrichIdx, classifyIdx, doneIdx)
	}
}

func TestEnrichDevices_PerDeviceErrorDiagnosticIncludesDeviceIdentity(t *testing.T) {
	orig := enricherRegistry["failing-enrich"]
	defer func() {
		if orig != nil {
			enricherRegistry["failing-enrich"] = orig
		} else {
			delete(enricherRegistry, "failing-enrich")
		}
	}()

	enrichErr := errors.New("lookup timed out")
	RegisterEnricher(&deterministicEnricher{name: "failing-enrich", enrichErr: enrichErr})

	devices := []Device{{IP: "10.0.0.5", MAC: "aa:bb:cc:dd:ee:ff"}}
	ch := make(chan Event, 10)

	result, diags := enrichDevices(context.Background(), devices, []string{"failing-enrich"}, ch)

	if !reflect.DeepEqual(result[0], devices[0]) {
		t.Fatalf("expected device unchanged when Enrich errors, got %+v", result[0])
	}
	if len(diags) != 1 {
		t.Fatalf("expected 1 diagnostic, got %v", diags)
	}
	if !strings.Contains(diags[0].Message, "mac:aa:bb:cc:dd:ee:ff") {
		t.Fatalf("expected the failure diagnostic to identify the affected device, got %q", diags[0].Message)
	}
	if diags[0].Reason != enrichErr.Error() {
		t.Fatalf("expected diagnostic reason to surface the underlying error, got %q", diags[0].Reason)
	}
}

func TestEnrichDevices_EmitsTechniqueStartedForRunningEnricher(t *testing.T) {
	orig := enricherRegistry["progress-enrich"]
	defer func() {
		if orig != nil {
			enricherRegistry["progress-enrich"] = orig
		} else {
			delete(enricherRegistry, "progress-enrich")
		}
	}()

	RegisterEnricher(&fakeEnricher{name: "progress-enrich"})

	devices := []Device{{IP: "10.0.0.5"}}
	ch := make(chan Event, 10)

	enrichDevices(context.Background(), devices, []string{"progress-enrich"}, ch)

	evt := <-ch
	if evt.Kind != EventKindTechniqueStarted || evt.Technique != "progress-enrich" {
		t.Fatalf("expected a TechniqueStarted event for the running enricher, got %+v", evt)
	}
}

// blockingEnricher simulates a slow/unresponsive enricher (e.g. a DNS
// lookup against an unreachable resolver): Enrich blocks until ctx is done.
type blockingEnricher struct {
	name string
}

func (e *blockingEnricher) Name() string            { return e.name }
func (e *blockingEnricher) RequiresPrivilege() bool { return false }
func (e *blockingEnricher) Enrich(ctx context.Context, device Device) (Device, error) {
	<-ctx.Done()
	return device, ctx.Err()
}

func TestRun_BoundsEnrichmentWithEnrichTimeout(t *testing.T) {
	orig := netInterfaces
	defer func() { netInterfaces = orig }()

	origTimeout := enrichTimeout
	defer func() { enrichTimeout = origTimeout }()
	enrichTimeout = 50 * time.Millisecond

	origArp := registry["arp"]
	origEnricher := enricherRegistry["slow-enrich"]
	defer func() {
		if origArp != nil {
			registry["arp"] = origArp
		} else {
			delete(registry, "arp")
		}
		if origEnricher != nil {
			enricherRegistry["slow-enrich"] = origEnricher
		} else {
			delete(enricherRegistry, "slow-enrich")
		}
	}()

	netInterfaces = func() ([]net.Interface, error) {
		iface := net.Interface{Index: 1, Name: "eth0", Flags: net.FlagUp}
		return []net.Interface{iface}, nil
	}

	RegisterTechnique(&mockTechnique{
		name: "arp",
		sightings: []Sighting{
			{IP: "192.168.1.10", MAC: "aa:bb:cc:dd:ee:ff", Technique: "arp"},
		},
	})
	// blockingEnricher never returns on its own; without enrichTimeout bounding
	// the ctx passed into enrichDevices, Run would hang here forever.
	RegisterEnricher(&blockingEnricher{name: "slow-enrich"})

	ctx := context.Background()
	events, err := Run(ctx, Options{
		Techniques:    []string{"arp"},
		EnrichOptions: []string{"slow-enrich"},
		Subnet:        "192.168.1.0/24",
	})
	if err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}

	done := make(chan struct{})
	go func() {
		for range events {
		}
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not complete within a bounded time; enrichTimeout was not applied to a stalled enricher")
	}
}

// ipVendorEnricher assigns a Vendor keyed by the Device's IP, simulating
// enrich/oui (Story 2.1) resolving a different vendor per device, so
// TestRun_ClassifiesEachDeviceExactlyOnceAfterEnrichmentBeforeDone can prove
// classification sees each device's own enriched signal rather than a
// shared fixed value.
type ipVendorEnricher struct {
	name    string
	vendors map[string]string
}

func (e *ipVendorEnricher) Name() string            { return e.name }
func (e *ipVendorEnricher) RequiresPrivilege() bool { return false }
func (e *ipVendorEnricher) Enrich(ctx context.Context, d Device) (Device, error) {
	if v, ok := e.vendors[d.IP]; ok {
		d.Vendor = v
	}
	return d, nil
}

func TestRun_ClassifiesEachDeviceExactlyOnceAfterEnrichmentBeforeDone(t *testing.T) {
	orig := netInterfaces
	defer func() { netInterfaces = orig }()

	origArp := registry["arp"]
	origEnricher := enricherRegistry["vendor-by-ip"]
	defer func() {
		if origArp != nil {
			registry["arp"] = origArp
		} else {
			delete(registry, "arp")
		}
		if origEnricher != nil {
			enricherRegistry["vendor-by-ip"] = origEnricher
		} else {
			delete(enricherRegistry, "vendor-by-ip")
		}
	}()

	netInterfaces = func() ([]net.Interface, error) {
		iface := net.Interface{Index: 1, Name: "eth0", Flags: net.FlagUp}
		return []net.Interface{iface}, nil
	}

	RegisterTechnique(&mockTechnique{
		name: "arp",
		sightings: []Sighting{
			{IP: "192.168.1.10", MAC: "aa:bb:cc:dd:ee:01", Technique: "arp"},
			{IP: "192.168.1.11", MAC: "aa:bb:cc:dd:ee:02", Technique: "arp"},
		},
	})
	// If classification ran before enrichment (violating AD-9's ordering),
	// neither device would have a Vendor yet and both would classify as
	// "unknown" regardless of this enricher.
	RegisterEnricher(&ipVendorEnricher{
		name: "vendor-by-ip",
		vendors: map[string]string{
			"192.168.1.10": "Dell Inc.",
			"192.168.1.11": "Apple, Inc.",
		},
	})

	ctx := context.Background()
	events, err := Run(ctx, Options{
		Techniques:    []string{"arp"},
		EnrichOptions: []string{"vendor-by-ip"},
		Subnet:        "192.168.1.0/24",
	})
	if err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}

	classifyUpdatesByIP := map[string]int{}
	var devices []Device
	for evt := range events {
		if evt.Kind == EventKindDeviceUpdated && evt.Technique == "classify" {
			classifyUpdatesByIP[evt.Device.IP]++
		}
		if evt.Kind == EventKindDone {
			devices = evt.Report.Devices
		}
	}

	// Exactly one classify-caused DeviceUpdated per device confirms
	// Classify ran exactly once per Device (not zero, not more than once).
	if classifyUpdatesByIP["192.168.1.10"] != 1 || classifyUpdatesByIP["192.168.1.11"] != 1 {
		t.Fatalf("expected exactly 1 classify-caused DeviceUpdated per device, got %v", classifyUpdatesByIP)
	}

	byIP := map[string]Device{}
	for _, d := range devices {
		byIP[d.IP] = d
	}
	if byIP["192.168.1.10"].DeviceType != DeviceTypeComputer {
		t.Fatalf("expected classification to see enrichment's vendor (Dell -> computer), got %+v", byIP["192.168.1.10"])
	}
	if byIP["192.168.1.11"].DeviceType != DeviceTypePhone {
		t.Fatalf("expected classification to see enrichment's vendor (Apple -> phone), got %+v", byIP["192.168.1.11"])
	}
}

// deviceTypeSettingEnricher simulates an enrich/* adapter that (incorrectly,
// in violation of AD-9) tries to set DeviceType itself. A discovery/*
// adapter has no equivalent way to attempt this at all: Sighting has no
// DeviceType field, only the post-merge Device does.
type deviceTypeSettingEnricher struct {
	name string
}

func (e *deviceTypeSettingEnricher) Name() string            { return e.name }
func (e *deviceTypeSettingEnricher) RequiresPrivilege() bool { return false }
func (e *deviceTypeSettingEnricher) Enrich(ctx context.Context, d Device) (Device, error) {
	d.DeviceType = "bogus-adapter-set-type"
	return d, nil
}

func TestRun_CoreClassificationOverridesAnyAdapterSetDeviceType(t *testing.T) {
	orig := netInterfaces
	defer func() { netInterfaces = orig }()

	origArp := registry["arp"]
	origEnricher := enricherRegistry["rogue-enrich"]
	defer func() {
		if origArp != nil {
			registry["arp"] = origArp
		} else {
			delete(registry, "arp")
		}
		if origEnricher != nil {
			enricherRegistry["rogue-enrich"] = origEnricher
		} else {
			delete(enricherRegistry, "rogue-enrich")
		}
	}()

	netInterfaces = func() ([]net.Interface, error) {
		iface := net.Interface{Index: 1, Name: "eth0", Flags: net.FlagUp}
		return []net.Interface{iface}, nil
	}

	RegisterTechnique(&mockTechnique{
		name: "arp",
		sightings: []Sighting{
			// No MAC vendor/port/service-data signal, so real classification
			// should land on "unknown" -- letting the bogus value through
			// undetected would look identical to a real classification, so
			// this device is deliberately signal-free to make the overwrite
			// unambiguous.
			{IP: "192.168.1.10", MAC: "aa:bb:cc:dd:ee:ff", Technique: "arp"},
		},
	})
	RegisterEnricher(&deviceTypeSettingEnricher{name: "rogue-enrich"})

	ctx := context.Background()
	events, err := Run(ctx, Options{
		Techniques:    []string{"arp"},
		EnrichOptions: []string{"rogue-enrich"},
		Subnet:        "192.168.1.0/24",
	})
	if err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}

	var devices []Device
	for evt := range events {
		if evt.Kind == EventKindDone {
			devices = evt.Report.Devices
		}
	}

	if len(devices) != 1 {
		t.Fatalf("expected 1 device, got %d", len(devices))
	}
	if devices[0].DeviceType != DeviceTypeUnknown {
		t.Fatalf("expected core classification to overwrite the adapter-set DeviceType with %q, got %q", DeviceTypeUnknown, devices[0].DeviceType)
	}
}
