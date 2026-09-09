package cli

import (
	"context"
	"time"

	"github.com/herbygillot/dockhand/internal/app"
	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/lockfile"
	"github.com/herbygillot/dockhand/internal/record"
)

// The two lockfiles the dispatch ruling names. BOTH LIVE IN THE COMMON
// GIT DIR and never in a worktree's own, because a linked worktree is
// the same checkout for every purpose dockhand has: two worktrees of one
// repository share a state ref, so two dispatchers over them would be
// two schedulers on one queue.
//
// They are FILES and not documents in the state ref, and that is the
// half of the ruling that is easy to get wrong. A crash releases a
// flock and does not release a record; a heartbeat written into git
// would make an idle dispatcher take the ledger flock forever and grow
// the store with a document whose only job is liveness.
const (
	// dispatchLock answers "is a scheduler active on this checkout". It
	// is held LOCK_EX for the dispatcher's whole life and PROBED with a
	// shared lock by every verb that wants to know whether anything will
	// finish what it starts.
	dispatchLock = ".dockhand-dispatch.lock"
	// passLock answers "a pass is in flight, and who holds it". It is
	// try-locked per pass and released across the sleep, which is
	// exactly why it cannot answer the first question — half of §11's
	// "the pass lock app.Cycle says it takes" was never the pass lock.
	passLock = ".dockhand-pass.lock"
)

// probeDeadline is how long a `dispatch` waits for the residency lock
// before it concludes somebody else is resident.
//
// It is not zero, and the reason is a race the shared probe introduces
// rather than removes: a prober holds LOCK_SH for the microseconds
// between its open and its unlock, and a dispatcher starting in that
// window would fail its LOCK_EX and exit 0 believing a scheduler was
// up, when the holder was a `status`. A short bounded retry costs a
// starting dispatcher nothing and makes "exits 0 naming the holder"
// something only a real dispatcher can cause.
const probeDeadline = 2 * time.Second

// lockPath is where one of the two locks lives for this repository.
func lockPath(ctx context.Context, repo *git.Repo, name string) (string, error) {
	return repo.CommonDirFile(ctx, name)
}

// probeResidency reads the dispatch lock and answers app.Residency —
// R10's chooser, and the input to every remedy line in the 60 band.
//
// THREE STATES, because the lock can be unreadable. A permission error
// or a filesystem with no flock is not "no dispatcher", and a process
// that assumed so would appoint itself judge beside a dispatcher it
// could not see. Under Unknown a --timeout watches only and says so, and
// Status settles nothing and says why (rule 7).
//
// The probe is a SHARED lock and never a try-lock; lockfile.Probe's own
// doc carries the defect that argument closes. Probers never exclude
// each other, so two verbs probing at once cannot each read the other
// as the resident.
func probeResidency(ctx context.Context, repo *git.Repo) app.Residency {
	path, err := lockPath(ctx, repo, dispatchLock)
	if err != nil {
		return app.Residency{State: app.ResidencyUnknown}
	}
	h, resident, err := lockfile.Probe(ctx, path)
	switch {
	case err != nil:
		return app.Residency{State: app.ResidencyUnknown}
	case !resident:
		return app.Residency{State: app.NoDispatcher}
	}
	return app.Residency{
		State:  app.DispatcherResident,
		Holder: record.OwnerID{Root: h.Root, Host: h.Host, PID: h.PID, Since: h.Since},
		Since:  h.Since,
	}
}

// residencyFunc is the RE-READING probe the operations that WAIT are
// given, where the ones that do not wait are handed a value.
//
// A dependency is a value unless its answer legitimately changes under
// the operation, and this one does: a --timeout that lasts an hour must
// notice a dispatcher that appeared at minute ten and drop from judging
// to watching. One judge per job, chosen by residency, and residency
// changes.
func residencyFunc(repo *git.Repo) func(context.Context) app.Residency {
	return func(ctx context.Context) app.Residency { return probeResidency(ctx, repo) }
}

// holderOf is the stamp a lock-taker writes: this process, and the verb
// it is running. record.OwnerID and lockfile.Holder are two shapes of
// one fact and this is the single point that maps between them — see
// lockfile.Holder for why the leaf declares its own.
func holderOf(me record.OwnerID, verb string) lockfile.Holder {
	return lockfile.Holder{Root: me.Root, Host: me.Host, PID: me.PID, Since: me.Since, Verb: verb}
}
