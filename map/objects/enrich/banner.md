---
type: object
cluster: enrich
universe: live
status: verified
entity: enrich/banner/banner.go
---

# banner (Enricher)

Opt-in (`--enrich banner`) best-effort banner grab on a device's already-
known open TCP ports (populated by `tcpconnect` and/or `tcpsyn`).

## Why this shape

Has nothing to grab a banner from until a port-opening enricher has already
run, so it only does useful work if it runs after `tcpconnect`/`tcpsyn` in
the enrichment order. `tcpconnect` is guaranteed first (it's in the always-
on default set, and `cmd/cli/root.go`'s `buildOptions` always prepends the
defaults before appending `--enrich` names) — but `tcpsyn` vs. `banner`'s
relative order is whatever the user typed in `--enrich` (e.g.
`--enrich tcpsyn,banner` works, `--enrich banner,tcpsyn` makes banner a
silent no-op for tcpsyn's ports specifically). Needs no privilege (connects
to an already-open TCP port over the normal OS stack) but still implements
the live-probe pattern for consistency with every other adapter.

## Shape

- `enricher` implements `engine.Enricher` + `PrivilegeProber`
- `Enrich` — iterates `device.OpenPorts`, dials each, reads a banner within a timeout, writes it back via `Device.Upsert` (same `(Port, Protocol)`, now with `Banner` set)
- `grabBanner` — the actual dial-and-read helper

Citations: `enrich/banner/banner.go:18` (`init`/register), `:58` (`Enrich`), `:89` (`grabBanner`)

## Connected to

- **owns:** the `Banner` field on `OpenPort` rows it updates in place (never creates a new port entry — only annotates one `tcpconnect`/`tcpsyn` already wrote)
- **owned-by:** `core/engine`'s enricher registry ([../engine/ports.md](../engine/ports.md))
- **joins:** `tcpconnect`/`tcpsyn` (must run before this in `--enrich` ordering to have any effect), `core/engine.Classify`'s banner-keyword signal (second-highest priority in the classify chain)
- **looks-like-but-is-not:** nothing else grabs banners

## If you change this

- **Hits:** `core/engine.Classify`'s banner-keyword matching, every `report/*` writer's `Banner` rendering
- **Does not hit:** `dns`/`oui` (unrelated fields); has no effect on a given port-opening enricher's ports if that enricher runs after `banner` in the effective order, but doesn't error — silently a no-op in that case

## Surfaces

| Surface | Role |
|---|---|
| `core/engine.Run` | drives via `Enricher.Enrich`/`PrivilegeProber`, only when named in `--enrich`, order-sensitive |

## See

- Source: `enrich/banner/banner.go`
