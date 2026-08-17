---
type: object
cluster: report
universe: live
status: verified
entity: report/ble/table/table.go
---

# BLE writers (table / json / markdown / plain)

The four `ble.Writer` implementations, mirroring the LAN writer set exactly
in shape but registered into `core/ble`'s own, separate registry
(`ble.RegisterWriter`).

## Why this shape

`report/ble/internal/blerender` holds the shared placeholder/sanitization
rules for the three human-readable writers (`table`/`markdown`/`plain`);
`report/ble/json` deliberately skips it, because JSON's contract is
"every key present, no `omitempty`, ever" ([../ble/device.md](../ble/device.md)),
which is a guarantee about key presence, not about substituting a
human-readable placeholder string the way the other three do.

## Shape

- `report/ble/table`, `report/ble/json`, `report/ble/markdown` (+ `escapeCell`), `report/ble/plain` — each `Write(ble.Report) ([]byte, error)`
- `table`/`markdown`/`plain` call `blerender.Fields` for placeholder-safe field extraction

Citations: `report/ble/table/table.go:16` (`init`/register), `:30` (`Write`); `report/ble/json/json.go:12`,`:25`; `report/ble/markdown/markdown.go:16`,`:29`,`:56` (`escapeCell`); `report/ble/plain/plain.go:15`,`:28`

## Connected to

- **owns:** nothing — read-only rendering
- **owned-by:** `core/ble`'s writer registry ([../ble/ports.md](../ble/ports.md))
- **joins:** `report/ble/internal/blerender` ([../internal/shared-plumbing.md](../internal/shared-plumbing.md)), `BLEDeviceProfile`/`Report` ([../ble/device.md](../ble/device.md), [../ble/event-model.md](../ble/event-model.md))
- **looks-like-but-is-not:** `report/*` (LAN writers, [lan-writers.md](lan-writers.md)) — same four-format pattern, unrelated registry and Report type

## If you change this

- **Hits:** nothing upstream — only caller is `cmd/cli/ble.go`'s `ble.GetWriter(format)` lookup + `Write` call
- **Does not hit:** `core/ble.Run`, `discovery/blescan`, `report/*` (LAN writers)

## Surfaces

| Surface | Role |
|---|---|
| `cmd/cli/ble.go` | looks up by `--format` (via `resolveBLEFormat`), calls `Write` once at the end of `runBLEScan` |

## See

- Source: `report/ble/table/table.go`, `report/ble/json/json.go`, `report/ble/markdown/markdown.go`, `report/ble/plain/plain.go`
