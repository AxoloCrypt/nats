---
type: object
cluster: internal
universe: live
status: verified
entity: internal/strutil/strutil.go
---

# strutil / subnetutil / rawcapture / render / blerender

Five small `internal/` packages, each scoped to exactly the parent tree that
can import it (Go's `internal/` visibility rule) — that's why `rawcapture`
and `subnetutil` are two separate packages instead of one shared one:
`discovery/internal` isn't importable from `enrich/*`.

## Why this shape

Each package holds logic genuinely duplicated in intent but not in code
across its scope's packages: `subnetutil` (interface/address enumeration,
used only within `discovery/*`), `rawcapture` (pcap-handle wrapping, used
only within `enrich/tcpsyn`+`enrich/udpscan` — a deliberate second copy of
the same pattern `discovery/arp` also implements inline, not a shared
import of it), `render` (LAN report formatting helpers, `report/*` only),
`blerender` (BLE report placeholder/sanitization rules, `report/ble/*`
only). `strutil` is the one exception — it's imported from both verticals
(`core/ble/ble.go` and elsewhere) because it holds no vertical-specific
logic, just `IsBlank`.

## Shape

- `internal/strutil`: `IsBlank(string) bool`
- `discovery/internal/subnetutil`: `EnumerateTargets`, `ResolveInterface`, `FindLocalIP`
- `enrich/internal/rawcapture`: `pcapAdapter` (Close/WritePacketData/ReadPacketData), `ResolveInterfaceForIP`, `ProbeCapture`
- `report/internal/render`: `FormatPorts`, `SanitizeLine` (collapses newlines **and tabs** — table writers delimit columns with `\t`)
- `report/ble/internal/blerender`: `Fields` (placeholder-safe field extraction), `field`

Citations: `internal/strutil/strutil.go:16`; `discovery/internal/subnetutil/subnetutil.go:13,40,78`; `enrich/internal/rawcapture/rawcapture.go:68,103`; `report/internal/render/render.go:15,37`; `report/ble/internal/blerender/blerender.go:44`

## Connected to

- **owns:** nothing — pure helpers
- **owned-by:** nothing — leaf utility layer
- **joins:** `discovery/arp`/`icmp`/`mdns`/`ssdp` (subnetutil), `enrich/tcpsyn`/`udpscan` (rawcapture), `report/table`/`markdown`/`plain` (render), `report/ble/table`/`markdown`/`plain` (blerender), `core/ble.Run` (strutil)
- **looks-like-but-is-not:** `discovery/arp`'s own inline pcap-adapter code — structurally identical to `rawcapture`'s, deliberately not the same package

## If you change this

- **Hits:** `SanitizeLine` — every LAN human-readable writer's column safety; `rawcapture.ProbeCapture` — both `tcpsyn` and `udpscan`'s privilege-check accuracy; `subnetutil.EnumerateTargets` — both `arp` and `icmp`'s sweep set
- **Does not hit:** anything outside each package's own scope tree (enforced by Go's `internal/` visibility, not just convention)

## Surfaces

| Surface | Role |
|---|---|
| scoped adapters listed above | read/write via direct import |

## See

- Source: `internal/strutil/strutil.go`, `discovery/internal/subnetutil/subnetutil.go`, `enrich/internal/rawcapture/rawcapture.go`, `report/internal/render/render.go`, `report/ble/internal/blerender/blerender.go`
