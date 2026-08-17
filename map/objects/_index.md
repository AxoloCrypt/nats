# Object index

One line per noun. `verified` cards have citations; `stub` lines are a
placeholder only — open the source directly if you need one now.

## engine (LAN vertical contract layer — `core/engine`)

- `Device` / `DeviceProfile` / `OpenPort` — [engine/device.md](engine/device.md) — verified
- `Options` / `Event` / `Report` / `Diagnostic` / `Sighting` — [engine/event-model.md](engine/event-model.md) — verified
- `DiscoveryTechnique` / `Enricher` / `Writer` / `AddressEnumerator` / `PrivilegeProber` + registry — [engine/ports.md](engine/ports.md) — verified

## ble (BLE vertical contract layer — `core/ble`)

- `BLEDeviceProfile` / `Advertisement` — [ble/device.md](ble/device.md) — verified
- `Options` / `Event` / `Report` / `Diagnostic` — [ble/event-model.md](ble/event-model.md) — verified
- `BLEScanner` / `Writer` + registry — [ble/ports.md](ble/ports.md) — verified

## discovery (`DiscoveryTechnique` / `BLEScanner` implementations)

- `arp` — [discovery/arp.md](discovery/arp.md) — verified
- `icmp` — [discovery/icmp.md](discovery/icmp.md) — verified
- `mdns` — [discovery/mdns.md](discovery/mdns.md) — verified
- `ssdp` — [discovery/ssdp.md](discovery/ssdp.md) — verified
- `blescan` — [discovery/blescan.md](discovery/blescan.md) — verified

## enrich (`Enricher` implementations)

- `dns` — [enrich/dns.md](enrich/dns.md) — verified
- `oui` — [enrich/oui.md](enrich/oui.md) — verified
- `tcpconnect` — [enrich/tcpconnect.md](enrich/tcpconnect.md) — verified
- `tcpsyn` — [enrich/tcpsyn.md](enrich/tcpsyn.md) — verified
- `udpscan` — [enrich/udpscan.md](enrich/udpscan.md) — verified
- `banner` — [enrich/banner.md](enrich/banner.md) — verified

## report (`Writer` implementations)

- LAN writers (`table`/`json`/`markdown`/`plain`) — [report/lan-writers.md](report/lan-writers.md) — verified
- BLE writers (`table`/`json`/`markdown`/`plain`) — [report/ble-writers.md](report/ble-writers.md) — verified

## cli (`cmd/cli`)

- `scan` command wiring — [cli/root-cmd.md](cli/root-cmd.md) — verified
- `ble` command wiring — [cli/ble-cmd.md](cli/ble-cmd.md) — verified
- Diagnostic-rendering choke point — [cli/diagnostic-rendering.md](cli/diagnostic-rendering.md) — verified
- `version` command — [cli/version-cmd.md](cli/version-cmd.md) — verified

## internal (shared plumbing)

- `strutil` / `subnetutil` / `rawcapture` / `render` / `blerender` — [internal/shared-plumbing.md](internal/shared-plumbing.md) — verified
