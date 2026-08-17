# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

`nats` is a Go CLI network scanner (module `nats`, Go 1.25). It discovers
devices on the local network and enriches them with identity info
(hostname, MAC vendor, open ports, best-effort device type), then renders
a report in one of several formats. Entry point: `cmd/cli` (`main.go` just
calls `Execute()` in `root.go`).

For a change-impact map of this codebase's types/interfaces and pipelines —
exact `path:line` citations, and what a given change hits or doesn't — see
[`map/CLAUDE.md`](map/CLAUDE.md).

## Common commands

```sh
go build ./...              # build everything
go build -o nats ./cmd/cli  # build the CLI binary
go vet ./...
go test ./...                              # full test suite
go test ./core/engine/...                  # one package
go test ./core/engine/ -run TestMerge       # one test
go test ./... -race                        # CI does not run -race, but use it when debugging goroutine/channel code
```

Building/testing requires libpcap headers installed (`discovery/arp` and
the `tcpsyn`/`udpscan` enrichers cgo-bind to libpcap via `gopacket/pcap`,
even just to compile). On Debian/Ubuntu: `sudo apt-get install libpcap-dev`.

CI (`.github/workflows/ci.yaml`) runs `go build ./...`, `go vet ./...`,
`go test ./...` on every push. Pushing a `v*` tag additionally cross-builds
the four-platform release matrix via GoReleaser (`.goreleaser.yaml`) and
publishes a GitHub release. That four-platform list (linux/amd64,
linux/arm64, linux/arm, windows/amd64) is the project's fixed supported-
platform set — it's enforced by `goreleaser_config_test.go` at the repo
root, which fails the build if `.goreleaser.yaml`'s matrix ever drifts from
it. Treat any change to that matrix as a deliberate, explicit decision, not
a routine edit.

Any claim phrased as a cross-platform guarantee (e.g. "no new cgo
dependency," "runs without root") must be verified against every platform in
that same four-platform matrix, not inferred from a build on whichever host
happens to be running: `GOOS=<os> GOARCH=<arch> go list -deps ./pkg` to
check for an unwanted platform-specific dependency, and `CGO_ENABLED=0
GOOS=<os> go build ./pkg` to confirm it actually compiles cgo-free there. A
Linux-only `go build ./...` has passed here while a darwin-only dependency
was present, so it is not evidence of cgo-freedom.

## Architecture

Four layers, each a family of small Go packages that plug into
`core/engine` through interfaces and `init()`-time self-registration —
never through direct imports of each other:

```
discovery/*  →  core/engine (Merge, Classify)  →  enrich/*  →  report/*
```

- **`core/engine`** — the only package the others depend on. Defines the
  three plugin interfaces (`ports.go`): `DiscoveryTechnique`, `Enricher`,
  `Writer`, plus the optional capabilities `AddressEnumerator` (sweep-based
  techniques report how many addresses they'll probe) and `PrivilegeProber`
  (report *why* a privilege check failed, not just that it did). A global
  `registry.go` map holds every registered technique/enricher/writer by
  name. `engine.Run` (`engine.go`) is the orchestrator: resolve subnet →
  run each requested discovery technique → `Merge` sightings into devices
  after each technique completes (live updates, not one batch at the end)
  → run enrichers in order over the merged devices → `Classify` each
  device exactly once → emit a final `Done` event with the full `Report`.
  It streams progress as it goes over a `chan Event` (`TechniqueStarted`,
  `AddressProbed`, `DeviceFound`/`DeviceUpdated`, `TechniqueSkipped`,
  `Done`).

- **`discovery/*`** (`arp`, `icmp`, `mdns`, `ssdp`) — each implements
  `DiscoveryTechnique` and registers itself via a package-level `init()`
  (side-effect import only — see `cmd/cli/root.go`'s block of `_ "nats/discovery/..."`
  imports). `arp`/`icmp` are sweep-based (enumerate a fixed address set,
  implement `AddressEnumerator`, close their sighting channel once every
  address is probed). `mdns`/`ssdp` are listen-based (no fixed target set,
  self-terminate after a quiescence window with no new sighting — `engine.Run`
  wraps them in a 5-minute safety-net timeout as a defect backstop only,
  never as the intended termination path). `discovery/internal/subnetutil`
  holds interface/target-enumeration logic shared only within `discovery/*`
  (Go's `internal/` visibility).

- **`core/engine.Merge`** (`merge.go`) resolves device identity: sightings
  sharing a MAC become one device; a no-MAC sighting merges into an
  existing device only by exact IP match, *unless* some other sighting in
  the same scan asserts a conflicting MAC for that IP, in which case IP
  matching is suppressed for every sighting at that IP. `deviceKey`
  (`engine.go`) mirrors this rule (MAC when present, else IP) so the event
  stream can tell a genuinely new device from a later update to one
  already reported — including the case where a device first seen without
  a MAC later gets one resolved (reported as an update, not a duplicate
  "found").

- **`enrich/*`** (`dns`, `oui`, `tcpconnect` — always-on defaults;
  `tcpsyn`, `udpscan`, `banner` — opt-in via `--enrich`) — each implements
  `Enricher` and self-registers the same way. Enrichers run in a fixed
  order over the fully-merged device list; each receives the previous
  enricher's output, so a later enricher's write to the same field (e.g.
  `Hostname`) overrides an earlier one. The keep-vs-overwrite decision for
  a given field lives inside each enricher (only a successful resolution
  overwrites) — `enrichDevices` in `engine.go` just applies them in order.
  `Device.Upsert` (`types.go`) is the only mutation path for
  `OpenPorts` — enrichers never append to the slice directly, so a repeat
  write to the same `(Port, Protocol)` replaces in place. `enrich/tcpsyn`
  and `enrich/udpscan` share raw-capture plumbing via
  `enrich/internal/rawcapture` (a separate copy from
  `discovery/internal/subnetutil` because Go's `internal/` rule scopes
  visibility to the parent tree — `discovery/internal` isn't importable
  from `enrich/*`).

- **`core/engine.Classify`** (`classify.go`) assigns a best-effort
  `DeviceType`, run exactly once per device after all merge+enrichment has
  completed — never inside a `discovery/*` or `enrich/*` adapter, so it
  always sees the complete signal set regardless of which
  techniques/enrichers actually ran. Signal priority (most to least
  specific, first match wins): open printer/casting TCP port → banner
  keyword → mDNS/SSDP `ServiceData` keyword → MAC vendor keyword →
  `unknown`.

- **`report/*`** (`table` default, plus `json`, `markdown`, `plain`) —
  each implements `Writer` and self-registers. A `Writer` consumes only
  the final `engine.Report` struct, never engine internals, and picks its
  output format purely from `Report.Devices`/`Report.Diagnostics`.
  `report/internal/render` holds formatting helpers shared across writers
  (e.g. rendering `OpenPorts`, and `SanitizeLine`, which collapses newlines
  **and tabs** in untrusted values — tabs matter because the table writers
  delimit columns with `\t`).

- **`report/ble/*`** (`table` default, plus `json`, `markdown`, `plain`) —
  the BLE vertical's own writer set, mirroring `report/*` but implementing
  `ble.Writer` against `core/ble.Report`. Registered via
  `ble.RegisterWriter` into `core/ble`'s registry, which is entirely
  separate from `core/engine`'s. `report/ble/internal/blerender` holds the
  shared placeholder/sanitization rules for the three human-readable BLE
  writers; the JSON one deliberately skips it, because it guarantees every
  key is present in the object (no `omitempty`) rather than substituting a
  human-readable placeholder.

- **`cmd/cli`** wires everything together: blank-imports every
  `discovery/*`, `enrich/*`, `report/*` package (so their `init()`s run
  and populate the registries), translates flags into `engine.Options`,
  drives the `<-chan Event` to render a live progress line to stderr, and
  prints the final report to stdout (and optionally a file, verbatim,
  additive to stdout — never a replacement for it). **Every** `Diagnostic`
  — whether produced by `core/engine`, `core/ble`, or `cmd/cli` itself —
  must be printed exclusively through `renderDiagnostic` in `root.go`;
  nothing else may read a `Diagnostic`'s `Severity`/`Message`/`Reason`
  fields directly. `core/ble.Diagnostic` is a distinct type (the BLE
  vertical may not import `core/engine`), and its one sanctioned reader is
  `renderBLEDiagnostic` in `ble.go`, which only converts it into an
  `engine.Diagnostic` for `renderDiagnostic` to format — it is a
  conversion, not a second renderer. This is enforced by
  `cmd/cli/diagnostic_enforcement_test.go`, which uses `go/packages` type
  info (not name matching) across `cmd/cli`, `core/engine` **and**
  `core/ble` to fail the build if any other function reads those fields —
  keep that invariant in mind before adding new diagnostic-printing code
  anywhere. `cmd/cli/version.go` adds the third subcommand, `version`, which
  touches neither engine: it prints `cmd/cli`'s `version` variable, whose
  literal is only the fallback for a local `go build` — GoReleaser's *default*
  ldflags (`-X main.version={{.Version}}`) overwrite it with the `v*` tag on
  every release build. That's why `.goreleaser.yaml` declares no `ldflags:` of
  its own, and why adding one without re-adding `-X main.version=` would
  silently ship binaries reporting a stale version;
  `TestGoReleaserConfigDoesNotOverrideVersionLdflags` fails the build if that
  happens. Releases are versioned by tagging, not by editing that literal.

## Git workflow: trunk-based development

`main` is always releasable. Non-trivial changes happen on a short-lived
feature branch, never directly on `main`:

1. Before writing code, check the working tree and current branch; if the
   tree is dirty or the branch doesn't match the work about to start, stop
   and ask rather than guessing.
2. If on `main`, branch off it first: `git checkout -b <type>/<slug> main`
   (`feature/`, `fix/`, `chore/` — prefer the story/epic key as the slug,
   e.g. `feature/4-8-ble-advertisement-dedup`).
3. Commit incrementally on that branch using this project's existing
   conventional-commit prefixes (`feat:`, `fix:`, `docs:`, `test:`,
   `refactor:`, `chore:`).
4. Never commit directly to `main`. Never merge, push, or open a PR without
   the user's explicit go-ahead — those are shared-visibility actions.
5. Delete branches once merged; don't let them accumulate.

Full policy (branch naming, exceptions, how it's wired into BMad agents)
lives in `docs/development-strategy.md` in the sibling `nats-metarepo`
repo — restated here because that repo's BMad customization only resolves
when a workflow's project root is `nats-metarepo` itself, not `~/nats`.

## Conventions to follow when adding or changing code

- **Privilege checks are live probes, not guesses.** Every technique/enricher
  that might need elevated privilege implements `RequiresPrivilege() bool`
  by actually attempting the privileged operation (e.g. opening a pcap
  handle) — never by hardcoding based on OS/platform. Optionally implement
  `PrivilegeProber` to surface the real underlying error (permission
  denied vs. missing driver vs. no such device) instead of a generic
  "requires privilege" message. When privilege isn't available, the engine
  skips just that one technique/enricher with a `warning` diagnostic and
  keeps going — it never fails the whole scan.
- **Swappable package-level vars for testability.** External/impure calls
  (`net.Interfaces`, `pcap.OpenLive`, `net.DefaultResolver.LookupAddr`,
  `mdns.QueryContext`, etc.) are assigned to a `var` at package scope
  specifically so tests can substitute a fake — follow this pattern rather
  than reaching for an interface+constructor or a mocking library.
  Everything under `enrich/`/`discovery/` that touches raw sockets, DNS,
  or pcap has a real integration path plus a fully-faked test path; new
  adapters should offer both.
- **Registration is `init()` + blank import only.** A new
  discovery/enrich/report adapter registers itself in an `init()` calling
  `engine.RegisterTechnique`/`RegisterEnricher`/`RegisterWriter`; it becomes
  reachable by adding a `_ "nats/..."` import in the command that drives it
  — `cmd/cli/root.go` for the LAN vertical, `cmd/cli/ble.go` for the BLE
  writers (which register via `ble.RegisterWriter` into `core/ble`'s own
  registry). Nothing else should import a concrete adapter package
  directly.
- **`Report`/`Device`/`Diagnostic` are the only contract across layers.**
  `report/*` writers, and anything downstream of `engine.Run`, must only
  depend on those structs — never on discovery/enrich internals.
