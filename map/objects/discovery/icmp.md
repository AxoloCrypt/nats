---
type: object
cluster: discovery
universe: live
status: verified
entity: discovery/icmp/icmp.go
---

# icmp (DiscoveryTechnique)

Opt-in sweep-based discovery via ICMP echo (ping) across the subnet.

## Why this shape

Sweep-based like `arp` (implements `AddressEnumerator`), but its
`RequiresPrivilege` probe is a raw-socket check rather than a pcap-handle
check — different underlying OS mechanism, same "actually attempt the
privileged operation" convention. Unlike `arp`, an icmp sighting never
carries a `MAC` (ICMP has no link-layer identity), so devices it discovers
merge into existing `Device`s by IP only, or create IP-only devices, until
some other technique resolves a MAC.

## Shape

- `technique` implements `DiscoveryTechnique` + `AddressEnumerator` + `PrivilegeProber`
- `EnumerateAddresses` — full sweep set for the subnet
- `Run` — sends echo requests, emits one `Sighting` (IP only, no MAC) per reply

Citations: `discovery/icmp/icmp.go:18` (`init`/register), `:70` (`RequiresPrivilege`), `:79` (`ProbePrivilege`), `:86` (`EnumerateAddresses`), `:125` (`Run`)

## Connected to

- **owns:** nothing
- **owned-by:** `core/engine`'s technique registry ([../engine/ports.md](../engine/ports.md))
- **joins:** `core/engine.Merge` — icmp is the concrete case behind Merge's "no-MAC sighting merges by IP unless another sighting asserts a conflicting MAC for that IP" rule (`core/engine/merge.go`)
- **looks-like-but-is-not:** `arp` — both sweep-based, but arp's sightings carry MAC and icmp's never do

## If you change this

- **Hits:** `core/engine.Merge`'s IP-only merge path (any change to what icmp puts in a `Sighting` changes what Merge has to reconcile)
- **Does not hit:** `arp`/`mdns`/`ssdp`, any enricher

## Surfaces

| Surface | Role |
|---|---|
| `core/engine.Run` | drives via `DiscoveryTechnique`/`AddressEnumerator`/`PrivilegeProber` |
| `cmd/cli/root.go` | reachable via `--techniques icmp` (not in the default set) |

## See

- Source: `discovery/icmp/icmp.go`
