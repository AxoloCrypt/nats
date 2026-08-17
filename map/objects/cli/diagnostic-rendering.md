---
type: object
cluster: cli
universe: live
status: verified
entity: cmd/cli/root.go
---

# Diagnostic-rendering choke point

The single sanctioned path for printing any `Diagnostic`, from either
vertical. `renderDiagnostic` (root.go) is the only function anywhere in the
repo permitted to read `engine.Diagnostic`'s `Severity`/`Message`/`Reason`
fields; `renderBLEDiagnostic` (ble.go) is the only permitted reader of
`ble.Diagnostic`'s fields, and its sole job is converting into an
`engine.Diagnostic` for `renderDiagnostic` to actually format — a
conversion, not a second renderer.

## Why this shape

`core/ble.Diagnostic` must exist as its own type (the BLE vertical can't
import `core/engine`), which would otherwise invite a second, drifting
implementation of diagnostic formatting for the BLE vertical. Funneling both
through one real renderer keeps severity strings, the "notice" fallback for
an empty `Severity`, and the reason-line format identical across `nats scan`
and `nats ble` output.

## Shape

- `renderDiagnostic(w io.Writer, d engine.Diagnostic) bool` — prints `severity: message` + optional `  reason: ...` line, returns whether severity was `"error"`
- `renderBLEDiagnostic(w io.Writer, d ble.Diagnostic) bool` — builds an `engine.Diagnostic{Severity: d.Severity, Message: d.Message, Reason: d.Reason}` and calls `renderDiagnostic`

Citations: `cmd/cli/root.go:133` (`renderDiagnostic`), `cmd/cli/ble.go:97` (`renderBLEDiagnostic`)

## Connected to

- **owns:** nothing
- **owned-by:** called from within `runScan` ([root-cmd.md](root-cmd.md)) and `runBLEScan` ([ble-cmd.md](ble-cmd.md))
- **joins:** `objects/engine/event-model.md` (`engine.Diagnostic`), `objects/ble/event-model.md` (`ble.Diagnostic`)
- **looks-like-but-is-not:** nothing — this is deliberately the only path, by design and by test

## If you change this

- **Hits:** `cmd/cli/diagnostic_enforcement_test.go`, which uses `go/packages` type info (not name matching) across `cmd/cli`, `core/engine`, and `core/ble` to fail the build if any *other* function reads those fields — any new diagnostic-printing code anywhere in the repo must route through here or the build breaks
- **Does not hit:** `report/*`/`report/ble/*` writers (they render `Report.Diagnostics` for machine-readable formats like JSON directly by serializing the struct, which is not "reading" in the sense this enforcement targets — see the test itself for the exact boundary before relying on this distinction)

## Surfaces

| Surface | Role |
|---|---|
| `runScan`, `runBLEScan` | call these, never read `Diagnostic` fields themselves |
| `cmd/cli/diagnostic_enforcement_test.go` | statically enforces the invariant |

## See

- Source: `cmd/cli/root.go:133`, `cmd/cli/ble.go:82-121`, `cmd/cli/diagnostic_enforcement_test.go`
