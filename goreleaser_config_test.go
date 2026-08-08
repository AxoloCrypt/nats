// Package release holds repo-level meta-tests for the release pipeline that
// don't belong to any single Go package.
package release

import (
	"os"
	"regexp"
	"testing"
)

var buildsSectionRe = regexp.MustCompile(`(?ms)^builds:\n(.*?)(?:^\S|\z)`)
var buildEntryRe = regexp.MustCompile(`(?m)^  - id:`)
var goosRe = regexp.MustCompile(`(?m)^\s*goos:\s*\[(\w+)\]`)
var goarchRe = regexp.MustCompile(`(?m)^\s*goarch:\s*\[(\w+)\]`)

// TestGoReleaserConfigTargetsMatchSupportedPlatforms fails CI if
// .goreleaser.yaml's build matrix ever drifts from the four platforms this
// project supports: linux/amd64, linux/arm64, linux/arm, windows/amd64.
// Removing (or silently adding) a release target changes which platforms
// the project ships for, so it must be a deliberate decision rather than
// something a routine .goreleaser.yaml edit can slip past CI.
//
// The two darwin targets this test previously required were dropped on
// 2026-08-07, when macOS stopped being a supported platform, and this
// want-set was updated in the same commit as .goreleaser.yaml — the test
// and the config it guards must never move separately, or CI breaks.
func TestGoReleaserConfigTargetsMatchSupportedPlatforms(t *testing.T) {
	data, err := os.ReadFile(".goreleaser.yaml")
	if err != nil {
		t.Fatalf("reading .goreleaser.yaml: %v", err)
	}
	content := string(data)

	section := buildsSectionRe.FindStringSubmatch(content)
	if section == nil {
		t.Fatalf("could not find a builds: section in .goreleaser.yaml")
	}

	// Scoping to the builds: section, then pairing goos/goarch within each
	// individual "- id: ..." entry (rather than zipping two file-wide regex
	// scans by list index) means reordering entries, or a future bracketed
	// goos:/goarch: appearing elsewhere in the file (e.g. under archives:),
	// can't produce a mismatched pairing.
	entries := buildEntryRe.Split(section[1], -1)
	got := make(map[string]bool, len(entries))
	for _, entry := range entries {
		if entry == "" {
			continue
		}
		goosMatch := goosRe.FindStringSubmatch(entry)
		goarchMatch := goarchRe.FindStringSubmatch(entry)
		if goosMatch == nil || goarchMatch == nil {
			t.Fatalf("build entry is missing a goos:/goarch: pair:\n%s", entry)
		}
		got[goosMatch[1]+"/"+goarchMatch[1]] = true
	}

	want := map[string]bool{
		"linux/amd64":   true,
		"linux/arm64":   true,
		"linux/arm":     true,
		"windows/amd64": true,
	}

	if len(got) != len(want) {
		t.Fatalf("expected exactly %d supported build targets in .goreleaser.yaml, found %d: %v", len(want), len(got), got)
	}
	for target := range want {
		if !got[target] {
			t.Errorf("supported platform %q is missing from .goreleaser.yaml", target)
		}
	}
	for target := range got {
		if !want[target] {
			t.Errorf("target %q in .goreleaser.yaml is not one of the four supported platforms — adding or removing a release target is a deliberate change to the supported-platform set, not a routine .goreleaser.yaml edit", target)
		}
	}
}
