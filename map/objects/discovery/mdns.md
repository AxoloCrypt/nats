---
type: object
cluster: discovery
universe: live
status: verified
entity: discovery/mdns/mdns.go
---

# mdns (DiscoveryTechnique)

Opt-in listen-based discovery via mDNS (`hashicorp/mdns`) — no fixed address
set, doesn't implement `AddressEnumerator`.

## Why this shape

Listen-based techniques self-terminate after a quiescence window with no new
sighting; `core/engine.Run` wraps them in a 5-minute safety-net timeout
(`safetyNetTimeout`, `core/engine/engine.go:32`) purely as a defect backstop
— that timeout firing in normal operation would mean this technique's own
quiescence logic broke. `RequiresPrivilege`/`ProbePrivilege` check multicast
UDP listen capability (port 5353), same live-probe convention as `arp`/`icmp`
but over a socket bind instead of pcap/raw-socket.

## Shape

- `technique` implements `DiscoveryTechnique` + `PrivilegeProber` (no `AddressEnumerator`)
- `Run` — queries mDNS, emits one `Sighting` per response with `ServiceData` keyed `"hostname"`/`"name"`/`"info"`, self-closes on quiescence

Citations: `discovery/mdns/mdns.go:13` (`init`/register), `:46` (`RequiresPrivilege`), `:54` (`ProbePrivilege`), `:58` (`Run`)

## Connected to

- **owns:** nothing
- **owned-by:** `core/engine`'s technique registry ([../engine/ports.md](../engine/ports.md))
- **joins:** `core/engine.Classify`'s mDNS/SSDP `ServiceData` keyword signal (`core/engine/classify.go`), `core/engine.Run`'s `safetyNetTimeout` wrapping (`processes/lan-scan.md`)
- **looks-like-but-is-not:** `ssdp` — same listen-based/quiescence pattern, different protocol and `ServiceData` key set

## If you change this

- **Hits:** `core/engine.Classify`'s `ServiceData` keyword matching if you change the keys mdns writes into `ServiceData`, the quiescence/self-termination contract `core/engine.Run` relies on to avoid hitting the 5-minute backstop
- **Does not hit:** `arp`/`icmp` (sweep-based, unrelated termination logic), `ssdp` (independent `ServiceData` key set)

## Surfaces

| Surface | Role |
|---|---|
| `core/engine.Run` | drives via `DiscoveryTechnique`/`PrivilegeProber`, wraps in safety-net timeout |
| `cmd/cli/root.go` | reachable via `--techniques mdns` (not in the default set) |

## See

- Source: `discovery/mdns/mdns.go`
