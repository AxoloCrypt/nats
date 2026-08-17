---
type: object
cluster: enrich
universe: live
status: verified
entity: enrich/udpscan/udpscan.go
---

# udpscan (Enricher)

Opt-in (`--enrich udpscan`) raw UDP probe scan via `enrich/internal/rawcapture`
— the UDP sibling of `tcpsyn`, distinguishing open/closed by whether an ICMP
port-unreachable comes back.

## Why this shape

UDP has no SYN-ACK/RST handshake to read, so "open" here means "no ICMP
port-unreachable within the response window" (an absence-based signal,
inherently less certain than TCP's) rather than a positive open
confirmation — `parsePortUnreachable` is the actual open/closed
discriminator, not `parseUDPReply`. Shares the raw-capture plumbing with
`tcpsyn` (same `rawcapture` package) but is a separate probe/parse
implementation because the packet shapes and success signal differ
completely.

## Shape

- `enricher` implements `engine.Enricher` + `PrivilegeProber`
- `Enrich` — sends raw UDP probes, classifies open/closed from ICMP port-unreachable presence/absence, writes `OpenPort` via `Device.Upsert`

Citations: `enrich/udpscan/udpscan.go:19` (`init`/register), `:58` (`Enrich`), `:284` (`parsePortUnreachable`, the open/closed discriminator)

## Connected to

- **owns:** `OpenPort` rows it writes (`Protocol: "udp"`)
- **owned-by:** `core/engine`'s enricher registry ([../engine/ports.md](../engine/ports.md))
- **joins:** `enrich/internal/rawcapture` ([../internal/shared-plumbing.md](../internal/shared-plumbing.md)), shared with `tcpsyn`
- **looks-like-but-is-not:** `tcpsyn` — same raw-capture plumbing, unrelated protocol/parse logic

## If you change this

- **Hits:** `Device.OpenPorts` consumers, CI's libpcap-dev step, four-platform release matrix's `CGO_ENABLED=1`
- **Does not hit:** `dns`/`oui`/`tcpconnect` (TCP-only), `discovery/*`

## Surfaces

| Surface | Role |
|---|---|
| `core/engine.Run` | drives via `Enricher.Enrich`/`PrivilegeProber`, only when named in `--enrich` |

## See

- Source: `enrich/udpscan/udpscan.go`
