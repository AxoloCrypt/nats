---
type: object
cluster: report
universe: live
status: verified
entity: report/table/table.go
---

# LAN writers (table / json / markdown / plain)

The four `engine.Writer` implementations for the LAN vertical. `table` is
the default output format (`DefaultOptions().OutputFormat`,
`core/engine/engine.go:18`).

## Why this shape

Each is a zero-field struct type named `Writer`, registered under its own
package name (`"table"`, `"json"`, `"markdown"`, `"plain"`) — a `Writer`
consumes only the final `Report`, never engine internals, so adding a fifth
format never touches `core/engine`. `table`/`markdown`/`plain` share
formatting/sanitization helpers via `report/internal/render`
([../internal/shared-plumbing.md](../internal/shared-plumbing.md));
`json` deliberately does not, since JSON has its own escaping and no
`\t`-delimited-column risk.

## Shape

- `report/table`: `Write` — tab-delimited columns, uses `render.SanitizeLine` to collapse newlines/tabs in untrusted values
- `report/json`: `Write` — `encoding/json` marshal of `Report`
- `report/markdown`: `Write` + `escapeCell` — markdown table, escapes `|`/newlines per cell
- `report/plain`: `Write` — one line per device, no table structure

Citations: `report/table/table.go:15` (`init`/register), `:28` (`Write`); `report/json/json.go:12`,`:25`; `report/markdown/markdown.go:16`,`:29`,`:54` (`escapeCell`); `report/plain/plain.go:15`,`:28`

## Connected to

- **owns:** nothing — pure read-only rendering
- **owned-by:** `core/engine`'s writer registry ([../engine/ports.md](../engine/ports.md))
- **joins:** `report/internal/render` (table/markdown/plain), `Device`/`Report` ([../engine/device.md](../engine/device.md), [../engine/event-model.md](../engine/event-model.md))
- **looks-like-but-is-not:** `report/ble/*` ([ble-writers.md](ble-writers.md)) — same four-format pattern, renders `ble.Report` instead, registered into `core/ble`'s separate registry

## If you change this

- **Hits:** nothing upstream — a `Writer`'s only caller is `cmd/cli/root.go`'s `GetWriter(opts.OutputFormat)` lookup and the final `Write(report)` call
- **Does not hit:** `core/engine.Run`, any discovery/enrich package, `report/ble/*`

## Surfaces

| Surface | Role |
|---|---|
| `cmd/cli/root.go` | looks up by `--format` name, calls `Write` once at the end of `runScan` |

## See

- Source: `report/table/table.go`, `report/json/json.go`, `report/markdown/markdown.go`, `report/plain/plain.go`
