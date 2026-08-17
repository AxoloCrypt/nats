---
type: process
status: verified
consumes: [../objects/cli/version-cmd.md]
produces: []
---

# release

A `v*` tag push cross-builds the fixed four-platform matrix and publishes a
GitHub release; every push (tag or not) runs build+test only.

## Input → Movement → Output

Input: a git push. Movement: `.github/workflows/ci.yaml`'s `test` job always
runs `go build ./...`/`go vet ./...`/`go test ./...`; a `v*` tag additionally
triggers one `goreleaser build --single-target` matrix leg per platform (each
on the runner OS that can natively/cross-provide that platform's C toolchain
+ libpcap/Npcap headers), then a `publish` job archives/checksums/creates the
GitHub release by hand. Output: four platform binaries in one GitHub release,
or (non-tag push) just a green/red CI check.

## Why this shape

OSS GoReleaser can't merge binaries built on different runners into one
release (that split/merge workflow is GoReleaser-Pro-only), so
`goreleaser release` isn't used at all — each matrix leg runs `goreleaser
build --single-target` and the `publish` job does the archiving/checksum/
release step itself once every leg's binary is downloaded. `.goreleaser.yaml`
declares no `ldflags:` of its own specifically so GoReleaser's *default*
`-X main.version={{.Version}}` keeps injecting the tag into
`cmd/cli.version` ([../objects/cli/version-cmd.md](../objects/cli/version-cmd.md))
— adding a custom `ldflags:` without re-adding that flag would silently ship
binaries reporting the stale `"0.1.0"` literal, which is exactly what
`TestGoReleaserConfigDoesNotOverrideVersionLdflags` (repo root) exists to
catch. The four-target matrix itself is a fixed, deliberate project decision
— `goreleaser_config_test.go` fails the build if `.goreleaser.yaml` ever
drifts from it. darwin/amd64 and darwin/arm64 were deliberately dropped
(2026-08-07): BLE scanning would need `tinygo-org/cbgo`, a cgo binding
against CoreBluetooth, which would violate the "BLE vertical introduces no
new cgo dependency" rule — note this doesn't make the *rest* of the build
cgo-free, since `gopacket/pcap` already requires `CGO_ENABLED=1` on every
remaining target.

## Steps

1. Every push: `test` job — checkout, setup-go, `apt-get install libpcap-dev`, `go build ./...`, `go vet ./...`, `go test ./...`. (`.github/workflows/ci.yaml`, `test` job)
2. `v*` tag push only: one matrix leg per platform in `.goreleaser.yaml`'s `builds:` (`linux-amd64`, `linux-arm64`, `linux-arm`, `windows-amd64`), each `CGO_ENABLED=1` with its own cross-toolchain env vars. (`.goreleaser.yaml:35-79`)
3. `publish` job: downloads each leg's binary, archives (`tar.gz`, `zip` on Windows) with `README.md`/`LICENSE`/`docs/USAGE.md` bundled in (GPL-3.0 compliance requirement), checksums, creates the GitHub release. (`.goreleaser.yaml:88-107`, `.github/workflows/ci.yaml`'s `publish` job)

## If you change this

- **Hits:** `goreleaser_config_test.go` (matrix drift), `TestGoReleaserConfigDoesNotOverrideVersionLdflags` (custom `ldflags:`), `docs/USAGE.md`'s platform table (must stay in sync with the matrix), the `publish` job's hand-rolled archive naming (must stay in sync with `.goreleaser.yaml`'s `name_template`)
- **Does not hit:** `core/engine`/`core/ble` runtime behavior — this process only affects how binaries are built and shipped, never what they do

## Surfaces

| Surface | Role |
|---|---|
| GitHub Actions | executes both jobs |
| a maintainer pushing a `v*` tag | triggers the release path |

## See

- Objects: [../objects/cli/version-cmd.md](../objects/cli/version-cmd.md)
- Source: `.github/workflows/ci.yaml`, `.goreleaser.yaml`, `goreleaser_config_test.go`
