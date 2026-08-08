# Using `nats`

Installation, command reference, and worked examples. For an overview of
what `nats` is — and the project-status and responsible-use notices you
should read first — see the [README](../README.md).

## Installation

Download the archive for your platform from the
[GitHub Releases page](https://github.com/AxoloCrypt/nats/releases), unpack
it, and put the `nats` (or `nats.exe`) binary on your `PATH`.

Every release ships prebuilt binaries for exactly these four platforms. The
list is a deliberate, fixed project decision — it does not change on a
per-release basis:

| Platform | Archive |
| --- | --- |
| Linux (x86_64) | `nats_linux_amd64.tar.gz` |
| Linux (ARM64, e.g. Raspberry Pi 4/5, 64-bit Termux) | `nats_linux_arm64.tar.gz` |
| Linux (ARMv7, e.g. older Android/Termux devices, Raspberry Pi Zero/1) | `nats_linux_arm.tar.gz` |
| Windows (x86_64) | `nats_windows_amd64.zip` |

### Runtime dependency: libpcap / Npcap

`nats`'s ARP discovery (the default technique) and its opt-in `tcpsyn`/
`udpscan` enrichers capture raw packets via `libpcap`. This is a **runtime**
dependency of the machine running `nats`, separate from the binary itself —
it doesn't change which platforms `nats` ships for, only what you need
installed to use the privileged parts of it:

- **Linux (including Termux/Android):** install `libpcap` via your package
  manager, e.g. `sudo apt-get install libpcap0.8` (Debian/Ubuntu),
  `sudo dnf install libpcap` (Fedora), or `pkg install libpcap` (Termux).
- **Windows:** install [Npcap](https://npcap.com/) (the WinPcap successor).
  During Npcap's installer, leave "Install Npcap in WinPcap API-compatible
  Mode" checked, which is what `nats` expects.

Discovery techniques that don't touch raw packets (`mdns`, `ssdp`) and
enrichers that don't either (`dns`, `oui`, `tcpconnect`, `banner`) work
without libpcap/Npcap installed.

## Scanning a network

```
nats scan [flags]
```

Running `nats scan` with no flags auto-detects your local subnet, performs
an ARP-only discovery sweep, and enriches every device found with reverse
DNS, MAC OUI vendor lookup, and a TCP connect port scan — then prints a
`table`-formatted summary to stdout.

### Flags

| Flag | Default | Description |
| --- | --- | --- |
| `--subnet` | auto-detected | Subnet to scan, in CIDR notation (e.g. `192.168.1.0/24`). |
| `--techniques` | `arp` | Comma-separated discovery techniques to run instead of the default: `arp`, `icmp`, `mdns`, `ssdp`. |
| `--enrich` | *(none — the three defaults below always run)* | Comma-separated **opt-in** enrichers to run in addition to the always-on defaults: `tcpsyn`, `udpscan`, `banner`. |
| `--format` | `table` | Output format for the scan summary: `table`, `json`, `markdown`, or `plain`. |
| `--output-file` | *(unset)* | Additionally write the scan summary to this file, verbatim, alongside the normal stdout output. |

The three enrichers that run by default and can't be turned off — reverse
DNS lookup, MAC OUI vendor lookup, and a TCP connect port scan — never need
elevated privilege.

## Scanning for BLE devices

```
nats ble [flags]
```

`nats ble` is fully independent of `nats scan`: it never triggers LAN or
Wi-Fi discovery, and vice versa. It runs OS-native passive BLE scanning and
never requires root/sudo — when the OS Bluetooth permission isn't granted,
`nats` reports why and exits cleanly instead of prompting for elevated
privilege.

Each run is a single bounded listening window: `nats ble` listens for
`--window`, reports once, and exits. Nothing is cached, persisted, or
correlated across runs.

```
$ nats ble --window 5s
ADDRESS            NAME       VENDOR      DEVICE TYPE  DISTANCE
aa:bb:cc:dd:ee:ff  My Watch   Acme Corp   wearable     ~1.2m (±0.4m)
11:22:33:44:55:66  unknown    unknown     unknown      ~4.8m (±1.9m)
```

### Flags

| Flag | Default | Description |
| --- | --- | --- |
| `--window` | `5s` | Listening window duration, greater than zero (e.g. `5s`, `10s`). |
| `--format` | `table` | Output format for the BLE scan summary: `table`, `json`, `markdown`, or `plain`. |
| `--output-file` | *(unset)* | Additionally write the BLE scan summary to this file, verbatim, alongside the normal stdout output. |

Any field a device doesn't broadcast — most commonly its name — renders as
an explicit `unknown` rather than being left blank or dropped, so a row
always has the same shape.

The completion line (`BLE scan complete. N devices found.`) and any
warnings go to **stderr**, so `--format json` output on stdout stays
parseable:

```
$ nats ble --format json > devices.json
BLE scan complete. 2 devices found.
```

Note that `nats ble --format json` includes a top-level `diagnostics` array
alongside `devices`. If Bluetooth permission is denied the scan is skipped,
`devices` is empty, and the reason appears in `diagnostics` — that is how a
script tells "nothing was nearby" from "the scan never ran".

## Checking the version

```
$ nats version
nats v0.1.0
```

`nats version` takes no flags, reads nothing from the network or the host,
and always exits 0. Its output goes to stdout, so it can be captured
directly (`nats version > version.txt`).

A binary from the [Releases page](https://github.com/AxoloCrypt/nats/releases)
reports the tag it was built from — quote it when filing an issue. A binary
you built yourself with `go build` reports the in-development version
compiled into the source, which is not necessarily any released version.

## Privilege requirements (root/sudo/Administrator)

Some techniques and enrichers need to open a raw socket or packet capture
handle, which the OS only grants to a privileged process:

| Requires root/sudo (or Administrator on Windows) | Does not |
| --- | --- |
| `arp` discovery (the default technique) | `icmp` discovery — usually works unprivileged, but depends on the OS's ping-group configuration |
| `tcpsyn` enricher (`--enrich tcpsyn`) | `mdns` / `ssdp` discovery |
| `udpscan` enricher (`--enrich udpscan`) | `dns`, `oui`, `tcpconnect` enrichers (always-on defaults) |
| | `banner` enricher (`--enrich banner`) |

If you run `nats scan` unprivileged and a selected technique/enricher can't
get the privilege it needs, `nats` doesn't fail the whole scan — it skips
just that one, prints a `warning` diagnostic naming it and why, and still
returns results for everything else you asked for. `nats scan --help`
describes these same requirements inline, so you get the same answer
whether you check `--help` up front or read the warning at runtime.

## Examples

```sh
# Default: auto-detected subnet, ARP-only discovery, default enrichment, table output
nats scan

# Explicit subnet, multiple discovery techniques
sudo nats scan --subnet 192.168.1.0/24 --techniques arp,mdns,ssdp

# Opt in to SYN scan + banner grabbing (SYN scan needs root/sudo)
sudo nats scan --enrich tcpsyn,banner

# JSON output, also saved to a file
nats scan --format json --output-file scan-results.json
```

## Building from source

Requires Go (see `go.mod` for the minimum version) and libpcap/Npcap
development headers installed (see the runtime dependency section above —
building, not just running, needs the headers too):

```sh
go build -o nats ./cmd/cli
```

## Release pipeline

Releases are cut via [GoReleaser](https://goreleaser.com) (`.goreleaser.yaml`)
and GitHub Actions (`.github/workflows/ci.yaml`): every push runs
`go build`/`go test`, and pushing a `v*` tag cross-compiles and publishes
binaries for all four platforms listed above.

The tag is also what a released binary reports from `nats version`:
GoReleaser's default ldflags inject it into `cmd/cli`'s `version` variable at
build time, so tagging is the only step needed to version a release — there
is no separate value to bump by hand for the released binaries. The literal
in `cmd/cli/version.go` is only the fallback for a local `go build`.
