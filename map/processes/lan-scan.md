---
type: process
status: verified
consumes: [../objects/engine/ports.md, ../objects/discovery/arp.md, ../objects/discovery/icmp.md, ../objects/discovery/mdns.md, ../objects/discovery/ssdp.md, ../objects/enrich/dns.md, ../objects/enrich/oui.md, ../objects/enrich/tcpconnect.md, ../objects/enrich/tcpsyn.md, ../objects/enrich/udpscan.md, ../objects/enrich/banner.md]
produces: [../objects/engine/event-model.md, ../objects/engine/device.md]
---

# lan-scan

`core/engine.Run` turns requested discovery techniques into a fully merged,
enriched, classified `Report`, streaming progress the whole way.

## Input → Movement → Output

Input: `engine.Options` (techniques, enrich list, subnet, format) from
`cmd/cli/root.go`'s `buildOptions`. Movement: resolve subnet → run each
technique, re-merging sightings into devices after every technique
completes → run enrichers in a fixed order over the merged devices → classify
each device exactly once. Output: a `chan Event` ending in one `Done` event
carrying the final `Report`.

## Why this shape

Re-merging after *each* technique (not once at the end) is what makes
`DeviceFound`/`DeviceUpdated` live rather than batched — a driving adapter
can render devices as they're discovered instead of staring at a blank
screen until the whole scan finishes. Classification runs exactly once,
strictly after all enrichment, specifically so it always sees the complete
signal set regardless of which techniques/enrichers actually ran — running
it per-technique or per-enricher would make `DeviceType` depend on
technique/enrich ordering, which nothing else in the pipeline does.

## Steps

1. `resolveSubnet(opts)` — auto-detect or use `--subnet`. (`core/engine/subnet.go:9`)
2. For each name in `opts.Techniques` (default `["arp"]`): privilege-check via `RequiresPrivilege`/`PrivilegeProber`, emit `TechniqueStarted`, run `tech.Run`, collect `Sighting`s. Listen-based techniques get wrapped in `safetyNetTimeout` (5 min, defect backstop only). (`core/engine/engine.go:144-245`)
3. After each technique's sightings land: `Merge(sightings)` resolves device identity (MAC-based; IP-only fallback suppressed if any sighting in this scan asserts a conflicting MAC for that IP), then `recordDeviceEvent` diffs against `knownDevices` to emit `DeviceFound`/`DeviceUpdated`. (`core/engine/merge.go:8`, `core/engine/engine.go:40-78,241-244`)
4. `enrichDevices(ctx, devices, opts.EnrichOptions, ch)` — runs each enricher in order (defaults `dns,oui,tcpconnect` prepended, then any `--enrich` names) with a shared 30s `enrichTimeout`; unknown/privilege-denied enrichers produce a warning `Diagnostic`, not a fatal error. (`core/engine/engine.go:38,89-142,252-260`)
5. `Classify(d)` runs once per device, after all enrichment. (`core/engine/classify.go:81`, `core/engine/engine.go:265-277`)
6. If nothing failed but zero devices were found, append an error-severity "no devices discovered" `Diagnostic` — the assumption that at least a router is reachable is LAN-specific (contrast `ble-scan.md`). (`core/engine/engine.go:283-289`)
7. Emit `Done` with the full `Report`, close the channel. (`core/engine/engine.go:291-299`)

## If you change this

- **Hits:** `cmd/cli/root.go`'s `runScan` exit-code contract (an empty result now exits 1 — `TestScanCommand_NoDevicesDiscovered_ExitsNonzero`), every discovery/enrich implementation's `RequiresPrivilege`/`PrivilegeProber` contract, `deviceKey`'s MAC-else-IP identity rule (must stay in sync with `Merge`'s own identity rule or `DeviceFound`/`DeviceUpdated` diverges from what `Merge` actually did)
- **Does not hit:** `core/ble.Run` (independent pipeline, no shared code — see `ble-scan.md`)

## Surfaces

| Surface | Role |
|---|---|
| `cmd/cli/root.go` | sole driving adapter |
| `discovery/*`, `enrich/*` | plugged in via registry lookups only |

## See

- Objects: [../objects/engine/event-model.md](../objects/engine/event-model.md), [../objects/engine/device.md](../objects/engine/device.md), [../objects/engine/ports.md](../objects/engine/ports.md)
- Source: `core/engine/engine.go`, `core/engine/merge.go`, `core/engine/classify.go`, `core/engine/subnet.go`
