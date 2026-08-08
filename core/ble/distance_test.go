package ble

import (
	"math"
	"regexp"
	"testing"
)

func TestEstimateDistance_KnownTXPowerProducesPositiveFiniteResult(t *testing.T) {
	txPower := -59
	meters, uncertainty := EstimateDistance(-70, &txPower)

	if meters <= 0 || math.IsInf(meters, 0) || math.IsNaN(meters) {
		t.Fatalf("expected a positive, finite meters, got %v", meters)
	}
	if uncertainty <= 0 || math.IsInf(uncertainty, 0) || math.IsNaN(uncertainty) {
		t.Fatalf("expected a positive, finite uncertainty, got %v", uncertainty)
	}
}

func TestEstimateDistance_NilTXPowerProducesPositiveFiniteResult(t *testing.T) {
	meters, uncertainty := EstimateDistance(-70, nil)

	if meters <= 0 || math.IsInf(meters, 0) || math.IsNaN(meters) {
		t.Fatalf("expected a positive, finite meters, got %v", meters)
	}
	if uncertainty <= 0 || math.IsInf(uncertainty, 0) || math.IsNaN(uncertainty) {
		t.Fatalf("expected a positive, finite uncertainty, got %v", uncertainty)
	}
}

func TestEstimateDistance_NilTXPowerWidensUncertainty(t *testing.T) {
	txPower := -59
	const rssi = -70

	metersWithTXPower, withTXPower := EstimateDistance(rssi, &txPower)
	metersNoTXPower, withoutTXPower := EstimateDistance(rssi, nil)

	// Compare as a proportion of each call's own meters, since a nil
	// TXPower also changes the assumed measuredPower and therefore the
	// meters figure itself — what matters is a meaningfully wider
	// proportional *band*, not a larger absolute uncertainty number that
	// just follows from a larger distance.
	proportionWithTXPower := withTXPower / metersWithTXPower
	proportionWithoutTXPower := withoutTXPower / metersNoTXPower
	if proportionWithoutTXPower <= proportionWithTXPower {
		t.Fatalf("expected a nil TXPower to widen the proportional uncertainty band, got %v (with) vs %v (without)", proportionWithTXPower, proportionWithoutTXPower)
	}
}

func TestEstimateDistance_DistanceIncreasesAsRSSIWeakens(t *testing.T) {
	txPower := -59

	closeMeters, _ := EstimateDistance(-50, &txPower)
	farMeters, _ := EstimateDistance(-90, &txPower)

	if farMeters <= closeMeters {
		t.Fatalf("expected weaker RSSI to produce a larger distance, got close=%v far=%v", closeMeters, farMeters)
	}
}

func TestEstimateDistance_NeverZeroUncertainty(t *testing.T) {
	txPower := -59
	cases := []*int{&txPower, nil}
	for _, tx := range cases {
		for _, rssi := range []int{-30, -50, -70, -90, -110} {
			_, uncertainty := EstimateDistance(rssi, tx)
			if uncertainty == 0 {
				t.Fatalf("expected nonzero uncertainty for rssi=%d txPower=%v, got 0", rssi, tx)
			}
		}
	}
}

var distancePattern = regexp.MustCompile(`^~\d+(\.\d+)?m \(±\d+(\.\d+)?m\)$`)

func TestFormatDistance_MatchesExpectedShape(t *testing.T) {
	got := FormatDistance(3.0, 2.0)
	if !distancePattern.MatchString(got) {
		t.Fatalf("expected FormatDistance output to match %q, got %q", distancePattern.String(), got)
	}
}

func TestFormatDistance_CloseRangeNeverRendersAsZero(t *testing.T) {
	// Regression: at one decimal place, small-but-nonzero meters/uncertainty
	// (e.g. a device a few cm from the scanner) rounded down to a displayed
	// "0.0", exactly the zero-uncertainty reading the format exists to
	// avoid, even though the underlying float was never 0.
	got := FormatDistance(0.0355, 0.0142)
	if !distancePattern.MatchString(got) {
		t.Fatalf("FormatDistance(0.0355, 0.0142) = %q, expected to match %q", got, distancePattern.String())
	}
	if got == "~0.0m (±0.0m)" {
		t.Fatalf("FormatDistance(0.0355, 0.0142) rendered as a bare zero: %q", got)
	}
}

func TestFormatDistance_AlwaysIncludesBothFigures(t *testing.T) {
	cases := []struct {
		meters, uncertainty float64
	}{
		{0.4, 0.28},
		{3.0, 1.2},
		{15.7, 11.0},
	}
	for _, c := range cases {
		got := FormatDistance(c.meters, c.uncertainty)
		if !distancePattern.MatchString(got) {
			t.Fatalf("FormatDistance(%v, %v) = %q, expected to match %q", c.meters, c.uncertainty, got, distancePattern.String())
		}
	}
}
