package engine

import "strings"

// v1 taxonomy Device Types (FR-7). DeviceTypeUnknown is what Classify
// returns when the combined signal set is insufficient to pick a more
// specific type — there is no accuracy bar for v1.
const (
	DeviceTypeRouter   = "router"
	DeviceTypePhone    = "phone"
	DeviceTypePrinter  = "printer"
	DeviceTypeIoT      = "iot"
	DeviceTypeSmartTV  = "smart-tv"
	DeviceTypeComputer = "computer"
	DeviceTypeUnknown  = "unknown"
)

// printerPorts/castingPorts are open-TCP-port signatures specific enough on
// their own to decide a DeviceType regardless of any other signal: 631
// (IPP) and 9100 (JetDirect/raw printing) for printers, 554 (RTSP
// streaming) and 8009 (Chromecast control) for smart TVs/casting receivers.
var printerPorts = map[int]bool{631: true, 9100: true}
var castingPorts = map[int]bool{554: true, 8009: true}

type classifyRule struct {
	deviceType string
	keywords   []string
}

// serviceDataRules maps a DeviceType to substrings looked for
// (case-insensitively) across a Device's merged mDNS/SSDP ServiceData
// values (Story 1.3's service-type/name/info/usn/server fields, carried
// through merge by Story 2.4). Checked in this fixed order — the first
// entry with a match wins — so priority doesn't depend on Go's random map
// iteration order.
var serviceDataRules = []classifyRule{
	{DeviceTypeRouter, []string{"internetgatewaydevice", "wanconnectiondevice"}},
	{DeviceTypePrinter, []string{"_ipp", "_pdl-datastream", "_printer"}},
	{DeviceTypeSmartTV, []string{"_googlecast", "dial-multiscreen", "mediarenderer", "roku:ecp"}},
	{DeviceTypeIoT, []string{"_hap", "_hue", "smartthings"}},
}

// bannerRules maps a DeviceType to substrings looked for
// (case-insensitively) in a Device's open ports' Banner text (enrich/banner,
// Story 2.3 — a raw first-read from an already-open TCP port, populated only
// for protocols that speak first, e.g. SSH/FTP/Telnet). Checked in this
// fixed order for the same reason as serviceDataRules.
var bannerRules = []classifyRule{
	{DeviceTypeRouter, []string{"dropbear", "busybox", "romsshell", "mikrotik", "routeros"}},
	{DeviceTypePrinter, []string{"jetdirect", "hp laserjet", "cups", "lexmark", "brother nc"}},
	{DeviceTypeIoT, []string{"esp8266", "esp32", "shelly", "tasmota"}},
	{DeviceTypeSmartTV, []string{"roku", "chromecast", "airplay"}},
}

// vendorRules maps a DeviceType to substrings looked for
// (case-insensitively) in the Vendor field (enrich/oui's MAC OUI lookup,
// Story 2.1) once no port, banner, or service-data signal has already
// decided a type. Checked in this fixed order for the same reason as
// serviceDataRules.
var vendorRules = []classifyRule{
	{DeviceTypeRouter, []string{"cisco", "netgear", "tp-link", "tplink", "d-link", "linksys", "mikrotik", "ubiquiti", "asustek", "belkin", "zyxel"}},
	{DeviceTypePrinter, []string{"hewlett packard", "canon", "epson", "brother", "lexmark", "xerox", "kyocera", "ricoh"}},
	{DeviceTypeSmartTV, []string{"lg electronics", "sony", "vizio", "roku", "amazon technologies"}},
	{DeviceTypePhone, []string{"apple", "samsung electronics", "xiaomi", "oneplus", "huawei", "oppo", "vivo", "google"}},
	{DeviceTypeIoT, []string{"sonos", "philips", "espressif", "nest labs", "ring llc"}},
	{DeviceTypeComputer, []string{"dell", "lenovo", "intel corporate", "microsoft corp", "gigabyte", "msi", "acer", "toshiba", "raspberry pi"}},
}

// Classify assigns a best-effort DeviceType (FR-7) to a fully-merged,
// fully-enriched Device by combining MAC vendor, open ports/banners, and
// mDNS/SSDP service data. It is a pure function with no side effects,
// called exactly once per Device by core/engine.Run after merge and
// enrichment have both completed (AD-9) — never inside a discovery/* or
// enrich/* adapter, so it always sees the complete signal set regardless of
// which techniques/enrichers ran, and always overwrites any value an
// adapter may have (incorrectly) set on DeviceType, since it never reads
// the Device's existing DeviceType field.
//
// Signals are checked from most to least specific: an open printing/casting
// port decides on its own, then Banner keywords, then ServiceData keywords,
// then Vendor keywords. DeviceTypeUnknown is returned when nothing matches.
func Classify(d Device) string {
	if hasOpenTCPPort(d, printerPorts) {
		return DeviceTypePrinter
	}
	if hasOpenTCPPort(d, castingPorts) {
		return DeviceTypeSmartTV
	}
	if t := matchBanners(d.OpenPorts, bannerRules); t != "" {
		return t
	}
	if t := matchServiceData(d.ServiceData, serviceDataRules); t != "" {
		return t
	}
	if t := matchVendor(d.Vendor, vendorRules); t != "" {
		return t
	}
	return DeviceTypeUnknown
}

func hasOpenTCPPort(d Device, ports map[int]bool) bool {
	for _, p := range d.OpenPorts {
		if p.State == "open" && p.Protocol == "tcp" && ports[p.Port] {
			return true
		}
	}
	return false
}

func matchBanners(openPorts []OpenPort, rules []classifyRule) string {
	for _, rule := range rules {
		for _, p := range openPorts {
			if p.Banner == "" {
				continue
			}
			lower := strings.ToLower(p.Banner)
			for _, kw := range rule.keywords {
				if strings.Contains(lower, kw) {
					return rule.deviceType
				}
			}
		}
	}
	return ""
}

func matchServiceData(data map[string]string, rules []classifyRule) string {
	for _, rule := range rules {
		for _, v := range data {
			lower := strings.ToLower(v)
			for _, kw := range rule.keywords {
				if strings.Contains(lower, kw) {
					return rule.deviceType
				}
			}
		}
	}
	return ""
}

func matchVendor(vendor string, rules []classifyRule) string {
	if vendor == "" {
		return ""
	}
	lower := strings.ToLower(vendor)
	for _, rule := range rules {
		for _, kw := range rule.keywords {
			if strings.Contains(lower, kw) {
				return rule.deviceType
			}
		}
	}
	return ""
}
