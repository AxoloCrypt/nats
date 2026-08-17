---
type: object
cluster: cli
universe: live
status: verified
entity: cmd/cli/version.go
---

# version command

`nats version` — touches neither vertical. Prints the `version` package var,
normalized into `vMAJOR.MINOR.PATCH` form.

## Why this shape

`version`'s literal (`"0.1.0"`) is only the fallback for a local
`go build ./cmd/cli` — GoReleaser's *default* ldflags
(`-X main.version={{.Version}}`) overwrite it with the `v*` tag on every
release build, which is why `.goreleaser.yaml` declares no `ldflags:` of its
own (adding one without re-adding `-X main.version=` would silently ship
binaries reporting this stale literal). `versionString` strips/re-adds the
`v` prefix because GoReleaser's `{{.Version}}` strips it but a hand-set
build might not.

## Shape

- `version` — package var, ldflags-injectable
- `unknownVersion` — printed when the injected value is blank
- `versionString() string` — normalizes to `vX.Y.Z`
- `versionCmd` — always exits 0, reads nothing from the network or host

Citations: `cmd/cli/version.go:26` (`version`), `:32` (`unknownVersion`), `:38` (`versionString`), `:45` (`versionCmd`)

## Connected to

- **owns:** nothing
- **owned-by:** `cmd/cli/main.go` (`Execute()`, via `rootCmd.AddCommand`)
- **joins:** `.goreleaser.yaml` (no custom `ldflags:`, by design — see `processes/release.md`), `goreleaser_config_test.go` at repo root (`TestGoReleaserConfigDoesNotOverrideVersionLdflags`)
- **looks-like-but-is-not:** nothing — the only subcommand that touches neither `core/engine` nor `core/ble`

## If you change this

- **Hits:** `TestGoReleaserConfigDoesNotOverrideVersionLdflags` if you add a custom `ldflags:` to `.goreleaser.yaml` without re-adding `-X main.version=`
- **Does not hit:** `core/engine`, `core/ble`, any discovery/enrich/report package

## Surfaces

| Surface | Role |
|---|---|
| a user running `nats version` | triggers this path |
| GoReleaser | injects `version`'s value at release-build time |

## See

- Source: `cmd/cli/version.go`
