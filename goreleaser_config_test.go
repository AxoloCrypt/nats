// Package release holds repo-level meta-tests for the release pipeline
// (Story 3.3) that don't belong to any single Go package.
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

// TestGoReleaserConfigTargetsMatchAD14 fails CI if .goreleaser.yaml's build
// matrix ever drifts from the four platforms AD-14 fixes as a hard
// architecture invariant: linux/amd64, linux/arm64, linux/arm,
// windows/amd64. Per AD-14 and this story's Dev Notes, removing (or
// silently adding) a release target is a spine-level decision, not
// something a routine .goreleaser.yaml edit should be able to slip past CI.
//
// The two darwin targets this test previously required were removed on
// 2026-08-07 by exactly the kind of explicit spine update AD-14 demands
// (Sprint Change Proposal "Drop macOS as a Supported Platform"), so this
// want-set was updated in the same commit as .goreleaser.yaml — the test
// and the config it guards must never move separately, or CI breaks.
func TestGoReleaserConfigTargetsMatchAD14(t *testing.T) {
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
		t.Fatalf("expected exactly %d AD-14 build targets in .goreleaser.yaml, found %d: %v", len(want), len(got), got)
	}
	for target := range want {
		if !got[target] {
			t.Errorf("AD-14 requires target %q in .goreleaser.yaml, but it's missing", target)
		}
	}
	for target := range got {
		if !want[target] {
			t.Errorf("target %q in .goreleaser.yaml is not one of the four AD-14 targets — adding or removing a release target requires an ARCHITECTURE-SPINE.md#AD-14 update, not a routine .goreleaser.yaml edit", target)
		}
	}
}
