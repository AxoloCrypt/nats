package engine

import (
	"testing"
)

func TestMerge_MACMatchProducesSingleDevice(t *testing.T) {
	sightings := []Sighting{
		{MAC: "aa:bb:cc:dd:ee:ff", IP: "10.0.0.1", Technique: "arp"},
		{MAC: "aa:bb:cc:dd:ee:ff", IP: "10.0.0.1", Technique: "mdns"},
	}

	devices, diags := Merge(sightings)

	if len(diags) != 0 {
		t.Fatalf("expected 0 diagnostics, got %d", len(diags))
	}
	if len(devices) != 1 {
		t.Fatalf("expected 1 device, got %d", len(devices))
	}
	if devices[0].MAC != "aa:bb:cc:dd:ee:ff" {
		t.Fatalf("expected MAC aa:bb:cc:dd:ee:ff, got %s", devices[0].MAC)
	}
	if devices[0].IP != "10.0.0.1" {
		t.Fatalf("expected IP 10.0.0.1, got %s", devices[0].IP)
	}
}

func TestMerge_IPMatchNoMACProducesSingleDevice(t *testing.T) {
	sightings := []Sighting{
		{IP: "10.0.0.1", Technique: "mdns"},
		{IP: "10.0.0.1", Technique: "ssdp"},
	}

	devices, diags := Merge(sightings)

	if len(diags) != 0 {
		t.Fatalf("expected 0 diagnostics, got %d", len(diags))
	}
	if len(devices) != 1 {
		t.Fatalf("expected 1 device, got %d", len(devices))
	}
	if devices[0].IP != "10.0.0.1" {
		t.Fatalf("expected IP 10.0.0.1, got %s", devices[0].IP)
	}
}

func TestMerge_IPMatchOneWithMACPreservesMAC(t *testing.T) {
	sightings := []Sighting{
		{MAC: "aa:bb:cc:dd:ee:ff", IP: "10.0.0.1", Technique: "arp"},
		{IP: "10.0.0.1", Technique: "mdns"},
	}

	devices, diags := Merge(sightings)

	if len(diags) != 0 {
		t.Fatalf("expected 0 diagnostics, got %d", len(diags))
	}
	if len(devices) != 1 {
		t.Fatalf("expected 1 device, got %d", len(devices))
	}
	if devices[0].MAC != "aa:bb:cc:dd:ee:ff" {
		t.Fatalf("expected preserved MAC aa:bb:cc:dd:ee:ff, got %s", devices[0].MAC)
	}
	if devices[0].IP != "10.0.0.1" {
		t.Fatalf("expected IP 10.0.0.1, got %s", devices[0].IP)
	}
}

func TestMerge_ConflictingMACForSameIPNotMerged(t *testing.T) {
	sightings := []Sighting{
		{MAC: "aa:bb:cc:dd:ee:ff", IP: "10.0.0.1", Technique: "arp"},
		{MAC: "11:22:33:44:55:66", IP: "10.0.0.1", Technique: "mdns"},
	}

	devices, diags := Merge(sightings)

	if len(diags) != 0 {
		t.Fatalf("expected 0 diagnostics, got %d", len(diags))
	}
	if len(devices) != 2 {
		t.Fatalf("expected 2 devices (not merged), got %d", len(devices))
	}

	macs := make(map[string]bool)
	for _, d := range devices {
		macs[d.MAC] = true
	}
	if !macs["aa:bb:cc:dd:ee:ff"] {
		t.Fatal("expected device with MAC aa:bb:cc:dd:ee:ff")
	}
	if !macs["11:22:33:44:55:66"] {
		t.Fatal("expected device with MAC 11:22:33:44:55:66")
	}
}

func TestMerge_EmptyIPDroppedWithDiagnostic(t *testing.T) {
	sightings := []Sighting{
		{IP: "", Technique: "bad"},
		{MAC: "aa:bb:cc:dd:ee:ff", IP: "10.0.0.1", Technique: "arp"},
	}

	devices, diags := Merge(sightings)

	if len(diags) != 1 {
		t.Fatalf("expected 1 diagnostic, got %d", len(diags))
	}
	if diags[0].Severity != "error" {
		t.Fatalf("expected severity error, got %s", diags[0].Severity)
	}
	if diags[0].Message != "sighting missing required IP" {
		t.Fatalf("expected 'sighting missing required IP', got %s", diags[0].Message)
	}
	if len(devices) != 1 {
		t.Fatalf("expected 1 device (the valid one), got %d", len(devices))
	}
	if devices[0].IP != "10.0.0.1" {
		t.Fatalf("expected valid device with IP 10.0.0.1, got %s", devices[0].IP)
	}
}

func TestMerge_AllEmptyIPDropped(t *testing.T) {
	sightings := []Sighting{
		{IP: "", Technique: "bad1"},
		{IP: "", Technique: "bad2"},
	}

	devices, diags := Merge(sightings)

	if len(diags) != 2 {
		t.Fatalf("expected 2 diagnostics, got %d", len(diags))
	}
	if len(devices) != 0 {
		t.Fatalf("expected 0 devices, got %d", len(devices))
	}
}

func TestMerge_EmptySightings(t *testing.T) {
	devices, diags := Merge(nil)

	if len(diags) != 0 {
		t.Fatalf("expected 0 diagnostics for nil input, got %d", len(diags))
	}
	if len(devices) != 0 {
		t.Fatalf("expected 0 devices for nil input, got %d", len(devices))
	}

	devices, diags = Merge([]Sighting{})

	if len(diags) != 0 {
		t.Fatalf("expected 0 diagnostics for empty input, got %d", len(diags))
	}
	if len(devices) != 0 {
		t.Fatalf("expected 0 devices for empty input, got %d", len(devices))
	}
}

func TestMerge_MultipleDistinctDevicesByMAC(t *testing.T) {
	sightings := []Sighting{
		{MAC: "aa:bb:cc:dd:ee:ff", IP: "10.0.0.1", Technique: "arp"},
		{MAC: "11:22:33:44:55:66", IP: "10.0.0.2", Technique: "arp"},
		{MAC: "aa:bb:cc:dd:ee:ff", IP: "10.0.0.1", Technique: "mdns"},
		{MAC: "11:22:33:44:55:66", IP: "10.0.0.2", Technique: "icmp"},
	}

	devices, diags := Merge(sightings)

	if len(diags) != 0 {
		t.Fatalf("expected 0 diagnostics, got %d", len(diags))
	}
	if len(devices) != 2 {
		t.Fatalf("expected 2 devices, got %d", len(devices))
	}
}

func TestMerge_IPMatchWithNoMACAssignedWhenIPConflicted(t *testing.T) {
	sightings := []Sighting{
		{MAC: "aa:bb:cc:dd:ee:ff", IP: "10.0.0.1", Technique: "arp"},
		{MAC: "11:22:33:44:55:66", IP: "10.0.0.1", Technique: "mdns"},
		{IP: "10.0.0.1", Technique: "ssdp"},
	}

	devices, diags := Merge(sightings)

	if len(diags) != 0 {
		t.Fatalf("expected 0 diagnostics, got %d", len(diags))
	}
	if len(devices) != 3 {
		t.Fatalf("expected 3 devices (two with conflicting MACs + no-MAC sighting as separate), got %d", len(devices))
	}
}

func TestMerge_SameMACDifferentIPsStillMerged(t *testing.T) {
	sightings := []Sighting{
		{MAC: "aa:bb:cc:dd:ee:ff", IP: "10.0.0.1", Technique: "arp"},
		{MAC: "aa:bb:cc:dd:ee:ff", IP: "10.0.0.2", Technique: "mdns"},
	}

	devices, diags := Merge(sightings)

	if len(diags) != 0 {
		t.Fatalf("expected 0 diagnostics, got %d", len(diags))
	}
	if len(devices) != 1 {
		t.Fatalf("expected 1 device (same MAC always merges), got %d", len(devices))
	}
	if devices[0].MAC != "aa:bb:cc:dd:ee:ff" {
		t.Fatalf("expected MAC aa:bb:cc:dd:ee:ff, got %s", devices[0].MAC)
	}
}

func TestMerge_ConflictingMACDoesNotMergeUnrelatedNoMACSightings(t *testing.T) {
	sightings := []Sighting{
		{MAC: "aa:bb:cc:dd:ee:ff", IP: "10.0.0.1", Technique: "arp"},
		{MAC: "11:22:33:44:55:66", IP: "10.0.0.1", Technique: "arp-stale"},
		{IP: "10.0.0.1", Technique: "mdns"},
		{IP: "10.0.0.1", Technique: "ssdp"},
	}

	devices, diags := Merge(sightings)

	if len(diags) != 0 {
		t.Fatalf("expected 0 diagnostics, got %d", len(diags))
	}
	if len(devices) != 4 {
		t.Fatalf("expected 4 devices (2 conflicting MAC + 2 unmerged no-MAC), got %d", len(devices))
	}

	noMACCount := 0
	for _, d := range devices {
		if d.MAC == "" {
			noMACCount++
		}
	}
	if noMACCount != 2 {
		t.Fatalf("expected the two no-MAC sightings to remain separate Devices, got %d no-MAC devices", noMACCount)
	}
}

func TestMerge_SameMACAtSecondIPStillClaimsNoMACSighting(t *testing.T) {
	sightings := []Sighting{
		{MAC: "aa:bb:cc:dd:ee:ff", IP: "10.0.0.1", Technique: "arp"},
		{MAC: "aa:bb:cc:dd:ee:ff", IP: "10.0.0.2", Technique: "arp"},
		{IP: "10.0.0.2", Technique: "mdns"},
	}

	devices, diags := Merge(sightings)

	if len(diags) != 0 {
		t.Fatalf("expected 0 diagnostics, got %d", len(diags))
	}
	if len(devices) != 1 {
		t.Fatalf("expected 1 device (no-MAC sighting at the MAC's second IP should merge in, not spawn a duplicate), got %d", len(devices))
	}
	if devices[0].MAC != "aa:bb:cc:dd:ee:ff" {
		t.Fatalf("expected MAC aa:bb:cc:dd:ee:ff, got %s", devices[0].MAC)
	}
}

// An enricher can only be shown to override a discovery-sourced
// Hostname/Vendor if Merge actually produced one. These confirm Merge copies
// ServiceData into the Device it produces, through the real Sighting ->
// Merge -> Device path, not just a hand-built Device literal.

func TestMerge_CopiesHostnameFromServiceDataMACMatch(t *testing.T) {
	sightings := []Sighting{
		{MAC: "aa:bb:cc:dd:ee:ff", IP: "10.0.0.1", Technique: "arp"},
		{MAC: "aa:bb:cc:dd:ee:ff", IP: "10.0.0.1", Technique: "mdns", ServiceData: map[string]string{"hostname": "host.lan"}},
	}

	devices, _ := Merge(sightings)

	if len(devices) != 1 {
		t.Fatalf("expected 1 device, got %d", len(devices))
	}
	if devices[0].Hostname != "host.lan" {
		t.Fatalf("expected Hostname 'host.lan' copied from ServiceData, got %q", devices[0].Hostname)
	}
}

func TestMerge_CopiesHostnameFromServiceDataIPMatchNoMAC(t *testing.T) {
	sightings := []Sighting{
		{IP: "10.0.0.1", Technique: "mdns", ServiceData: map[string]string{"hostname": "host.lan"}},
	}

	devices, _ := Merge(sightings)

	if len(devices) != 1 {
		t.Fatalf("expected 1 device, got %d", len(devices))
	}
	if devices[0].Hostname != "host.lan" {
		t.Fatalf("expected Hostname 'host.lan' copied from ServiceData, got %q", devices[0].Hostname)
	}
}

func TestMerge_ServiceDataDoesNotOverwriteAlreadySetField(t *testing.T) {
	sightings := []Sighting{
		{MAC: "aa:bb:cc:dd:ee:ff", IP: "10.0.0.1", Technique: "mdns", ServiceData: map[string]string{"hostname": "first.lan"}},
		{MAC: "aa:bb:cc:dd:ee:ff", IP: "10.0.0.1", Technique: "ssdp", ServiceData: map[string]string{"hostname": "second.lan"}},
	}

	devices, _ := Merge(sightings)

	if len(devices) != 1 {
		t.Fatalf("expected 1 device, got %d", len(devices))
	}
	if devices[0].Hostname != "first.lan" {
		t.Fatalf("expected the first non-empty ServiceData hostname to win, got %q", devices[0].Hostname)
	}
}

func TestMerge_NoServiceDataLeavesHostnameAndVendorEmpty(t *testing.T) {
	sightings := []Sighting{
		{MAC: "aa:bb:cc:dd:ee:ff", IP: "10.0.0.1", Technique: "arp"},
	}

	devices, _ := Merge(sightings)

	if len(devices) != 1 {
		t.Fatalf("expected 1 device, got %d", len(devices))
	}
	if devices[0].Hostname != "" || devices[0].Vendor != "" {
		t.Fatalf("expected empty Hostname/Vendor with no ServiceData, got %+v", devices[0])
	}
}

// Classify is the first consumer of a Device's ServiceData — identity
// merging alone doesn't require preserving it. These confirm Merge carries
// it through onto the Device, key by key, the same
// first-non-empty-wins policy as Hostname/Vendor.

func TestMerge_CarriesServiceDataThroughOntoDevice(t *testing.T) {
	sightings := []Sighting{
		{MAC: "aa:bb:cc:dd:ee:ff", IP: "10.0.0.1", Technique: "ssdp", ServiceData: map[string]string{"type": "urn:schemas-upnp-org:device:InternetGatewayDevice:1"}},
	}

	devices, _ := Merge(sightings)

	if len(devices) != 1 {
		t.Fatalf("expected 1 device, got %d", len(devices))
	}
	if got := devices[0].ServiceData["type"]; got != "urn:schemas-upnp-org:device:InternetGatewayDevice:1" {
		t.Fatalf("expected ServiceData[\"type\"] carried onto Device, got %q", got)
	}
}

func TestMerge_ServiceDataMergesDifferentlyShapedKeysAcrossTechniques(t *testing.T) {
	sightings := []Sighting{
		{MAC: "aa:bb:cc:dd:ee:ff", IP: "10.0.0.1", Technique: "mdns", ServiceData: map[string]string{"name": "printer._ipp._tcp.local."}},
		{MAC: "aa:bb:cc:dd:ee:ff", IP: "10.0.0.1", Technique: "ssdp", ServiceData: map[string]string{"usn": "uuid:1234::upnp:rootdevice"}},
	}

	devices, _ := Merge(sightings)

	if len(devices) != 1 {
		t.Fatalf("expected 1 device, got %d", len(devices))
	}
	if devices[0].ServiceData["name"] != "printer._ipp._tcp.local." {
		t.Fatalf("expected mdns's \"name\" key carried onto Device, got %+v", devices[0].ServiceData)
	}
	if devices[0].ServiceData["usn"] != "uuid:1234::upnp:rootdevice" {
		t.Fatalf("expected ssdp's \"usn\" key carried onto Device, got %+v", devices[0].ServiceData)
	}
}

func TestMerge_ServiceDataDoesNotOverwriteAlreadySetKey(t *testing.T) {
	sightings := []Sighting{
		{MAC: "aa:bb:cc:dd:ee:ff", IP: "10.0.0.1", Technique: "mdns", ServiceData: map[string]string{"info": "first"}},
		{MAC: "aa:bb:cc:dd:ee:ff", IP: "10.0.0.1", Technique: "mdns", ServiceData: map[string]string{"info": "second"}},
	}

	devices, _ := Merge(sightings)

	if len(devices) != 1 {
		t.Fatalf("expected 1 device, got %d", len(devices))
	}
	if devices[0].ServiceData["info"] != "first" {
		t.Fatalf("expected the first non-empty ServiceData value to win, got %q", devices[0].ServiceData["info"])
	}
}
