---
type: object
cluster: engine
universe: live
status: verified
entity: core/engine/types.go
---

# Device

The merged, enriched, classified record for one physical network device.
Product word "device" and the type name diverge slightly: the type is
`DeviceProfile`; `Device` is a type alias for it (`= DeviceProfile`), kept so
older call sites reading `engine.Device` and the JSON-tagged struct itself
are the same identifier.

## Why this shape

`OpenPorts` has exactly one legal mutation path — `Upsert` — because
enrichers run in a fixed order over the same device and must be able to
replace an already-recorded port (e.g. `tcpconnect` records port 80 open,
`banner` later replaces that entry with a banner-carrying copy) without ever
producing a duplicate `(Port, Protocol)` row. Direct slice appends anywhere
else would silently double-count a port across enrichers.

## Shape

- `DeviceProfile` (aliased as `Device`): `IP`, `MAC`, `Hostname`, `Vendor`,
  `DeviceType`, `OpenPorts []OpenPort`, `ServiceData map[string]string`
- `OpenPort`: `Port`, `Protocol`, `State`, `Banner`
- `(*DeviceProfile).Upsert(OpenPort)` — replace-in-place by `(Port, Protocol)`, append otherwise

Citations: `core/engine/types.go:73` (alias), `core/engine/types.go:50` (`OpenPort`), `core/engine/types.go:57` (`DeviceProfile`), `core/engine/types.go:79` (`Upsert`)

## Connected to

- **owns:** `OpenPort` rows (via `Upsert`)
- **owned-by:** `Report.Devices` ([event-model.md](event-model.md))
- **joins:** every `Enricher.Enrich` call and `Classify` ([ports.md](ports.md), `processes/lan-scan.md`)
- **looks-like-but-is-not:** `core/ble.BLEDeviceProfile` ([../ble/device.md](../ble/device.md)) — same shape family, independent type, no shared import

## If you change this

- **Hits:** every `Enricher` implementation (`objects/enrich/*`), `Classify` (`core/engine/classify.go`), every LAN `Writer` (`objects/report/lan-writers.md`), `Merge`/`recordDeviceEvent` (`processes/lan-scan.md`)
- **Does not hit:** `core/ble` (independent type, no import relationship — see `core/engine/import_test.go`)

## Surfaces

| Surface | Role |
|---|---|
| `discovery/*` | produces `Sighting`, not `Device` — never writes this directly |
| `core/engine.Merge` | writes (constructs `Device` from `Sighting`s) |
| `enrich/*` | reads previous state, writes via return value + `Upsert` |
| `core/engine.Classify` | writes `DeviceType` only |
| `report/*` | reads only |

## See

- Source: `core/engine/types.go`
