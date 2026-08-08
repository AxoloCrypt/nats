package main

import (
	"bytes"
	"regexp"
	"testing"
)

func TestVersionCommand_RegisteredOnRoot(t *testing.T) {
	found := false
	for _, cmd := range rootCmd.Commands() {
		if cmd == versionCmd {
			found = true
		}
	}
	if !found {
		t.Fatal("expected versionCmd to be registered on rootCmd alongside scanCmd and bleCmd")
	}
}

// TestVersionCommand_PrintsToStdoutOnly pins the split that makes `nats
// version > version.txt` useful: the version string is this command's output
// and belongs on stdout, and nothing incidental may leak onto stderr — the
// same stdout/stderr discipline runScan and runBLEScan keep for their reports.
func TestVersionCommand_PrintsToStdoutOnly(t *testing.T) {
	withOverriddenWriters(t, func(progress, diagnostic, report *bytes.Buffer) {
		versionCmd.Run(versionCmd, nil)

		want := "nats " + versionString() + "\n"
		if report.String() != want {
			t.Fatalf("expected stdout %q, got %q", want, report.String())
		}
		if progress.Len() != 0 || diagnostic.Len() != 0 {
			t.Fatalf("expected nothing on stderr, got progress %q and diagnostic %q", progress.String(), diagnostic.String())
		}
	})
}

// TestVersionString_NormalizesLeadingV covers both spellings the version
// variable can legitimately arrive in: the bare "0.1.0" GoReleaser injects
// (its {{.Version}} strips the tag's "v") and a hand-passed "v0.1.0". Both
// must render identically, so that whether a binary was built by the release
// pipeline or by hand can never change the shape of what it reports.
func TestVersionString_NormalizesLeadingV(t *testing.T) {
	origVersion := version
	defer func() { version = origVersion }()

	tests := []struct {
		name     string
		injected string
		want     string
	}{
		{"goreleaser form, no leading v", "0.1.0", "v0.1.0"},
		{"tag form, leading v", "v0.1.0", "v0.1.0"},
		{"prerelease suffix preserved", "1.2.0-rc.1", "v1.2.0-rc.1"},
		{"surrounding whitespace trimmed", "  0.1.0  ", "v0.1.0"},
		{"empty falls back to unknown", "", unknownVersion},
		{"whitespace-only falls back to unknown", "   ", unknownVersion},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			version = tt.injected
			if got := versionString(); got != tt.want {
				t.Fatalf("versionString() with version %q = %q, want %q", tt.injected, got, tt.want)
			}
		})
	}
}

// TestDefaultVersionIsTagShaped guards the compiled-in fallback rather than
// its exact value: pinning the literal would force this test to be edited on
// every release, but leaving it unchecked lets a malformed value (a stray
// "vv0.1.0", a branch name, an empty string) ship in every locally built
// binary — the one build path GoReleaser's injection never covers.
func TestDefaultVersionIsTagShaped(t *testing.T) {
	if !regexp.MustCompile(`^v?\d+\.\d+\.\d+(-[0-9A-Za-z.-]+)?$`).MatchString(version) {
		t.Fatalf("compiled-in default version %q is not MAJOR.MINOR.PATCH shaped (optionally v-prefixed, optionally with a prerelease suffix)", version)
	}
}
