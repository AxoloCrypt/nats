package mdns

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"nats/core/engine"

	"github.com/hashicorp/mdns"
)

func TestTechnique_Name(t *testing.T) {
	tech := &technique{}
	if tech.Name() != "mdns" {
		t.Fatalf("expected 'mdns', got %s", tech.Name())
	}
}

func TestTechnique_RequiresPrivilege(t *testing.T) {
	orig := probePrivilege
	defer func() { probePrivilege = orig }()

	probePrivilege = func() (bool, error) { return false, nil }
	tech := &technique{}
	if tech.RequiresPrivilege() {
		t.Fatal("expected RequiresPrivilege to return false from mocked probe")
	}
}

func TestTechnique_RequiresPrivilege_ReturnsTrueOnFailure(t *testing.T) {
	orig := probePrivilege
	defer func() { probePrivilege = orig }()

	probePrivilege = func() (bool, error) { return true, errors.New("boom") }
	tech := &technique{}
	if !tech.RequiresPrivilege() {
		t.Fatal("expected RequiresPrivilege to return true from mocked probe")
	}
}

func TestTechnique_ProbePrivilege_ReturnsUnderlyingError(t *testing.T) {
	orig := probePrivilege
	defer func() { probePrivilege = orig }()

	probeErr := errors.New("network unreachable")
	probePrivilege = func() (bool, error) { return true, probeErr }

	tech := &technique{}
	requires, err := tech.ProbePrivilege()
	if !requires {
		t.Fatal("expected ProbePrivilege to report requiring privilege")
	}
	if err != probeErr {
		t.Fatalf("expected the underlying probe error to be returned, got %v", err)
	}
}

func TestTechnique_SelfRegistration(t *testing.T) {
	tech, ok := engine.GetTechnique("mdns")
	if !ok {
		t.Fatal("expected mdns technique to be registered in engine registry")
	}
	if tech.Name() != "mdns" {
		t.Fatalf("expected registered technique Name() to return 'mdns', got %s", tech.Name())
	}
}

// withFastQuiescenceVars overrides roundInterval/quiescenceWindow/queryFn for
// the duration of a test and restores the originals on cleanup.
func withFastQuiescenceVars(t *testing.T, round, quiescence time.Duration, query func(ctx context.Context, params *mdns.QueryParam) error) {
	t.Helper()

	origRound := roundInterval
	origQuiescence := quiescenceWindow
	origQuery := queryFn

	t.Cleanup(func() {
		roundInterval = origRound
		quiescenceWindow = origQuiescence
		queryFn = origQuery
	})

	roundInterval = round
	quiescenceWindow = quiescence
	queryFn = query
}

func TestTechnique_Run_ClosesAfterQuiescenceWindowWithNoSightings(t *testing.T) {
	var rounds int32
	withFastQuiescenceVars(t, 20*time.Millisecond, 60*time.Millisecond,
		func(ctx context.Context, params *mdns.QueryParam) error {
			atomic.AddInt32(&rounds, 1)
			return nil
		})

	tech := &technique{}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	ch, err := tech.Run(ctx, "")
	if err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}

	var sightings []engine.Sighting
	done := make(chan struct{})
	go func() {
		for s := range ch {
			sightings = append(sightings, s)
		}
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("channel did not close within the expected quiescence window")
	}

	if len(sightings) != 0 {
		t.Fatalf("expected no sightings, got %d", len(sightings))
	}

	// quiescenceWindow (60ms) is an exact multiple (3x) of roundInterval
	// (20ms): rounds run at t=0,20,40,60ms (the ticker's 3rd tick lands
	// exactly on the quiescence deadline, and -- since the deadline check
	// runs after the round rather than gating it -- the round due exactly at
	// the deadline still runs before Run recognizes quiescence and returns).
	// The count that matters here is that it is always the same across many
	// repeated runs (verified separately under -count and -race): a genuine
	// regression would show up as this test flaking between two counts, not
	// as a wrong-but-stable count.
	if got := atomic.LoadInt32(&rounds); got != 4 {
		t.Fatalf("expected exactly 4 query rounds before closing (deterministic given quiescenceWindow is an exact multiple of roundInterval), got %d", got)
	}
}

func TestTechnique_Run_KeepsListeningPastSightingsAndClosesQuiescenceWindowAfterLast(t *testing.T) {
	var round int32
	withFastQuiescenceVars(t, 20*time.Millisecond, 60*time.Millisecond,
		func(ctx context.Context, params *mdns.QueryParam) error {
			n := atomic.AddInt32(&round, 1)
			// Only the first two rounds produce a sighting; every round
			// after that is silent, so quiescence must be measured from the
			// last sighting, not from Run's start.
			if n <= 2 {
				entry := &mdns.ServiceEntry{
					AddrV4: net.ParseIP(fmt.Sprintf("192.168.1.%d", n)),
				}
				select {
				case params.Entries <- entry:
				case <-ctx.Done():
				}
			}
			return nil
		})

	tech := &technique{}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	start := time.Now()
	ch, err := tech.Run(ctx, "")
	if err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}

	var sightings []engine.Sighting
	for s := range ch {
		sightings = append(sightings, s)
	}
	elapsed := time.Since(start)

	if len(sightings) != 2 {
		t.Fatalf("expected 2 sightings forwarded, got %d: %+v", len(sightings), sightings)
	}

	// The 2nd (last) sighting happens in the round starting at ~roundInterval
	// (20ms); the channel must stay open roughly quiescenceWindow (60ms)
	// past that point -- well beyond a single roundInterval -- rather than
	// closing as soon as sightings stop.
	minExpected := roundInterval + quiescenceWindow
	if elapsed < minExpected {
		t.Fatalf("channel closed too early: elapsed %v, expected at least %v (idle timer must reset on each sighting)", elapsed, minExpected)
	}
	maxExpected := minExpected + 2*time.Second
	if elapsed > maxExpected {
		t.Fatalf("channel took too long to close: elapsed %v, expected at most %v", elapsed, maxExpected)
	}
}

// TestTechnique_Run_BlockingRoundsStillGetSeveralAttempts guards against a
// regression where the shipped defaults collapse to a single query attempt:
// a fake queryFn that actually blocks for ~roundInterval (unlike the instant
// fakes above, which don't exercise how a real production round -- whose own
// query blocks for close to roundInterval -- interacts with the pacing
// between rounds) must still get several rounds within quiescenceWindow, the
// same ratio the shipped defaults use (quiescenceWindow = 5x roundInterval).
func TestTechnique_Run_BlockingRoundsStillGetSeveralAttempts(t *testing.T) {
	var rounds int32
	round := 20 * time.Millisecond
	withFastQuiescenceVars(t, round, 5*round, // same 5:1 ratio as the shipped defaults
		func(ctx context.Context, params *mdns.QueryParam) error {
			atomic.AddInt32(&rounds, 1)
			time.Sleep(round) // simulates a real query blocking for ~roundInterval
			return nil
		})

	tech := &technique{}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	ch, err := tech.Run(ctx, "")
	if err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}

	done := make(chan struct{})
	go func() {
		for range ch {
		}
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("channel did not close within the expected quiescence window")
	}

	if got := atomic.LoadInt32(&rounds); got < 2 {
		t.Fatalf("expected several query rounds when each round blocks for ~roundInterval like a real query does, got only %d (this is the exact shape of a prior regression that collapsed SSDP's shipped defaults to a single search attempt)", got)
	}
}
