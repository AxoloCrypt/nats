---
type: object
cluster: discovery
universe: live
status: verified
entity: discovery/arp/arp.go
---

# arp (DiscoveryTechnique)

The default LAN discovery technique — raw ARP request/reply over a pcap
handle, sweep-based (enumerates every host address in the subnet up front).

## Why this shape

`RequiresPrivilege()` and `ProbePrivilege()` both actually open a pcap handle
rather than checking OS/root status, per the repo-wide "live probe, not a
guess" convention (`../../CLAUDE.md`) — the real failure mode on a given
machine (no libpcap, permission denied, no such device) is what
`ProbePrivilege`'s error surfaces through `PrivilegeProber`. `pcapAdapter`
wraps the real pcap handle behind a small interface so `Run` can be tested
without a real NIC.

## Shape

- `technique` implements `DiscoveryTechnique` + `AddressEnumerator` + `PrivilegeProber`
- `EnumerateAddresses` — full sweep set for the subnet
- `Run` — sends/reads raw ARP frames, emits one `Sighting` per reply, closes the channel once every enumerated address is probed

Citations: `discovery/arp/arp.go:17` (`init`/register), `:27` (`RequiresPrivilege`), `:36` (`ProbePrivilege`), `:135` (`EnumerateAddresses`), `:154` (`Run`)

## Connected to

- **owns:** nothing
- **owned-by:** `core/engine`'s technique registry ([../engine/ports.md](../engine/ports.md))
- **joins:** `core/engine.Merge` (consumes its `Sighting`s), `enrich/tcpsyn`/`enrich/udpscan` (share `enrich/internal/rawcapture`, a separate copy of the same pcap-adapter pattern — not the same code)
- **looks-like-but-is-not:** `enrich/internal/rawcapture` — same pcap-wrapping pattern, deliberately duplicated rather than shared, because `discovery/internal` isn't importable from `enrich/*` (Go `internal/` visibility)

## If you change this

- **Hits:** anything relying on `Sighting.MAC` always being populated for arp-sourced sightings (`core/engine.Merge`'s MAC-based identity resolution), CI's libpcap-dev install step, the four-platform release matrix's `CGO_ENABLED=1` requirement
- **Does not hit:** `icmp`/`mdns`/`ssdp` (independent techniques), any enricher (arp only produces `Sighting`s, never touches `Device`)

## Surfaces

| Surface | Role |
|---|---|
| `core/engine.Run` | drives via `DiscoveryTechnique`/`AddressEnumerator`/`PrivilegeProber` |
| `cmd/cli/root.go` | reachable only via blank import + `--techniques arp` (the default) |

## See

- Source: `discovery/arp/arp.go`
