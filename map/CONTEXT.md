# map/ — how to walk this

This is a **system map** (ICM form 6): a record library of nouns plus a
short shelf of verbs plus a change-impact index, laid over the `nats` repo.
It is not a second spec — every claim it makes is a citation into `../`.

## Universes

| Universe | Meaning | How to treat it |
|---|---|---|
| **live** | wired into a registry via `init()` and reachable from `cmd/cli`'s blank imports | implement and cite against these |
| **leftover** | present, no longer the main path | none identified in this repo currently |
| **ghost** | named in a comment/doc, not filed as code | don't implement against these — see `_meta/schema.md`'s note on the deferred Android BLE bridge |

Every card in this map is `live` unless its frontmatter says otherwise.

## Name collisions

See `CLAUDE.md`'s "Name collisions to know up front" — the LAN vertical
(`core/engine`) and BLE vertical (`core/ble`) define parallel, independently-
typed `Event`/`Report`/`Diagnostic`/`Writer` shapes that must never import
each other. Every object card in `objects/engine/` and `objects/ble/` is
otherwise structurally the same card shape.

## How the clusters divide

`objects/` clusters by layer, matching how `../CLAUDE.md` already describes
the architecture (discovery → engine → enrich → report, times two verticals,
plus the CLI that wires it together, plus shared internal plumbing):

- `engine/` — `core/engine`'s contract layer (types + plugin interfaces + registry) for the LAN vertical
- `ble/` — `core/ble`'s contract layer for the BLE vertical
- `discovery/` — one card per `DiscoveryTechnique`/`BLEScanner` implementation
- `enrich/` — one card per `Enricher` implementation
- `report/` — the writer sets for both verticals
- `cli/` — `cmd/cli`'s command wiring and the diagnostic-rendering choke point
- `internal/` — the five `internal/` shared-plumbing packages

## Walking rules

1. Start at `CLAUDE.md`, not here, for routing.
2. Open a cluster's `_index.md` line before opening a card — the index says
   whether the card is a filled-in `verified` card or a `stub`.
3. A card's "If you change this" section is first-order only. For the full
   transitive picture of a specific planned change, read `effects/CONTEXT.md`.
4. Don't read an entire cluster folder to answer a single-noun question —
   that's the slurp `_index.md` exists to prevent.
