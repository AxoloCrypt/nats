package ble

import (
	"context"
	"time"
)

// BLEScanner is the driven-adapter interface every platform scanner
// implements, including the deferred Android bridge.
//
// The method signatures are pinned; the obligations documented on them are
// what every implementation has to honour for "a skipped piece is always
// named explicitly in the warning, never silently dropped" to hold end to
// end.
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

// Writer renders a final Report as bytes for one output format — same shape
// as the LAN vertical's engine.Writer, but its own independently-defined
// type: core/ble must never import core/engine (the import-boundary rule
// enforced by import_test.go). Each
// implementation (report/ble/table, report/ble/json, report/ble/markdown,
// report/ble/plain) consumes only the Report — never core/ble's live scan
// state — so a Writer stays decoupled from how a scan produced the devices
// it renders.
type Writer interface {
	Name() string
	Write(Report) ([]byte, error)
}
