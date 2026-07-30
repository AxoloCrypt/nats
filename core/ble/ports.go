package ble

import (
	"context"
	"time"
)

// BLEScanner is the pinned driven-adapter interface (spine AD-2), copied
// exactly — it also binds the future Android adapter (AD-3).
type BLEScanner interface {
	Probe() (ok bool, reason string)
	Scan(ctx context.Context, window time.Duration) (<-chan Advertisement, error)
}
