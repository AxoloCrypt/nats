# effects/ — change-impact catalog

"If you're changing X, open these cards first." First-order pointers only —
each card's own "If you change this" has the actual Hits/Does not hit
reasoning; this file just routes there. If this index and a card disagree,
the card is right — fix this file.

| If you're changing... | Open these first |
|---|---|
| `Device`/`DeviceProfile`/`OpenPort` shape | [../objects/engine/device.md](../objects/engine/device.md) → every card in `objects/enrich/`, `objects/report/lan-writers.md` |
| `BLEDeviceProfile`/`Advertisement` shape | [../objects/ble/device.md](../objects/ble/device.md) → `objects/discovery/blescan.md`, `objects/report/ble-writers.md` |
| the three plugin interfaces (`DiscoveryTechnique`/`Enricher`/`Writer`) | [../objects/engine/ports.md](../objects/engine/ports.md) → every implementation card in `objects/discovery/`, `objects/enrich/`, `objects/report/lan-writers.md` |
| `BLEScanner`/BLE `Writer` interfaces | [../objects/ble/ports.md](../objects/ble/ports.md) → `objects/discovery/blescan.md`, `objects/report/ble-writers.md` |
| how a scan is orchestrated (merge/enrich/classify order, event emission) | [../processes/lan-scan.md](../processes/lan-scan.md) |
| how a BLE scan is orchestrated | [../processes/ble-scan.md](../processes/ble-scan.md) |
| adding a new discovery technique | [../objects/engine/ports.md](../objects/engine/ports.md) + an existing card in `objects/discovery/` as a pattern + `../../CLAUDE.md`'s "Conventions" section (privilege-probe + swappable-var + init-registration rules) |
| adding a new enricher | same as above, using `objects/enrich/` as the pattern |
| adding a new report format (either vertical) | [../objects/report/lan-writers.md](../objects/report/lan-writers.md) or [../objects/report/ble-writers.md](../objects/report/ble-writers.md) as the pattern |
| any `Diagnostic` printing/formatting | [../objects/cli/diagnostic-rendering.md](../objects/cli/diagnostic-rendering.md) — **do not** add a second reader of `Severity`/`Message`/`Reason`; `cmd/cli/diagnostic_enforcement_test.go` will fail the build |
| `cmd/cli` flag wiring for `scan` | [../objects/cli/root-cmd.md](../objects/cli/root-cmd.md) |
| `cmd/cli` flag wiring for `ble` | [../objects/cli/ble-cmd.md](../objects/cli/ble-cmd.md) |
| the release/build matrix or version string | [../processes/release.md](../processes/release.md), [../objects/cli/version-cmd.md](../objects/cli/version-cmd.md) |
| anything under `internal/` | [../objects/internal/shared-plumbing.md](../objects/internal/shared-plumbing.md) — check which scope tree can even import it before assuming a caller |

## What never crosses

Two boundaries hold no matter what you're changing — see `../CLAUDE.md`'s
"Name collisions" section for the enforcement mechanism:

- `core/engine` ⟷ `core/ble`: never import each other.
- Any `Diagnostic`/`Report`/`Device` internals: `report/*` and `report/ble/*`
  depend only on the final struct shape, never on discovery/enrich internals
  or on how a scan was driven.
