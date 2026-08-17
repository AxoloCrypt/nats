---
type: object
cluster: ble
universe: live
status: verified
entity: core/ble/types.go
---

# BLEDeviceProfile / Advertisement

The BLE vertical's device shapes. `Advertisement` is the raw per-packet shape
a `BLEScanner` emits; `BLEDeviceProfile` is the compiled, de-duplicated
row `core/ble.Run` builds by merging repeat advertisements from the same
`Address`.

## Why this shape

Every `BLEDeviceProfile` field renders as an explicit placeholder rather than
being dropped — deliberately the opposite of `engine.Device`'s `omitempty`
tags. A real peripheral re-broadcasts every ~20ms-1.28s, so `core/ble.Run`
merges repeats by `Address` in-memory (never package-level state); once a
field resolves away from its placeholder (`""` for `Name`, `"unknown"` for
`Vendor`/`DeviceType`), a later less-informative packet must never blank it
back — `DistanceEstimate` is the one exception, unconditionally overwritten
every packet since "how close right now" only means anything as the latest
reading.

## Shape

- `Advertisement`: `Address`, `Name`, `RSSI`, `TXPower *int`, `Appearance`, `ServiceUUIDs`, `ManufacturerData`, `CompanyID *uint16` — every field beyond `Address`/`RSSI` optional, zero-valued when the platform doesn't expose it
- `BLEDeviceProfile`: `Address`, `Name`, `Vendor`, `DeviceType`, `DistanceEstimate` — no `omitempty` on any JSON tag

Citations: `core/ble/types.go:55` (`Advertisement`), `core/ble/types.go:92` (`BLEDeviceProfile`), merge/placeholder logic at `core/ble/ble.go:114-179`

## Connected to

- **owns:** nothing further down
- **owned-by:** `Report.Devices` ([event-model.md](event-model.md))
- **joins:** `DeriveVendor`/`ClassifyDeviceType`/`EstimateDistance` (see `core/ble/vendor.go`, `core/ble/classify.go`, `core/ble/distance.go` — not separately carded, referenced from `processes/ble-scan.md`)
- **looks-like-but-is-not:** `core/engine.DeviceProfile` ([../engine/device.md](../engine/device.md)) — parallel shape, independent type, opposite `omitempty` convention

## If you change this

- **Hits:** `core/ble.Run`'s merge loop (`core/ble/ble.go`), every BLE `Writer` (`objects/report/ble-writers.md`), `report/ble/internal/blerender` (placeholder rendering assumes every field is populated, never absent)
- **Does not hit:** `core/engine` (independent type, no import — `core/ble/import_test.go` enforces this)

## Surfaces

| Surface | Role |
|---|---|
| `discovery/blescan` | produces `Advertisement` |
| `core/ble.Run` | reads `Advertisement`, writes `BLEDeviceProfile` |
| `report/ble/*` | reads `BLEDeviceProfile` only |

## See

- Source: `core/ble/types.go`
