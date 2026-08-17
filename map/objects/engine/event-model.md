---
type: object
cluster: engine
universe: live
status: verified
entity: core/engine/types.go
---

# Options / Event / Report / Diagnostic / Sighting

The LAN vertical's progress-streaming and result shapes: what `cmd/cli`
configures a scan with (`Options`), what it receives live (`Event`), and what
it's left holding at the end (`Report`).

## Why this shape

`Event` carries a full `Report` only on `Kind: Done` (`Event.Report` is
otherwise zero-valued) — every other event kind is a progress notification,
not a partial result, so a driving adapter never has to reconcile two
different "current state" representations mid-scan. `Sighting` is
deliberately pre-merge (raw `MAC`/`IP`/`Technique`/`ServiceData` from one
technique) — it's `Merge`'s only input, never a `Writer`'s.

## Shape

- `Options`: `Techniques`, `EnrichOptions`, `OutputFormat`, `OutputFile`, `Subnet`
- `EventKind`: `TechniqueStarted`, `AddressProbed`, `DeviceFound`, `DeviceUpdated`, `TechniqueSkipped`, `Done`
- `Event`: `Kind`, `Technique`, `Address`, `Reason`, `Device`, `TotalAddresses`, `Diagnostics`, `Report`
- `Diagnostic`: `Severity`, `Message`, `Reason` — see `objects/cli/diagnostic-rendering.md` for the one place these fields may be read
- `Sighting`: `MAC`, `IP`, `Technique`, `ServiceData`
- `Report`: `Devices []Device`, `Diagnostics []Diagnostic`

Citations: `core/engine/types.go:3` (`Options`), `:11` (`EventKind`), `:22` (`Event`), `:37` (`Diagnostic`), `:43` (`Sighting`), `:89` (`Report`)

## Connected to

- **owns:** `Device` ([device.md](device.md)) inside `Report.Devices` and `Event.Device`
- **owned-by:** `core/engine.Run` ([../../processes/lan-scan.md](../../processes/lan-scan.md)), which is the only producer of `Event`s
- **joins:** `cmd/cli/root.go`'s `runScan` ([../cli/root-cmd.md](../cli/root-cmd.md)), the only consumer of the `Event` channel
- **looks-like-but-is-not:** `core/ble.Event`/`Report`/`Diagnostic` ([../ble/event-model.md](../ble/event-model.md)) — parallel shapes, independently defined, `Diagnostic.Reason` is `omitempty` here but not in the BLE vertical

## If you change this

- **Hits:** `core/engine.Run` (all emission sites), `cmd/cli/root.go`'s `runScan`/`renderProgress`/`renderDone`, every LAN `Writer` (reads `Report`)
- **Does not hit:** `core/ble` (independent type)

## Surfaces

| Surface | Role |
|---|---|
| `core/engine.Run` | writes (only producer) |
| `cmd/cli/root.go` | reads (only vertical-appropriate consumer of the channel) |
| `report/*` | reads `Report` only, never `Event` |

## See

- Source: `core/engine/types.go`
