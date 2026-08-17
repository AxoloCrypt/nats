---
type: object
cluster: cli
universe: live
status: verified
entity: cmd/cli/ble.go
---

# ble command wiring (ble.go)

`nats ble`'s cobra command — structurally mirrors `root.go`'s `scan` wiring
but drives `core/ble.Run` and blank-imports only `report/ble/*` (never the
LAN `discovery/*`/`enrich/*`/`report/*` set, which lives in `root.go`).

## Why this shape

Reuses `root.go`'s shared swappable output vars (`progressWriter`,
`diagnosticWriter`, `reportWriter`) rather than defining its own — both
commands write to the same process stdout/stderr, so one set of vars is the
single point tests substitute for either command.

## Shape

- `buildBLEOptions(cmd) ble.Options` — flags → `Options` (just `--window`)
- `resolveBLEFormat(cmd) string` — `--format` → writer name
- `renderBLEDone` — stderr summary
- `renderBLEDiagnostic(w, d) bool` — the one sanctioned reader of `ble.Diagnostic` fields; converts into an `engine.Diagnostic` and hands it to `renderDiagnostic` ([diagnostic-rendering.md](diagnostic-rendering.md))
- `runBLEScan(reportW, events, writer, outputFile) bool` — drains the event channel

Citations: `cmd/cli/ble.go:13-16` (blank imports), `:29` (`buildBLEOptions`), `:52` (`resolveBLEFormat`), `:74` (`renderBLEDone`), `:97` (`renderBLEDiagnostic`), `:123` (`runBLEScan`), `:243` (`init`)

## Connected to

- **owns:** nothing — driving adapter over `core/ble`'s contract
- **owned-by:** `cmd/cli/main.go` (`Execute()`, via `rootCmd.AddCommand`)
- **joins:** `core/ble.Run` ([../../processes/ble-scan.md](../../processes/ble-scan.md)), `objects/cli/diagnostic-rendering.md`, `objects/report/ble-writers.md` (reachable only via this file's blank imports)
- **looks-like-but-is-not:** `cmd/cli/root.go` ([root-cmd.md](root-cmd.md)) — parallel shape, separate vertical

## If you change this

- **Hits:** which BLE writers are reachable (`--format`), `renderBLEDiagnostic`'s conversion contract (must stay the only reader of `ble.Diagnostic` fields — enforced by `diagnostic_enforcement_test.go`)
- **Does not hit:** `core/engine`/`cmd/cli/root.go` (independent command, independent vertical — `core/ble` never imports `core/engine`)

## Surfaces

| Surface | Role |
|---|---|
| a user running `nats ble` | triggers this path |
| `core/ble.Run` | driven only from this file |

## See

- Source: `cmd/cli/ble.go`
