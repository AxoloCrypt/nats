---
type: object
cluster: enrich
universe: live
status: verified
entity: enrich/tcpconnect/tcpconnect.go
---

# tcpconnect (Enricher)

Always-on default enricher: OS-level TCP connect scan over a common-ports
list, no raw sockets — the non-privileged sibling of the opt-in `tcpsyn`.

## Why this shape

Goes through the real OS TCP stack (`net.Dial`), so it needs no elevated
privilege at all, unlike `tcpsyn`'s raw-packet approach — that's the entire
reason both exist: `tcpconnect` is always-on because it's cheap and
unprivileged, `tcpsyn` is opt-in because it needs pcap/raw-socket access and
is noisier/faster.

## Shape

- `enricher` implements `engine.Enricher`
- `Enrich` — dials each port in its common-ports list, writes an `OpenPort` via `Device.Upsert` on connect success

Citations: `enrich/tcpconnect/tcpconnect.go:15` (`init`/register), `:67` (`Enrich`)

## Connected to

- **owns:** `OpenPort` rows it writes (via `Device.Upsert`, [../engine/device.md](../engine/device.md))
- **owned-by:** `core/engine`'s enricher registry ([../engine/ports.md](../engine/ports.md))
- **joins:** `core/engine.Classify`'s open-port signal (highest priority in the classify chain), `banner` (reads the ports this enricher opens, if `tcpsyn` didn't already)
- **looks-like-but-is-not:** `tcpsyn` — same common-ports intent, different mechanism and privilege requirement; they deliberately share the same port list at `enrich/tcpsyn/tcpsyn.go:26` so the two "agree" on what's a common service

## If you change this

- **Hits:** `Device.OpenPorts` consumers (`core/engine.Classify`'s port-signal matching, every `report/*` writer, `banner`'s port list to probe)
- **Does not hit:** `dns`/`oui`

## Surfaces

| Surface | Role |
|---|---|
| `core/engine.Run` | drives via `Enricher.Enrich`, part of the default set |

## See

- Source: `enrich/tcpconnect/tcpconnect.go`
