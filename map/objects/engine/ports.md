---
type: object
cluster: engine
universe: live
status: verified
entity: core/engine/ports.go
---

# DiscoveryTechnique / Enricher / Writer + registry

The three plugin interfaces every LAN-vertical adapter implements, plus the
package-level registry maps `init()` populates them into. This is the entire
extension surface of the LAN vertical — a new discovery technique, enricher,
or report format is exactly one new implementation of one of these three.

## Why this shape

`RequiresPrivilege()` is a live probe by contract, not a hardcoded
OS/platform guess (see `../../CLAUDE.md`'s "Privilege checks" convention) —
that's why it's a method on the interface rather than a static capability
flag the engine could just read off a registry entry. `AddressEnumerator`
and `PrivilegeProber` are optional capabilities (checked via type assertion
in `core/engine/engine.go`), not part of the required interface, because
listen-based techniques (`mdns`, `ssdp`) have no fixed address set to
enumerate and most techniques never fail privilege checks for a
non-generic reason.

## Shape

- `DiscoveryTechnique`: `Name()`, `RequiresPrivilege() bool`, `Run(ctx, target) (<-chan Sighting, error)`
- `AddressEnumerator` (optional): `EnumerateAddresses(target) ([]string, error)`
- `PrivilegeProber` (optional): `ProbePrivilege() (bool, error)`
- `Enricher`: `Name()`, `RequiresPrivilege() bool`, `Enrich(ctx, Device) (Device, error)`
- `Writer`: `Name()`, `Write(Report) ([]byte, error)`
- registry: `RegisterTechnique`/`GetTechnique`, `RegisterEnricher`/`GetEnricher`, `RegisterWriter`/`GetWriter`/`WriterNames`

Citations: `core/engine/ports.go:5` (`DiscoveryTechnique`), `:17` (`AddressEnumerator`), `:28` (`PrivilegeProber`), `:32` (`Enricher`), `:42` (`Writer`), `core/engine/registry.go:5-7` (three registry maps)

## Connected to

- **owns:** nothing — pure contract
- **owned-by:** nothing — this is the seam, not a leaf
- **joins:** every card in `objects/discovery/`, `objects/enrich/`, `objects/report/lan-writers.md`; consumed by `core/engine.Run` ([../../processes/lan-scan.md](../../processes/lan-scan.md))
- **looks-like-but-is-not:** `core/ble.BLEScanner`/`Writer` ([../ble/ports.md](../ble/ports.md)) — same pattern, independent interfaces

## If you change this

- **Hits:** every discovery/enrich/report implementation (must satisfy the new interface shape), `core/engine/engine.go`'s type assertions for the optional interfaces, `cmd/cli/root.go`'s blank-import list stays valid but any call-site using the old method signature breaks at compile time
- **Does not hit:** `core/ble` (independent interfaces, no shared type)

## Surfaces

| Surface | Role |
|---|---|
| `discovery/*`, `enrich/*`, `report/*` | implement (write side of the contract) |
| `core/engine.Run` | reads via registry lookups only — never imports a concrete adapter package |
| `cmd/cli/root.go` | triggers registration via blank imports; never calls registry functions directly except `WriterNames` |

## See

- Source: `core/engine/ports.go`, `core/engine/registry.go`
