---
type: object
cluster: ble
universe: live
status: verified
entity: core/ble/ports.go
---

# BLEScanner / Writer (BLE) + registry

The BLE vertical's plugin interfaces. Unlike the LAN vertical's fan-out of
many discovery techniques, exactly one `BLEScanner` is registered per
platform build (`var scanner BLEScanner`, not a map) — `discovery/blescan`
picks the platform's real implementation via build tags
(`blescan_linux.go`/`blescan_windows.go`).

## Why this shape

`Scan`'s contract is pinned to disallow a nil-channel-with-nil-error return:
without that rule, `core/ble.Run` can't distinguish "the scan was refused"
from "nothing was nearby yet", and it would range over a nil channel
forever. `Probe`'s `reason` on `ok=false` must be non-empty by contract —
enforced softly by `core/ble.skipDiagnostic`'s `unknownSkipReason` fallback,
not by a compile-time check, because `BLEScanner` implementations are a
closed set inside this repo but the interface itself can't enforce a
non-empty string.

## Shape

- `BLEScanner`: `Probe() (ok bool, reason string)`, `Scan(ctx, window) (<-chan Advertisement, error)`
- `Writer`: `Name()`, `Write(Report) ([]byte, error)` — same shape as `engine.Writer`, independent type
- registry: single-slot `RegisterScanner`/`GetScanner` (not a map — mirrors a future second scanner, e.g. the deferred Android bridge mentioned at `core/ble/ble.go:46`, being a ghost, not live code), plus `RegisterWriter`/`GetWriter`/`WriterNames`

Citations: `core/ble/ports.go:15` (`BLEScanner`), `:42` (`Writer`), `core/ble/registry.go:9` (`scanner` var), `:13` (`writerRegistry`)

## Connected to

- **owns:** nothing — pure contract
- **owned-by:** nothing — the seam
- **joins:** `objects/discovery/blescan.md`, `objects/report/ble-writers.md`, consumed by `core/ble.Run` ([../../processes/ble-scan.md](../../processes/ble-scan.md))
- **looks-like-but-is-not:** `core/engine.DiscoveryTechnique`/`Writer` ([../engine/ports.md](../engine/ports.md))

## If you change this

- **Hits:** `discovery/blescan` (the sole `BLEScanner` implementation), every BLE `Writer`, `core/ble.Run`'s `GetScanner`/`Probe`/`Scan` call sequence
- **Does not hit:** `core/engine` (independent interfaces)

## Surfaces

| Surface | Role |
|---|---|
| `discovery/blescan` | implements `BLEScanner` |
| `report/ble/*` | implement `Writer` |
| `core/ble.Run` | reads via registry lookups only |
| `cmd/cli/ble.go` | triggers registration via blank imports |

## See

- Source: `core/ble/ports.go`, `core/ble/registry.go`
