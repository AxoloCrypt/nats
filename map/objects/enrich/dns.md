---
type: object
cluster: enrich
universe: live
status: verified
entity: enrich/dns/dns.go
---

# dns (Enricher)

Always-on default enricher: reverse DNS lookup to populate `Hostname`.

## Why this shape

One of the three enrichers in `DefaultOptions().EnrichOptions`
(`core/engine/engine.go:15`) — runs on every `nats scan` unless overridden.
Needs no privilege; `RequiresPrivilege`/`ProbePrivilege` still exist and
still live-probe, for consistency with every adapter in the repo rather than
because DNS lookups can actually fail on privilege grounds.

## Shape

- `enricher` implements `engine.Enricher`
- `Enrich` — reverse-resolves `device.IP` via `net.DefaultResolver.LookupAddr` (swappable var), only overwrites `Hostname` on a successful resolution

Citations: `enrich/dns/dns.go:13` (`init`/register), `:44` (`Enrich`)

## Connected to

- **owns:** nothing
- **owned-by:** `core/engine`'s enricher registry ([../engine/ports.md](../engine/ports.md))
- **joins:** runs first in the default enrichment order, before `oui`/`tcpconnect` — see `processes/lan-scan.md`
- **looks-like-but-is-not:** nothing else in this repo does reverse DNS

## If you change this

- **Hits:** `Device.Hostname` consumers — every `report/*` writer, `core/engine.Classify` if a future signal reads `Hostname`
- **Does not hit:** `oui`/`tcpconnect`/`tcpsyn`/`udpscan`/`banner` (each owns a disjoint `Device` field)

## Surfaces

| Surface | Role |
|---|---|
| `core/engine.Run` | drives via `Enricher.Enrich`, always includes it unless `--enrich` omits the default set entirely |

## See

- Source: `enrich/dns/dns.go`
