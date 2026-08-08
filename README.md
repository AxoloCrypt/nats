# nats

`nats` is a command-line network scanner. It discovers devices on your local
network and enriches them with identifying information — hostname, MAC
vendor, open ports, and a best-effort device type — then prints a summary in
the format you ask for.

It also scans for nearby Bluetooth Low Energy devices, listing each one with
its vendor, a guessed device type, and a rough distance estimate.

## What it does

- **Discovery** — finds devices via ARP, ICMP, mDNS, or SSDP, alone or in
  combination, and merges what each technique saw into one device per host.
- **Enrichment** — resolves reverse DNS, MAC OUI vendor, and open TCP ports
  by default; SYN scanning, UDP scanning, and banner grabbing are available
  opt-in.
- **Classification** — infers a device type (router, printer, phone, smart
  TV, IoT device, computer) from the combined signals, on a best-effort
  basis.
- **BLE scanning** — a passive, bounded listening window that never pairs,
  connects, or persists anything between runs.
- **Output** — `table`, `json`, `markdown`, or `plain`, to stdout and
  optionally to a file at the same time.
- **Degrades instead of failing** — anything needing privilege you don't
  have is skipped with a warning naming what and why, and the rest of the
  scan still runs.

Single binary, no runtime beyond `libpcap`/Npcap for the raw-packet
features. Prebuilt for Linux (x86_64, ARM64, ARMv7) and Windows (x86_64).

**→ [Installation, command reference, and examples](docs/USAGE.md)**

## Project status

`nats` is **under active development and has not reached a stable release.**

Expect bugs, rough edges, and incomplete features. Flags, output formats,
and behaviour may change between versions without notice. Results may be
wrong or incomplete: device-type classification is explicitly best-effort,
BLE distance figures are coarse estimates with wide uncertainty bands, and a
scan finding nothing is not proof that nothing is there.

Don't rely on it for anything where a wrong or missing result matters.
Bug reports and issues are welcome.

## Responsible use

`nats` is a network scanning tool. Scanning networks, hosts, or devices that
you do not own — or do not have explicit permission to test — may be illegal
where you live, regardless of your intent and regardless of whether anything
is harmed or disrupted. Many organisations also prohibit it by policy on
networks they operate.

**You are solely responsible for how you use this tool.** It is your
responsibility to ensure your use of it complies with all applicable laws
and with the rules of any network you run it against.

`nats` is published for legitimate purposes: inventorying and understanding
networks you own or are authorised to assess. The author accepts no
responsibility or liability for how anyone else chooses to use it, or for
any damage, disruption, or legal consequence arising from its use or misuse.

## License

`nats` is free software: you can redistribute it and/or modify it under the
terms of the **GNU General Public License, version 3** as published by the
Free Software Foundation. See [`LICENSE`](LICENSE) for the full text.

This program is distributed in the hope that it will be useful, but WITHOUT
ANY WARRANTY; without even the implied warranty of MERCHANTABILITY or
FITNESS FOR A PARTICULAR PURPOSE. See the GNU General Public License for
more details.

### Third-party components

`nats` links several permissively licensed Go libraries — `gopacket`,
`miekg/dns`, `godbus`, `pflag`, `tinygo.org/x/bluetooth` and the `golang.org/x`
packages (BSD-3-Clause), `hashicorp/mdns` and `koron/go-ssdp` (MIT), and
`spf13/cobra` (Apache-2.0) — all of which are GPL-3.0 compatible. Packet
capture goes through `libpcap` (BSD-3-Clause) on Linux and Npcap on Windows;
neither is bundled with `nats`, and Npcap must be obtained separately from
[npcap.com](https://npcap.com/) under its own license terms.

The Bluetooth company-identifier table in `core/ble/vendor_data.go` is
generated from Nordic Semiconductor's
[bluetooth-numbers-database](https://github.com/NordicSemiconductor/bluetooth-numbers-database),
itself derived from the Bluetooth SIG's public Assigned Numbers list.
