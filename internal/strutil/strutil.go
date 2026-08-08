// Package strutil holds string helpers importable from any nats package —
// currently used by core/ble, cmd/cli, and report/ble/internal/blerender,
// and intended for reuse by core/engine and its siblings too — so a rule
// like "blank" gets one definition instead of being restated (and drifting)
// at each call site.
package strutil

import "strings"

// IsBlank reports whether s is empty or contains only whitespace.
//
// A whitespace-only string (e.g. " ", "\n") is treated as absent, not as a
// present-but-empty value: a bare s == "" check would miss it, silently
// treating "no name broadcast" and "name is one space" as different states
// when callers need them to be the same.
func IsBlank(s string) bool {
	return strings.TrimSpace(s) == ""
}
