package ble

import (
	"context"
	"time"
)

// BLEScanner is the pinned driven-adapter interface (spine AD-2), copied
// exactly — it also binds the future Android adapter (AD-3).
// The method signatures are pinned; the obligations documented on them are
// what every implementation (including the deferred Android bridge) has to
// honour for NL-FR-13's "a skipped piece is always named explicitly in the
// warning, never silently dropped" to hold end to end.
type BLEScanner interface {
	// Probe reports whether a scan can run. On ok=false, reason must be a
	// non-empty, human-readable diagnosis naming why — core/ble passes it
	// through verbatim into the warning Diagnostic the user actually reads,
	// so an empty or whitespace-only reason produces a warning that names
	// what was skipped but never why. core/ble substitutes a generic
	// fallback in that case rather than printing a blank reason, but that is
	// a backstop for a broken implementation, not a licence to omit it.
	Probe() (ok bool, reason string)

	// Scan streams advertisements until window elapses or ctx is cancelled.
	// A scan that cannot start must be reported as a non-nil error here,
	// never as an immediately-closed channel: given only an empty channel,
	// core/ble cannot tell "the scan was refused" from "nothing was nearby",
	// and those mean opposite things to a user. Returning a nil channel with
	// a nil error is not permitted — core/ble would range over it forever.
	Scan(ctx context.Context, window time.Duration) (<-chan Advertisement, error)
}

// Writer renders a final Report as bytes for one output format (spine AD-11)
// — same shape as base spine's engine.Writer (AD-7), but its own
// independently-defined type: core/ble must never import core/engine
// (NL-AD-1's import-boundary rule, unchanged since Story 4.1). Each
// implementation (report/ble/table, report/ble/json, report/ble/markdown,
// report/ble/plain) consumes only the Report — never core/ble's live scan
// state — so a Writer stays decoupled from how a scan produced the devices
// it renders.
type Writer interface {
	Name() string
	Write(Report) ([]byte, error)
}
