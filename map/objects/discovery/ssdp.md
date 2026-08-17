---
type: object
cluster: discovery
universe: live
status: verified
entity: discovery/ssdp/ssdp.go
---

# ssdp (DiscoveryTechnique)

Opt-in listen-based discovery via SSDP/UPnP (`koron/go-ssdp`) — mirrors
`mdns`'s shape and termination contract over a different protocol
(multicast UDP port 1900).

## Why this shape

Same listen-based / quiescence-then-self-close / 5-minute safety-net-timeout
pattern as `mdns` — see [mdns.md](mdns.md)'s "Why this shape" for the
shared rationale, not repeated here. `ssdp`'s distinguishing piece is
`extractIP`, which parses a device's IP out of the SSDP `LOCATION` header URL
rather than reading it off the UDP packet's source address.

## Shape

- `technique` implements `DiscoveryTechnique` + `PrivilegeProber` (no `AddressEnumerator`)
- `Run` — queries SSDP, emits one `Sighting` per response with `ServiceData` keyed `"type"`/`"usn"`/`"server"`/`"location"`, self-closes on quiescence
- `extractIP` — parses IP from the `LOCATION` header

Citations: `discovery/ssdp/ssdp.go:14` (`init`/register), `:47` (`RequiresPrivilege`), `:55` (`ProbePrivilege`), `:59` (`Run`), `:161` (`extractIP`)

## Connected to

- **owns:** nothing
- **owned-by:** `core/engine`'s technique registry ([../engine/ports.md](../engine/ports.md))
- **joins:** `core/engine.Classify`'s mDNS/SSDP `ServiceData` keyword signal (`core/engine/classify.go`)
- **looks-like-but-is-not:** `mdns` — same pattern, different `ServiceData` keys and IP-extraction mechanism

## If you change this

- **Hits:** `core/engine.Classify`'s `ServiceData` keyword matching if you change the keys ssdp writes, the quiescence contract `core/engine.Run` relies on
- **Does not hit:** `arp`/`icmp`/`mdns`

## Surfaces

| Surface | Role |
|---|---|
| `core/engine.Run` | drives via `DiscoveryTechnique`/`PrivilegeProber`, wraps in safety-net timeout |
| `cmd/cli/root.go` | reachable via `--techniques ssdp` (not in the default set) |

## See

- Source: `discovery/ssdp/ssdp.go`
