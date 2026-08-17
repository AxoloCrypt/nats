---
type: object
cluster: ble
universe: live
status: verified
entity: core/ble/types.go
---

# Options / Event / Report / Diagnostic (BLE)

The BLE vertical's own progress-streaming and result shapes — structurally
mirrors `core/engine`'s ([../engine/event-model.md](../engine/event-model.md))
but is an independently-defined type family; `core/ble` must never import
`core/engine` (`core/ble/import_test.go`).

## Why this shape

Two deliberate divergences from the LAN vertical, both documented at
`core/ble/ble.go:45-68`: (1) `Diagnostic.Reason` has no `omitempty` tag here
— the BLE vertical's Report always carries a present (if empty) key so a
JSON consumer can distinguish "successful nothing nearby" from "scan was
skipped"; (2) a BLE scan that completes with zero advertisements emits no
Diagnostic at all (LAN's equivalent emits an error-severity "no devices
discovered"), because empty air is an ordinary BLE outcome, not a failure.

## Shape

- `Options`: `Window time.Duration`, default 5s (`DefaultOptions()`)
- `EventKind`: `DeviceFound`, `Done` — no `TechniqueStarted`/`AddressProbed`/`TechniqueSkipped`/`DeviceUpdated` (single scanner per platform, no sweep/technique fan-out)
- `Event`: `Kind`, `Device`, `Diagnostics`, `Report`
- `Diagnostic`: `Severity`, `Message`, `Reason` (all tagged, none `omitempty`)
- `Report`: `Devices []BLEDeviceProfile`, `Diagnostics []Diagnostic` (both tagged, no `omitempty`)

Citations: `core/ble/types.go:6` (`Options`), `:14` (`DefaultOptions`), `:18` (`EventKind`), `:32` (`Event`), `:46` (`Diagnostic`), `:78` (`Report`)

## Connected to

- **owns:** `BLEDeviceProfile` ([device.md](device.md)) inside `Report.Devices`
- **owned-by:** `core/ble.Run` ([../../processes/ble-scan.md](../../processes/ble-scan.md)), the only producer
- **joins:** `cmd/cli/ble.go`'s `runBLEScan` ([../cli/ble-cmd.md](../cli/ble-cmd.md)), the only consumer
- **looks-like-but-is-not:** `core/engine.Event`/`Report`/`Diagnostic` ([../engine/event-model.md](../engine/event-model.md))

## If you change this

- **Hits:** `core/ble.Run` (all emission sites), `cmd/cli/ble.go`'s `runBLEScan`/`renderBLEDone`/`renderBLEDiagnostic`, every BLE `Writer`
- **Does not hit:** `core/engine` (independent type, no import path exists)

## Surfaces

| Surface | Role |
|---|---|
| `core/ble.Run` | writes (only producer) |
| `cmd/cli/ble.go` | reads via `renderBLEDiagnostic`, the sanctioned converter into `engine.Diagnostic` — see `objects/cli/diagnostic-rendering.md` |
| `report/ble/*` | reads `Report` only |

## See

- Source: `core/ble/types.go`
