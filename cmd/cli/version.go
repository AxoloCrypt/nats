package main

import (
	"fmt"
	"strings"

	"nats/internal/strutil"

	"github.com/spf13/cobra"
)

// version is the version this binary reports. It is the fallback for a plain
// `go build ./cmd/cli`, not the source of truth for a release: GoReleaser's
// default ldflags include -X main.version={{.Version}}, and .goreleaser.yaml
// declares no ldflags of its own, so every binary in the four-platform release
// matrix gets this variable overwritten with the v* tag being built.
//
// GoReleaser's {{.Version}} strips the tag's leading "v" ("v0.1.0" -> "0.1.0"),
// so the injected value and a hand-written one can legitimately differ in that
// prefix — versionString normalizes it rather than assuming either form. That
// dependency on GoReleaser's defaults is guarded by
// TestGoReleaserConfigDoesNotOverrideVersionLdflags at the repo root: adding a
// custom ldflags: to .goreleaser.yaml without re-adding -X main.version would
// otherwise make released binaries silently report this literal instead of
// their own tag.
var version = "0.1.0"

// unknownVersion is what `nats version` reports when the injected value is
// blank — e.g. a build passing an empty -X main.version=. Printing "nats "
// with nothing after it would read as a broken command rather than as a
// missing version, which is the one thing this command exists to tell you.
const unknownVersion = "unknown"

// versionString renders version in the "vMAJOR.MINOR.PATCH" form the project's
// git tags use, so what the command prints can be pasted straight into a bug
// report or a `git checkout` without the reader having to guess whether the
// leading "v" belongs there.
func versionString() string {
	if strutil.IsBlank(version) {
		return unknownVersion
	}
	return "v" + strings.TrimPrefix(strings.TrimSpace(version), "v")
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the nats version",
	Long: `Print the version of nats this binary was built from.

Released binaries report the git tag they were built from. A binary built
locally from source reports the in-development version compiled into it,
which is not necessarily the version of any release.

Exit code: always 0 — the command performs no scan and reads nothing from
the network or the host.`,
	Run: func(cmd *cobra.Command, args []string) {
		// reportWriter (stdout), not progressWriter: the version string is
		// this command's actual output, not progress commentary about it, so
		// it must survive `nats version > file` — and, like the scan/ble
		// writers' output, it goes through the swappable global so tests can
		// capture it.
		fmt.Fprintf(reportWriter, "nats %s\n", versionString())
	},
}
