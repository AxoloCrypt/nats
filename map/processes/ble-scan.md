---
type: process
status: verified
consumes: [../objects/ble/ports.md, ../objects/discovery/blescan.md]
produces: [../objects/ble/event-model.md, ../objects/ble/device.md]
---

# ble-scan

`core/ble.Run` turns one platform `BLEScanner` into a de-duplicated
`Report` of compiled `BLEDeviceProfile`s.

## Input → Movement → Output

Input: `ble.Options` (just `Window`, default 5s) from `cmd/cli/ble.go`'s
`buildBLEOptions`. Movement: probe the single registered scanner, start a
timed scan, merge repeat advertisements from the same `Address` into one row
each, streaming `DeviceFound` on first sighting only. Output: a `chan Event`
ending in one `Done` event carrying the final `Report`.

## Why this shape

No fan-out, no re-merge-after-each-technique step like `lan-scan` — there is
exactly one scanner per platform build, so the pipeline is flatter by
construction. The merge-by-`Address` step exists because a real BLE
peripheral re-broadcasts every ~20ms-1.28s; without it, one physical device
would produce dozens of report rows per scan window. Three skip paths (no
scanner registered, `Probe` fails, `Scan` fails) all funnel through the same
`skipDiagnostic` helper so their severity/wording can't drift apart, and — a
deliberate divergence from `lan-scan` — an empty-but-successful scan reports
no error `Diagnostic` at all, because "nothing broadcasting nearby" is
ordinary for BLE, unlike LAN's router-is-always-reachable assumption.

## Steps

1. `GetScanner()` — no registered scanner → skip with `"no BLEScanner registered"`. (`core/ble/ble.go:88-92`)
2. `s.Probe()` — `ok=false` → skip with the scanner's own reason (or `unknownSkipReason` if blank/whitespace-only). (`core/ble/ble.go:94-97,34-43`)
3. `s.Scan(ctx, opts.Window)` — error → skip with `err.Error()`. (`core/ble/ble.go:100-104`)
4. For each `Advertisement` on `advCh`: if `Address` already seen, merge into the existing row (`DistanceEstimate` always overwritten; `Name`/`Vendor`/`DeviceType` only overwritten while still at their placeholder value) and don't re-fire `DeviceFound`; otherwise build a new `BLEDeviceProfile`, emit `DeviceFound`. (`core/ble/ble.go:114-179`)
5. On `advCh` close: emit `Done` with the compiled `Report`, close `ch`. No "zero devices" error diagnostic is ever added here, unlike `lan-scan`'s step 6. (`core/ble/ble.go:181-184`)

## If you change this

- **Hits:** `cmd/cli/ble.go`'s `runBLEScan` (always exits 0 on an empty-but-successful scan — no equivalent to `lan-scan`'s exit-1 case), every BLE writer (reads the compiled `BLEDeviceProfile`s)
- **Does not hit:** `core/engine.Run` (independent pipeline; `core/ble/import_test.go` enforces no import of `core/engine`)

## Surfaces

| Surface | Role |
|---|---|
| `cmd/cli/ble.go` | sole driving adapter |
| `discovery/blescan` | the one registered `BLEScanner`, plugged in via registry lookup only |

## See

- Objects: [../objects/ble/event-model.md](../objects/ble/event-model.md), [../objects/ble/device.md](../objects/ble/device.md), [../objects/ble/ports.md](../objects/ble/ports.md)
- Source: `core/ble/ble.go`
