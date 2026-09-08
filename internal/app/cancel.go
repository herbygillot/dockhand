package app

import (
	"context"
	"maps"
	"slices"
	"time"

	"github.com/herbygillot/dockhand/internal/change"
	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/lease"
	"github.com/herbygillot/dockhand/internal/ledger"
	"github.com/herbygillot/dockhand/internal/platform"
	"github.com/herbygillot/dockhand/internal/progress"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/run"
	"github.com/herbygillot/dockhand/internal/statestore"
	"github.com/herbygillot/dockhand/internal/verify"
)

// Cancel is R21's operation, and it is TWO calls once Finish is a
// sequencer: run.Finish with Interrupt Canceled for each Active attempt
// on the tip, and lease.Release for each finished attempt's KEPT
// environment ("done debugging, the slot back"; the verdict stands).
// The stale sweep the shipped verb performs first is grafted back in as
// the same supersede stage Verify runs.
//
// ONE JUDGE: a cancellation is an observation Judge reads
// (Evidence.Interrupt), not a second verdict-writer, and no run.Cancel
// effect function is needed because under the five-method Verifier
// stopping a job IS releasing its environment. Publication never calls
// this (settled rule). Also composed by bump --replace, by Discard and
// by Cycle's retire, which is why its body is run and not Run: those
// roads have already resolved.
type Cancel struct {
	Repo     *git.Repo
	Ledger   *ledger.Ledger
	State    *statestore.Store
	Verifier func(context.Context) (verify.Verifier, error)
	// Local is run.Finish's propose seam; a canceled build proposes
	// nothing, but Finish is one function with one signature.
	Local    run.Local
	Me       record.OwnerID
	Now      func() time.Time
	Progress progress.Sink
}

// CancelResult says what was freed, per release and not per run.
type CancelResult struct {
	Stopped  []string // attempts finished with Interrupt Canceled
	Released []string // kept environments handed back, verdicts standing
}

// Run resolves the target and cancels what stands on its tip. A moved or
// deleted ref is reported (45) and nothing is stopped: this is the road
// a person typed, and a person who has moved the branch themselves is
// asked which of the two they meant before any environment is destroyed.
func (c Cancel) Run(ctx context.Context, target string) (CancelResult, error) {
	ref, err := change.Resolve(ctx, c.Repo, c.State, target)
	if err != nil {
		return CancelResult{}, err
	}
	st, err := c.State.Read(ctx)
	if err != nil {
		return CancelResult{}, err
	}
	return c.run(ctx, st, ref.ID(), ref.Tip())
}

// run is the body Discard, --replace, Survey's supersede and Cycle's
// retire compose without re-resolving: the record's id and its recorded
// tip, never a Ref.
func (c Cancel) run(ctx context.Context, st statestore.State, id record.ChangeID, tip string) (CancelResult, error) {
	var res CancelResult
	prov, err := provider(ctx, c.Verifier)
	if err != nil {
		// nothing held needs no provider; anything held is an error
		if isNoProvider(err) && nothingHeld(st, id, tip) {
			return res, nil
		}
		return res, err
	}
	if err := supersede(ctx, c.State, c.Ledger, prov, c.Local, st, id, tip, c.Claimant(), c.Me, c.Now); err != nil {
		return res, err
	}
	for _, a := range onTip(st, id, tip) {
		switch {
		case a.Active():
			itr := &record.Interrupt{Why: record.InterruptCanceled, By: c.Me, At: c.Now(), Detail: "canceled by the user"}
			if _, err := run.Finish(ctx, c.State, c.Ledger, prov, c.Local, a, specOf(st, a), itr, c.Claimant(), c.Now); err != nil {
				return res, err
			}
			res.Stopped = append(res.Stopped, a.ID)
		case a.Settled() && held(st, a):
			if err := lease.Release(ctx, c.State, prov, a.Change, a.Platform, c.Claimant(), c.Now); err != nil {
				return res, err
			}
			res.Released = append(res.Released, a.Lease)
		}
	}
	return res, nil
}

// Claimant is who this cancellation is, written onto every claim it
// takes. It carries no pass token: a cancel is a person's act and a pass
// composes this body under its own Claimant through Cycle's retire.
func (c Cancel) Claimant() lease.Claimant { return lease.Claimant{Owner: c.Me} }

// nothingHeld is the predicate behind "nothing held needs no provider":
// this change's tip carries no Active attempt and no kept lease, so
// there is nothing a provider would have been asked to stop. It is asked
// only when the provider is ABSENT, and it is the difference between a
// `discard` that works on a laptop with no tart and one that refuses.
func nothingHeld(s statestore.State, id record.ChangeID, tip string) bool {
	for _, a := range onTip(s, id, tip) {
		if a.Active() || (a.Settled() && held(s, a)) {
			return false
		}
	}
	stale := run.Stale(s, s.Changes[string(id)], tip)
	return len(stale.Active) == 0 && len(stale.Kept) == 0
}

// onTip is this change's attempts at the tip that stands, in a stable
// order. The former tips' work is supersede's population and not this
// one's, which is why the two stages are separate and both run.
func onTip(s statestore.State, id record.ChangeID, tip string) []record.Attempt {
	var out []record.Attempt
	for _, key := range slices.Sorted(maps.Keys(s.Attempts)) {
		if a := s.Attempts[key]; a.Change == id && a.Sha == tip {
			out = append(out, a)
		}
	}
	return out
}

// held reports that the environment this attempt used is still in this
// checkout's hands: a --keep-env debug guest, or a failure's kept
// environment. The join is through the attempt's own lease token, which
// is the store's key for a lease and the one name a lease is guaranteed
// to have.
func held(s statestore.State, a record.Attempt) bool {
	if a.Lease == "" {
		return false
	}
	l, ok := s.Leases[a.Lease]
	return ok && l.Held()
}

// specOf re-derives the question an attempt was enqueued with, from the
// RECORD and from nothing else, so that the spec a drain hands Finish an
// hour later is the spec the enqueue computed. run.Roster is what seats
// the members — including a withheld one and a person's forced override
// — and the Ask carries the enqueuer's --test and --keep-env rather than
// the flags of whatever process is settling.
//
// KeepEnv and Trace are outside run.Spec.ID by construction, so a spec
// re-derived here matches the enqueued one whether or not the watcher
// asked to keep the environment; FromSource and Requires are inside it
// and are NOT re-derivable from the record — they are the preflight's,
// read from a staged Portfile at start — which is why this value is for
// OBSERVING a run and never for enqueuing one. run.Start derives its own.
func specOf(s statestore.State, a record.Attempt) run.Spec {
	c := s.Changes[string(a.Change)]
	members, withheld := run.Roster(c, a)
	rel, known := platform.ByName(a.Platform)
	if !known {
		// A release this build cannot name: the platform is carried as the
		// record spells it so nothing downstream invents one, and the
		// provider refuses it as unsupported rather than building the
		// wrong thing.
		rel = platform.Release{Name: a.Platform}
	}
	return run.Spec{
		Content:  a.Content,
		Roster:   members,
		Withheld: withheld,
		Platform: rel,
		Test:     a.Ask.Test,
		KeepEnv:  a.Ask.KeepEnv,
	}
}
