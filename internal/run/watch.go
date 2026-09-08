package run

import (
	"context"
	"errors"
	"io"
	"time"

	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/statestore"
	"github.com/herbygillot/dockhand/internal/verify"
)

// AwaitRecord watches the STORE until the attempt is terminal or ctx
// ends, and returns what it read last. It polls no provider — R10: a
// waiting client under a resident dispatcher is a watcher, and the
// record is what it watches. It replaces Await, which polled the
// provider and was therefore a second observer of a job somebody else
// was judging. A caller with NO dispatcher resident does not call this;
// it loops Finish over its own attempt and IS the judge — app decides
// which by app.Residency, re-read each iteration so a dispatcher that
// appears mid-wait takes over. Expiry (ctx) detaches and never fails: a
// timeout is the caller giving up on watching, never a verdict.
//
// IT READS BEFORE IT WAITS, so a caller that starts watching an attempt
// somebody has already judged returns at once rather than after one
// tick. That is the ordinary case under a resident dispatcher and it
// costs one read either way.
//
// AN ATTEMPT THAT VANISHES ends the watch with ErrNoChange rather than
// with a timeout. A record dropped under a watcher — a discard, a
// recreated state ref — is a real end to the thing being watched, and
// waiting out the caller's whole duration for it would report a
// still-running build.
func AwaitRecord(ctx context.Context, st *statestore.Store, attempt string, every time.Duration) (record.Attempt, error) {
	if every <= 0 {
		every = defaultWatch
	}
	t := time.NewTicker(every)
	defer t.Stop()
	var last record.Attempt
	for {
		s, err := st.Read(ctx)
		switch {
		case ctx.Err() != nil:
			// The caller gave up while the read was in flight. A read that
			// died with the context is the detach and not a failure of the
			// store, and reporting it as one would tell a person their
			// build had broken because they pressed Ctrl-C.
			return last, nil
		case err != nil && !errors.Is(err, statestore.ErrNoState):
			return last, err
		}
		a, ok := s.Attempts[attempt]
		switch {
		case !ok && last.ID != "":
			return last, ErrNoChange
		case ok:
			last = a
			if a.Settled() {
				return a, nil
			}
		}
		select {
		case <-ctx.Done():
			// Detached. The build outlives us by design, and the verdict
			// still arrives in the record for whoever reads it next — so
			// this is the caller's own expiry and never an error about the
			// attempt.
			return last, nil
		case <-t.C:
		}
	}
}

// defaultWatch is how often the store is re-read for a caller that named
// no cadence. It is deliberately slower than a provider poll would be: a
// read is a git cat-file batch over the whole state ref, so the watcher
// pays for every record in the tree on every tick, and a person waiting
// on a build that runs for minutes is not served by a tighter loop.
const defaultWatch = 2 * time.Second

// Follow copies the provider's live log to w until the job ends or ctx
// does, through verify.Streamer, and JUDGES NOTHING: the verdict still
// arrives through the record, watched by AwaitRecord or produced by
// Finish. On a provider that cannot stream it returns
// verify.ErrUnsupported and the caller says so, falling back to
// `dockhand log` once the record settles. It is --trace's whole
// implementation, on verify and on log, and the reason --trace could
// leave the bump family (R11): watching is a separate road from asking.
//
// IT DOES NOT POLL BETWEEN CHUNKS. A draft polled Status "to know when
// to stop", which made every tracing client a second observer of a job
// somebody else was judging; the stream's own EOF is when to stop.
//
// It takes the LEASE for Observe's reason: a verify.Job is addressed by
// provider and id, and record.Attempt.Lease is the request token the
// store keys the lease document under.
func Follow(ctx context.Context, prov verify.Verifier, l record.Lease, w io.Writer) error {
	s, ok := prov.(verify.Streamer)
	if !ok {
		return verify.ErrUnsupported
	}
	job := verify.Job{Provider: l.ID.Provider, ID: l.ID.ID, Started: l.ID.Started, Request: l.Request}
	return s.Stream(ctx, job, w)
}
