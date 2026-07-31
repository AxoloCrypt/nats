package ble

import (
	"fmt"
	"math"
)

// Neither the PRD nor the architecture spine pins exact log-distance
// path-loss constants (FR-6 marks the TX-Power fallback itself
// [ASSUMPTION]) — these are a defensible, commonly-used starting point for
// indoor/BLE beacon-style estimation, not a value reverse-engineered from a
// spec:
//   - defaultMeasuredPower: the calibrated RSSI (dBm) expected at 1 meter,
//     used only when an Advertisement doesn't broadcast its own TX Power.
//     -59 dBm matches the default calibration constant popularized by
//     Apple's iBeacon spec.
//   - pathLossExponent: how quickly signal strength decays with distance;
//     2.0 sits at the low (free-space-like) end of the 2.0-2.5 range
//     typical for indoor BLE beacon estimation.
//   - knownTXUncertaintyFactor / assumedTXUncertaintyFactor: the
//     proportional uncertainty band applied to the estimated distance —
//     wider when TX Power had to be assumed rather than read from the
//     Advertisement. Proportional (not a fixed absolute meter figure) so
//     the band stays honest at both very close and very far range.
const (
	defaultMeasuredPower       = -59.0
	pathLossExponent           = 2.0
	knownTXUncertaintyFactor   = 0.4
	assumedTXUncertaintyFactor = 0.7
)

// EstimateDistance is the sole log-distance path-loss implementation in the
// codebase (spine AD-5) — nothing in discovery/blescan computes distance
// itself. It always returns a combined (meters, uncertainty) pair; meters
// and uncertainty are both strictly positive for any finite rssi, so
// uncertainty is never 0 (PRD counter-metric SM-C1 warns against narrowing
// the band just to look more precise).
func EstimateDistance(rssi int, txPower *int) (meters float64, uncertainty float64) {
	measuredPower := defaultMeasuredPower
	uncertaintyFactor := assumedTXUncertaintyFactor
	if txPower != nil {
		measuredPower = float64(*txPower)
		uncertaintyFactor = knownTXUncertaintyFactor
	}

	meters = math.Pow(10, (measuredPower-float64(rssi))/(10*pathLossExponent))
	uncertainty = meters * uncertaintyFactor
	return meters, uncertainty
}

// FormatDistance combines EstimateDistance's (meters, uncertainty) pair into
// the single rendered string spine's Structural Seed pins for
// BLEDeviceProfile.DistanceEstimate (e.g. "~3.0m (±2.0m)") — no Writer may
// emit meters without its paired uncertainty.
//
// Sub-1m values use two decimal places rather than one: at one decimal,
// close-range readings (a device a few cm–1m from the scanner, a common
// case) round down to a displayed "0.0", which is exactly the
// suspiciously-tight/zero-uncertainty failure mode PRD SM-C1 warns against
// — the underlying float is never 0, but the rendered string was.
func FormatDistance(meters, uncertainty float64) string {
	if meters < 1 {
		return fmt.Sprintf("~%.2fm (±%.2fm)", meters, uncertainty)
	}
	return fmt.Sprintf("~%.1fm (±%.1fm)", meters, uncertainty)
}
