package mdns

import (
	"context"
	"net"
	"time"

	"nats/core/engine"

	"github.com/hashicorp/mdns"
)

func init() {
	engine.RegisterTechnique(&technique{})
}

type technique struct{}

func (t *technique) Name() string {
	return "mdns"
}

var probePrivilege = func() (bool, error) {
	conn, err := net.ListenMulticastUDP("udp4", nil, &net.UDPAddr{IP: net.IPv4(224, 0, 0, 251), Port: 5353})
	if err != nil {
		return true, err
	}
	conn.Close()
	return false, nil
}

// queryFn wraps mdns.QueryContext so Run's round-by-round behavior can be
// unit-tested against a fake instead of real multicast.
var queryFn = mdns.QueryContext

// roundInterval bounds both the per-round query timeout and the maximum
// wait between the end of one round and the start of the next.
var roundInterval = 2 * time.Second

// quiescenceWindow is how long Run waits without a new sighting before
// closing its own channel (AD-13). It is reset on every round that forwards
// at least one sighting.
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
		// query takes. A production round already blocks for ~roundInterval
		// on its own (queryFn's Timeout below), so a *fresh* roundInterval
		// timer started only after the round returns would double the real
		// cadence to ~2x roundInterval -- invisible in tests, which use an
		// instant fake queryFn, but a real regression in production. A
		// concurrent ticker (like a real clock, ticking regardless of what
		// the round is doing) overlaps with the round's own duration instead,
		// matching this technique's pre-quiescence pacing.
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

		for {
			entries := make(chan *mdns.ServiceEntry, 100)
			forwarderDone := make(chan struct{})
			sawSighting := false

			go func() {
				defer close(forwarderDone)
				for entry := range entries {
					serviceData := map[string]string{}
					if entry.Host != "" {
						serviceData["hostname"] = entry.Host
					}
					if entry.Name != "" {
						serviceData["name"] = entry.Name
					}
					if entry.Info != "" {
						serviceData["info"] = entry.Info
					}

					if entry.AddrV4 == nil {
						continue
					}
					ip := entry.AddrV4.String()

					select {
					case ch <- engine.Sighting{
						IP:          ip,
						MAC:         "",
						Technique:   "mdns",
						ServiceData: serviceData,
					}:
						// Only read after forwarderDone closes below, which
						// happens-after every write in this goroutine -- safe
						// without an atomic/mutex despite being a plain bool.
						sawSighting = true
					case <-ctx.Done():
						return
					}
				}
			}()

			// mdns.QueryContext blocks for up to Timeout regardless of ctx
			// cancellation mid-call (it is not itself ctx-interruptible); a
			// cancellation during this call is only observed once it returns.
			params := &mdns.QueryParam{
				Domain:  "local",
				Entries: entries,
				Timeout: roundInterval,
			}
			_ = queryFn(ctx, params)

			close(entries)
			<-forwarderDone

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
