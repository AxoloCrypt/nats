package ssdp

import (
	"context"
	"net"
	"net/url"
	"time"

	"nats/core/engine"

	koron "github.com/koron/go-ssdp"
)

func init() {
	engine.RegisterTechnique(&technique{})
}

type technique struct{}

func (t *technique) Name() string {
	return "ssdp"
}

var probePrivilege = func() (bool, error) {
	conn, err := net.ListenMulticastUDP("udp4", nil, &net.UDPAddr{IP: net.IPv4(239, 255, 255, 250), Port: 1900})
	if err != nil {
		return true, err
	}
	conn.Close()
	return false, nil
}

// searchFn wraps koron.Search so Run's round-by-round behavior can be
// unit-tested against a fake instead of real multicast.
var searchFn = koron.Search

// roundInterval paces search rounds and (floored at 1s) sizes searchFn's own
// wait-seconds argument -- see the ticker/waitSec comments in Run.
var roundInterval = 5 * time.Second

// quiescenceWindow is how long Run waits without a new sighting before
// closing its own channel — this listen-based technique self-terminates on
// quiescence rather than on a fixed deadline. It is reset on every round
// that forwards at least one sighting.
var quiescenceWindow = 10 * time.Second

func (t *technique) RequiresPrivilege() bool {
	requires, _ := probePrivilege()
	return requires
}

// ProbePrivilege implements engine.PrivilegeProber, exposing the real
// underlying error instead of the generic "requires privilege" message
// RequiresPrivilege() alone can only imply.
func (t *technique) ProbePrivilege() (bool, error) {
	return probePrivilege()
}

func (t *technique) Run(ctx context.Context, target string) (<-chan engine.Sighting, error) {
	ch := make(chan engine.Sighting, 100)

	go func() {
		defer close(ch)

		// ticker paces rounds independently of how long each round's own
		// search takes. A production round already blocks for ~roundInterval
		// on its own (searchFn's waitSec below), so a *fresh* roundInterval
		// timer started only after the round returns would double the real
		// cadence to ~2x roundInterval -- invisible in tests, which use an
		// instant fake searchFn, but a real regression in production (with
		// the shipped 5s roundInterval / 10s quiescenceWindow defaults, it
		// collapsed SSDP to a single search attempt per scan). A concurrent
		// ticker (like a real clock, ticking regardless of what the round is
		// doing) overlaps with the round's own duration instead, matching
		// this technique's pre-quiescence pacing.
		ticker := time.NewTicker(roundInterval)
		defer ticker.Stop()

		// idleDeadline is the wall-clock time at which quiescence closes the
		// channel; it is pushed out by quiescenceWindow every time a round
		// forwards a sighting. It is checked as a plain time.Now() comparison
		// immediately after each round -- never raced against the ticker in
		// a select -- so there is no nondeterminism about whether an extra
		// round runs when quiescenceWindow lands on an exact multiple of
		// roundInterval (two independently-scheduled timer channels becoming
		// ready in the same instant would otherwise make a select's choice
		// between them unpredictable).
		idleDeadline := time.Now().Add(quiescenceWindow)

		// waitSec is searchFn's own wait-seconds argument (how long a single
		// search call blocks listening for responses), derived from
		// roundInterval but floored at 1 so a sub-second roundInterval (only
		// reachable via a misconfigured override, never a shipped default)
		// can't silently produce waitSec=0 and drop all responses that round.
		waitSec := int(roundInterval / time.Second)
		if waitSec < 1 {
			waitSec = 1
		}

		for {
			sawSighting := false

			// koron.Search blocks for up to waitSec regardless of ctx
			// cancellation mid-call (it takes no ctx at all); a cancellation
			// during this call is only observed once it returns.
			results, err := searchFn(koron.All, waitSec, "")
			if err == nil {
				for _, r := range results {
					serviceData := map[string]string{}
					if r.Type != "" {
						serviceData["type"] = r.Type
					}
					if r.USN != "" {
						serviceData["usn"] = r.USN
					}
					if r.Server != "" {
						serviceData["server"] = r.Server
					}
					if r.Location != "" {
						serviceData["location"] = r.Location
					}

					ip := extractIP(r.Location)
					if ip == "" {
						continue
					}

					select {
					case ch <- engine.Sighting{
						IP:          ip,
						MAC:         "",
						Technique:   "ssdp",
						ServiceData: serviceData,
					}:
						sawSighting = true
					case <-ctx.Done():
						return
					}
				}
			}

			if sawSighting {
				idleDeadline = time.Now().Add(quiescenceWindow)
			}

			if !time.Now().Before(idleDeadline) {
				return
			}

			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()

	return ch, nil
}

func extractIP(location string) string {
	if location == "" {
		return ""
	}
	u, err := url.Parse(location)
	if err != nil {
		return ""
	}
	host := u.Hostname()
	if host == "" {
		return ""
	}
	if ip := net.ParseIP(host); ip != nil {
		return host
	}
	addrs, err := net.LookupHost(host)
	if err != nil || len(addrs) == 0 {
		return ""
	}
	return addrs[0]
}
