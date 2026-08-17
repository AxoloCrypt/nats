# map/CLAUDE.md — change-impact map of `nats`

A walkable graph of `nats`'s nouns (types/interfaces) and verbs (pipelines),
built so an editor — human or agent — can answer "what is X" and "what else
moves if I touch X" without reading the whole tree. The root `../CLAUDE.md`
explains the architecture in prose; this map cites exact `path:line`
locations and states first-order change impact. The subject tree (`../`) is
the source of truth — this map only points at it.

## Name collisions to know up front

- **Two independent verticals, same shapes, no shared import.** `core/engine`
  (LAN) and `core/ble` (BLE) each define their own `Event`/`Report`/
  `Diagnostic`/`Writer`. They look identical and must never import each
  other (`core/engine/import_test.go`, `core/ble/import_test.go`,
  `cmd/cli/diagnostic_enforcement_test.go` enforce this at the type-system
  level). If you're looking at a `Diagnostic`, check which package it's from.
- **`Device` is an alias.** `engine.Device` is `= DeviceProfile`
  (`core/engine/types.go:65`), not a separate type.

## Where things live

| Folder | What it holds |
|---|---|
| `objects/` | one card per noun (type/interface), clustered by layer |
| `processes/` | one card per verb — a pipeline or build that actually runs |
| `effects/` | change-impact catalog: "changing X? open these cards" |
| `_meta/schema.md` | the two node types and their frontmatter |
| `_templates/` | blank `object.md` / `process.md` starters |

## Route by what you're trying to do

| If you're... | Go to |
|---|---|
| asking "what is `<type/interface>`?" | `objects/_index.md`, then the cluster card it points to |
| about to change a file and want to know the blast radius | `effects/CONTEXT.md` |
| asking "how does a scan actually run end to end?" | `processes/lan-scan.md` or `processes/ble-scan.md` |
| asking "how does a release get built and shipped?" | `processes/release.md` |
| adding a new discovery technique / enricher / writer | an existing card in `objects/discovery/` or `objects/enrich/` for the pattern, then `../CLAUDE.md`'s "Conventions" section for the registration rule |

## The one rule

This map cites the subject tree; it never re-explains it. If a card and the
source disagree, the source wins and the card is stale — fix the card.
