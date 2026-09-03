package courtesy

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"sync"
	"time"

	"github.com/herbygillot/dockhand/internal/exitcode"
)

// Policy is how hard dockhand is willing to lean on one host.
//
// The numbers are per host and not per sweep, which is the whole
// design: a sweep that touched a thousand different forges would be
// fine at any rate, and a sweep that touches github.com nine hundred
// times is the one that needs bounding. Two limits rather than one
// because they bound different things — the interval bounds the rate,
// the ceiling bounds the burst — and whichever binds first, binds.
type Policy struct {
	// Interval is the minimum gap between two requests to one host.
	Interval time.Duration
	// Jitter is the fraction of Interval added at random to each gap,
	// 0 for none. A perfectly regular request train is itself a bot
	// signature, and it is also what makes a retry storm synchronize.
	Jitter float64
	// Ceiling is how many requests may be in flight to one host at
	// once. Zero means one.
	Ceiling int
	// Backoff is how long a host that refused dockhand is left
	// entirely alone. Zero means no wall is ever raised, which is
	// wrong for anything that talks to a real forge and is the right
	// default for a test.
	Backoff time.Duration
	// Strikes is how many consecutive failures wall a host that never
	// said anything recognizable as a refusal. Zero means a host is
	// walled only by its own words.
	//
	// It exists because the words are not always there and the damage
	// is worst when they are not. A forge that is simply unreachable —
	// DNS, a captive network, an outage — matches no refusal phrase, so
	// nothing walls it; and for the staged observer an unanswered cheap
	// witness promotes every port behind it to a full-cost candidate,
	// which turns a sweep from one ls-remote per port into one livecheck
	// per port at the moment the forge stopped working. A host that has
	// failed three times in a row is a host to leave alone whatever it
	// said.
	Strikes int
}

// Default is the policy a sweep uses when nobody says otherwise.
//
// Two requests a second to one host, at most two at a time, is slower
// than any of these services would refuse and slower than dockhand
// could go — which is the point. A thousand-port sweep is a background
// errand, and the cost of being twice as polite as necessary is that
// it finishes in ten minutes rather than five. The cost of being half
// as polite as necessary is an IP address that cannot clone anything
// for an hour.
//
// The quarter-hour wall is sized to GitHub's own secondary-limit
// advice, which asks a client that has been refused to wait at least a
// minute and to stop making the request that was refused. Fifteen is
// generous on purpose: a sweep that hit a wall has thousands of other
// ports to get on with, so waiting costs it nothing, and a client that
// comes straight back is the one that gets a longer ban.
// Three strikes is the pool's own number for the same judgment: a run
// of three consecutive failures is what tells a thing that has stopped
// working from a run of unlucky ones.
var Default = Policy{
	Interval: 500 * time.Millisecond,
	Jitter:   0.4,
	Ceiling:  2,
	Backoff:  15 * time.Minute,
	Strikes:  3,
}

// ErrWalled reports a host dockhand has stopped asking: it refused us,
// and the backoff has not expired. It is the sentinel behind
// WalledError, so a caller can ask errors.Is without knowing the type.
var ErrWalled = errors.New("courtesy: host walled off")

// WalledError is a witness dockhand declined to consult because the
// host it lives on has refused us recently.
//
// It is deliberately not a failure of the port it was raised about. A
// forge that rate-limited a sweep has said nothing whatever about the
// four hundredth port, and a row that called it broken would send
// somebody to read a Portfile that is fine. It is upstream's band, the
// one for a service that ran and refused, and the remedy is always the
// same: ask again later.
type WalledError struct {
	// Host is the budget the wall is on.
	Host string
	// Retry is how long is left of the backoff.
	Retry time.Duration
	// Cause is what the host said, when a caller recorded one.
	Cause error
}

func (e *WalledError) Error() string {
	msg := fmt.Sprintf("%s refused dockhand; not asking again for %s", e.Host, e.Retry.Round(time.Second))
	if e.Cause != nil {
		return msg + ": " + e.Cause.Error()
	}
	return msg
}

// Unwrap exposes both the sentinel and whatever the host said, so
// errors.Is answers for either.
func (e *WalledError) Unwrap() []error {
	if e.Cause == nil {
		return []error{ErrWalled}
	}
	return []error{ErrWalled, e.Cause}
}

// DockhandExit: the upstream band, for a service that ran and refused.
func (e *WalledError) DockhandExit() int { return exitcode.WitnessAPI }

// Code names the outcome for a machine.
func (e *WalledError) Code() string { return "witness-walled" }

// Pacer schedules requests so that no one host is asked too often, too
// many at once, or at all while it is refusing us.
//
// The queue depth is the property that makes this safe to put under a
// per-target timeout, and it is worth stating because it is not
// obvious. Only a worker that is actively running a target ever enters
// the pacer, so at most one request per sweep worker is ever waiting —
// eight, on an eight-way pool, not nine hundred. A target therefore
// waits at most (workers-1) intervals for its turn, a few seconds, not
// the whole sweep's duration. The interval sets how long the SWEEP
// takes; it does not stretch any single target's clock.
type Pacer struct {
	pol   Policy
	clock Clock
	// rnd draws the jitter fraction in [0,1). A field rather than a
	// call to math/rand so a test can make the spacing exact; nothing
	// outside this package sets it.
	rnd func() float64

	mu    sync.Mutex
	hosts map[string]*bucket
}

// bucket is one host's budget.
type bucket struct {
	// next is when this host may be asked again. It is stamped
	// forward by each reservation, so requests take their turns in
	// arrival order without anybody holding a lock while waiting.
	next time.Time
	// until is when the wall comes down; zero for a host that has not
	// refused us.
	until time.Time
	// cause is what the host said when it refused, carried so that
	// every port refused behind the wall can quote the one refusal
	// that raised it rather than reporting a bare timeout.
	cause error
	// strikes is how many requests to this host have failed in a row
	// for a reason nobody could read as a refusal. Reset by any
	// request that succeeds, and by the wall it raises.
	strikes int
	// slots is the ceiling, as a counting semaphore.
	slots chan struct{}
}

// NewPacer builds a pacer over a policy. A nil clock is the real one.
func NewPacer(pol Policy, clock Clock) *Pacer {
	if clock == nil {
		clock = RealClock{}
	}
	if pol.Ceiling < 1 {
		pol.Ceiling = 1
	}
	if pol.Jitter < 0 {
		pol.Jitter = 0
	}
	return &Pacer{pol: pol, clock: clock, rnd: rand.Float64, hosts: map[string]*bucket{}}
}

// Ask runs f once, when the host's budget allows it.
//
// The order is deliberate: the ceiling is taken first and the interval
// reserved inside it, so a request that never runs — because the
// context ended while it queued — does not consume a time slot that
// some other request could have used. It also keeps the host's clock
// from running ahead of reality: next is only ever stamped forward by
// a request that is about to happen.
//
// A walled host is refused here rather than in f. That is the whole
// point of the wall — the request must not be made — and it is why the
// refusal is an error a caller can recognize instead of a silent skip.
func (p *Pacer) Ask(ctx context.Context, host string, f func(context.Context) error) error {
	if err := p.wall(host); err != nil {
		return err
	}
	b := p.bucket(host)
	select {
	case b.slots <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	}
	defer func() { <-b.slots }()

	if err := p.clock.Sleep(ctx, p.reserve(b)); err != nil {
		return err
	}
	// Checked twice on purpose: a host may have been walled by another
	// worker while this one queued for its slot, and asking anyway
	// would be the one request the wall exists to prevent.
	if err := p.wall(host); err != nil {
		return err
	}
	return f(ctx)
}

// Wall records that a host refused dockhand — a rate limit, an abuse
// warning, an authentication demand — so that nothing asks it again
// until the backoff expires.
//
// It is the caller's to raise because only the caller can read the
// refusal: a 429 in a JSON body, a git error mentioning a limit, an
// HTTP status the transport saw. The pacer's job is to remember it and
// to apply it to every other port on the same host, which is the part
// no single call site can do for itself.
func (p *Pacer) Wall(host string, cause error) {
	if p.pol.Backoff <= 0 {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.wallLocked(host, cause)
}

// Struck records a request that failed for a reason nobody could read
// as a refusal, and raises the wall once a host has failed Strikes
// times in a row.
//
// It is the answer to the failure a forge does not word: an outage, a
// DNS that stopped resolving, a network that went away. None of them
// says "rate limit" and all of them are hosts to stop asking, and the
// walled row a port behind one gets is already the right state for it —
// not the port's fault, not the sweep's, and fixed by running again
// later.
//
// An interrupted request is not a strike, and that is the whole of what
// the caller must keep out of here: a context that ended is this run's
// clock running out, and it says nothing about the host.
func (p *Pacer) Struck(host string, cause error) {
	if p.pol.Strikes <= 0 || p.pol.Backoff <= 0 {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	b := p.bucketLocked(host)
	b.strikes++
	if b.strikes < p.pol.Strikes {
		return
	}
	n := b.strikes
	p.wallLocked(host, fmt.Errorf("%d requests in a row failed, the last: %w", n, cause))
}

// Cleared records a request that worked, so that a host is walled by a
// run of failures and never by an accumulation of scattered ones.
func (p *Pacer) Cleared(host string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if b, ok := p.hosts[host]; ok {
		b.strikes = 0
	}
}

// wallLocked raises the wall and clears the streak that may have raised
// it, so a host that comes back after the backoff starts from zero.
func (p *Pacer) wallLocked(host string, cause error) {
	b := p.bucketLocked(host)
	b.until = p.clock.Now().Add(p.pol.Backoff)
	b.cause = cause
	b.strikes = 0
}

// Walled reports how long a host is being left alone, and false when
// it is not.
func (p *Pacer) Walled(host string) (time.Duration, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	b, ok := p.hosts[host]
	if !ok || b.until.IsZero() {
		return 0, false
	}
	left := b.until.Sub(p.clock.Now())
	if left <= 0 {
		return 0, false
	}
	return left, true
}

// wall returns the refusal for a walled host, nil for one that may be
// asked.
func (p *Pacer) wall(host string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	b, ok := p.hosts[host]
	if !ok || b.until.IsZero() {
		return nil
	}
	left := b.until.Sub(p.clock.Now())
	if left <= 0 {
		// The wall has come down. Clearing it here rather than on a
		// timer is what keeps the pacer free of goroutines it would
		// have to shut down.
		b.until, b.cause = time.Time{}, nil
		return nil
	}
	return &WalledError{Host: host, Retry: left, Cause: b.cause}
}

// reserve claims this request's turn and returns how long to wait for
// it. The bucket's clock is stamped forward immediately, so the next
// caller queues behind this one instead of racing it, and nobody holds
// a lock while sleeping.
func (p *Pacer) reserve(b *bucket) time.Duration {
	p.mu.Lock()
	defer p.mu.Unlock()
	now := p.clock.Now()
	if b.next.Before(now) {
		b.next = now
	}
	at := b.next
	gap := p.pol.Interval
	if p.pol.Jitter > 0 {
		gap += time.Duration(float64(p.pol.Interval) * p.pol.Jitter * p.rnd())
	}
	b.next = at.Add(gap)
	return at.Sub(now)
}

func (p *Pacer) bucket(host string) *bucket {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.bucketLocked(host)
}

func (p *Pacer) bucketLocked(host string) *bucket {
	b, ok := p.hosts[host]
	if !ok {
		b = &bucket{slots: make(chan struct{}, p.pol.Ceiling)}
		p.hosts[host] = b
	}
	return b
}
