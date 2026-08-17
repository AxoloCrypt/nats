---
type: object
cluster: enrich
universe: live
status: verified
entity: enrich/oui/oui.go
---

# oui (Enricher)

Always-on default enricher: MAC OUI vendor lookup to populate `Vendor`.

## Why this shape

Reuses `gopacket/gopacket/macs.ValidMACPrefixMap` — an in-memory table
already shipped by `gopacket`, which the module already depends on for
`discovery/arp`'s packet capture — rather than adding a second OUI dataset
dependency. Needs no privilege (in-memory lookup); still implements the
live-probe pattern for consistency.

## Shape

- `enricher` implements `engine.Enricher`
- `Enrich` — looks up `device.MAC`'s prefix in `macs.ValidMACPrefixMap`, only overwrites `Vendor` on a match

Citations: `enrich/oui/oui.go:20` (`init`/register), `:58` (`Enrich`)

## Connected to

- **owns:** nothing
- **owned-by:** `core/engine`'s enricher registry ([../engine/ports.md](../engine/ports.md))
- **joins:** `core/engine.Classify`'s MAC-vendor-keyword signal (lowest priority in the classify chain — `core/engine/classify.go`)
- **looks-like-but-is-not:** nothing else does vendor lookup

## If you change this

- **Hits:** `Device.Vendor` consumers — every `report/*` writer, `core/engine.Classify`'s vendor-keyword matching
- **Does not hit:** `dns`/`tcpconnect`/`tcpsyn`/`udpscan`/`banner`

## Surfaces

| Surface | Role |
|---|---|
| `core/engine.Run` | drives via `Enricher.Enrich`, part of the default set |

## See

- Source: `enrich/oui/oui.go`
