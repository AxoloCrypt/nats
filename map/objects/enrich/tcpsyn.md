---
type: object
cluster: enrich
universe: live
status: verified
entity: enrich/tcpsyn/tcpsyn.go
---

# tcpsyn (Enricher)

Opt-in (`--enrich tcpsyn`) raw TCP SYN scan over the same common-ports list
`tcpconnect` uses, via `enrich/internal/rawcapture`.

## Why this shape

Needs a destination MAC to address the Ethernet frame directly (L2, same-
subnet only, like `discovery/arp`) — a device merged without a MAC (e.g.
mDNS/SSDP-only, no ARP corroboration) can't be targeted this way and is
skipped without error. `scanSourcePort` is a single fixed value
(`layers.TCPPort(54321)`) because raw packets never touch the OS socket
table, so there's no real port to collide with — replies are correlated by
that fixed destination port instead. Shares its port list with `tcpconnect`
(`enrich/tcpsyn/tcpsyn.go:26`) so the two scanners agree on what "common
services" means.

## Shape

- `enricher` implements `engine.Enricher` + `PrivilegeProber`
- `RequiresPrivilege`/`ProbePrivilege` delegate to `rawcapture.ProbeCapture`
- `Enrich` — crafts and sends raw SYN packets, collects SYN-ACK/RST replies within `responseWindow` (1s), writes `OpenPort` via `Device.Upsert`

Citations: `enrich/tcpsyn/tcpsyn.go:19` (`init`/register), `:26` (shared `scanPorts`), `:33` (`scanSourcePort`), `:44-57` (privilege methods), `:65` (`Enrich`)

## Connected to

- **owns:** `OpenPort` rows it writes
- **owned-by:** `core/engine`'s enricher registry ([../engine/ports.md](../engine/ports.md))
- **joins:** `enrich/internal/rawcapture` ([../internal/shared-plumbing.md](../internal/shared-plumbing.md)), shared with `udpscan`; `banner` (reads ports this opens)
- **looks-like-but-is-not:** `tcpconnect` — same port list, raw-packet mechanism instead of OS socket, needs privilege; `discovery/arp` — same pcap-adapter pattern, deliberately duplicated rather than shared code (see arp's card)

## If you change this

- **Hits:** `Device.OpenPorts` (same downstream consumers as `tcpconnect`), CI's libpcap-dev step, four-platform release matrix's `CGO_ENABLED=1`
- **Does not hit:** `dns`/`oui` (unrelated fields), `discovery/*` (independent code path despite the shared pcap pattern)

## Surfaces

| Surface | Role |
|---|---|
| `core/engine.Run` | drives via `Enricher.Enrich`/`PrivilegeProber`, only when named in `--enrich` |

## See

- Source: `enrich/tcpsyn/tcpsyn.go`
