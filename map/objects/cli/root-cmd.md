---
type: object
cluster: cli
universe: live
status: verified
entity: cmd/cli/root.go
---

# scan command wiring (root.go)

`nats scan`'s cobra command, flag→`engine.Options` translation, and the loop
that drives `engine.Run`'s event channel into stderr progress + stdout
report.

## Why this shape

`root.go`'s import block blank-imports every `discovery/*`/`enrich/*`/
`report/*` package (never `report/ble/*` — that's `ble.go`'s job) purely for
`init()` side effects, so the registries in `core/engine` are populated
before any command runs. `engineRun`/`writeReportFile`/`osExit`/
`progressWriter`/`diagnosticWriter`/`reportWriter` are swappable package-
level vars specifically so tests can substitute fakes — the repo-wide
testability convention (`../../CLAUDE.md`).

## Shape

- `rootCmd` — the cobra root; `scanCmd` — the `scan` subcommand
- `buildOptions(cmd) engine.Options` — flags → `Options`; `--enrich` appends to (never replaces) the default enricher set
- `runScan(w, events, writer, outputFile) bool` — drains the event channel, returns whether an error-severity `Diagnostic` was seen (drives `osExit(1)`)
- `renderProgress`/`renderDone` — stderr progress line
- `splitTechniques` — comma-split helper shared by `--techniques`/`--enrich` parsing

Citations: `cmd/cli/root.go:14-28` (blank imports), `:33` (`rootCmd`), `:43` (`buildOptions`), `:74` (`renderProgress`), `:106` (`renderDone`), `:116-121` (swappable vars), `:162` (`runScan`), `:234` (`scanCmd`), `:295` (`splitTechniques`), `:331` (`Execute`)

## Connected to

- **owns:** nothing — a driving adapter over `core/engine`'s contract
- **owned-by:** `cmd/cli/main.go` (`Execute()`)
- **joins:** `core/engine.Run` ([../../processes/lan-scan.md](../../processes/lan-scan.md)), `objects/cli/diagnostic-rendering.md` (via `renderDiagnostic`, called from inside `runScan`), every card in `objects/discovery/`, `objects/enrich/`, `objects/report/lan-writers.md` (reachable only via this file's blank imports)
- **looks-like-but-is-not:** `cmd/cli/ble.go` ([ble-cmd.md](ble-cmd.md)) — same shape (cobra command + drain loop), separate vertical, separate command

## If you change this

- **Hits:** which discovery techniques/enrichers/writers are reachable at all (removing a blank import un-registers it from `--techniques`/`--enrich`/`--format`), `runScan`'s exit-code contract (`TestScanCommand_NoDevicesDiscovered_ExitsNonzero` and friends in `root_test.go`)
- **Does not hit:** `core/ble`/`cmd/cli/ble.go` (independent command, independent vertical)

## Surfaces

| Surface | Role |
|---|---|
| a user running `nats scan` | triggers this path |
| `core/engine.Run` | driven by this file, never imported elsewhere in `cmd/cli` except here |

## See

- Source: `cmd/cli/root.go`
