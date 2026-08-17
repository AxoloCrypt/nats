---
name: schema
description: Closed set of node types and frontmatter fields used across map/
---

# Schema

Two node types. Nothing else gets a card.

## `type: object`

A noun: a type, interface, or tightly-coupled group of small types/functions
that an editor would ask about as one thing (e.g. "the plugin contract",
"the event/report shape"). Frontmatter:

```yaml
type: object
cluster: engine | ble | discovery | enrich | report | cli | internal
universe: live | leftover | ghost
status: stub | verified
entity: <path to the owning file>
```

`status: verified` requires a citation (`path:line`) and a date/commit in
the card body. `stub` is a one-line placeholder in `objects/_index.md` with
no card body yet.

## `type: process`

A verb: a movement that actually runs (a pipeline, a build, a release).
Frontmatter:

```yaml
type: process
status: stub | verified
consumes: [links to object cards]
produces: [links to object cards]
```

## Naming

- Card files: kebab-case, named for the noun/verb (`device.md`, `arp.md`,
  `lan-scan.md`).
- File/type names and product language mostly agree in this repo (it's a
  CLI, not a product with marketing names) — no alias table needed. The one
  place they diverge is noted on the card itself: `core/engine.Device` is a
  type alias for `DeviceProfile` (see `objects/engine/device.md`).

## Universe notes specific to this repo

- **live**: everything reachable from `cmd/cli`'s blank imports, i.e.
  actually wired into a registry and runnable.
- **leftover**: none currently identified.
- **ghost**: `discovery/blescan_windows.go`'s Android bridge is referenced
  in comments (`core/ble/ble.go:46`) as "deferred" — not filed as code yet,
  so it isn't a ghost card, just a note on `objects/ble/ports.md`.
