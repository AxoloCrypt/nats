---
type: object
cluster: discovery
universe: live
status: verified
entity: discovery/blescan/blescan.go
---

# blescan (BLEScanner)

The sole `core/ble.BLEScanner` implementation, built on
`tinygo.org/x/bluetooth`. Platform-split for TX Power: the base file's
`enableAdapter`/`scanAdapter`/`stopScanAdapter` vars drive the
cross-platform scan lifecycle; `startTXPowerTracking`/`txPowerFor` are a
second seam that `blescan_linux.go` (BlueZ D-Bus) and `blescan_windows.go`
(WinRT COM interop) each override independently, because
`tinygo.org/x/bluetooth`'s `ScanResult` never carries TX Power itself.

## Why this shape

The OS-agnostic default for `startTXPowerTracking`/`txPowerFor` is a true
no-op pair (TXPower stays `nil`) — a platform that hasn't overridden both
behaves exactly as if TX Power tracking didn't exist, so adding TX Power
support for a third platform is additive (new platform file) rather than a
change to the base file. `startTXPowerTracking` is bound to `Scan`'s own
child context specifically so its goroutine can never outlive the scan that
started it.

## Shape

- `scanner` implements `ble.BLEScanner` (`Probe`, `Scan`)
- swappable vars: `enableAdapter`, `scanAdapter`, `stopScanAdapter`, `startTXPowerTracking`, `txPowerFor`
- `blescan_linux.go`: `txPowerTracker` sourced from BlueZ over D-Bus
- `blescan_windows.go`: `txPowerTracker` sourced from WinRT `IBluetoothLEAdvertisementReceivedEventArgs2` via raw COM vtable calls

Citations: `discovery/blescan/blescan.go:12` (`init`/register), `:58` (`Probe`), `:98` (`Scan`), `:44-48` (TX Power seam doc comment), `discovery/blescan/blescan_linux.go:24` (`init`), `discovery/blescan/blescan_windows.go:301` (`init`)

## Connected to

- **owns:** nothing
- **owned-by:** `core/ble`'s single-slot scanner registry ([../ble/ports.md](../ble/ports.md))
- **joins:** `core/ble.Run` (`processes/ble-scan.md`) — the only consumer of `Advertisement`s this package emits
- **looks-like-but-is-not:** `discovery/arp` etc. — those implement `engine.DiscoveryTechnique` (LAN vertical); `blescan` implements `ble.BLEScanner` (BLE vertical), unrelated interface

## If you change this

- **Hits:** `objects/ble/device.md`'s `Advertisement` shape if you add/remove a field the tracker populates, the four-platform release matrix (a fifth platform needs its own tracker file or falls back to the TXPower-`nil` default), the "no new cgo dependency" invariant — a macOS tracker via `tinygo-org/cbgo` would violate it, which is why darwin was dropped from `.goreleaser.yaml`
- **Does not hit:** `core/engine`/LAN discovery techniques (no shared code path)

## Surfaces

| Surface | Role |
|---|---|
| `core/ble.Run` | drives via `BLEScanner.Probe`/`Scan` |
| `cmd/cli/root.go` | reachable via blank import (registers on package init, used only through `nats ble`) |

## See

- Source: `discovery/blescan/blescan.go`, `discovery/blescan/blescan_linux.go`, `discovery/blescan/blescan_windows.go`
