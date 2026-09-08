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
	"github.com/herbygillot/dockhand/internal/publish"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/run"
	"github.com/herbygillot/dockhand/internal/statestore"
	"github.com/herbygillot/dockhand/internal/verify"
)

// Status is the operation behind `status [--no-update | --refresh]
// [--json]`, and it earns an operation because settling is run + lease
// in one Finish (R13, F1, R10). It SETTLES what is running — run.Finish
// per Active attempt this checkout owns, which releases the lease (the
// idle 33 GB guest) — ONLY when Residency says NoDispatcher. Resident:
// it reads the record and says "building, last observed T; the
// scheduler settles it". Unknown (the lock unreadable, or --no-update,
// which takes no lock): it settles nothing and says why — rule 7,
// because a settle is a judgment and one job has one judge. It never
// starts (only Cycle starts what is queued), never discharges (owed is
// lease.Outstanding as a REPORT, every obligation with its Standing and
// foreign roots named, nothing seized), never retires, never publishes.
//
// Residency is a VALUE here where it is a function on Change, Verify
// and Accept: status runs once and does not wait, so the answer cannot
// change under it. The remedy line is derived from it in cli — resident:
// information; not: "`dockhand dispatch` to keep them moving, or
// `dockhand cycle` to start them once" — which is what makes "cycle
// tells you to run cycle" unrepresentable.
type Status struct {
	Repo      *git.Repo
	Ledger    *ledger.Ledger
	State     *statestore.Store
	Env       publish.Env
	Local     run.Local
	Verifier  func(context.Context) (verify.Verifier, error)
	Me        record.OwnerID
	Residency Residency
	Now       func() time.Time
}

// StatusRequest is the depth: NoUpdate is statestore.Read and nothing
// else (Forge ForgeAsCached with nothing asked, Residency Unknown by
// construction); Forge ForgeRefresh under --refresh, recorded into the
// courtesy cache; ForgeAsCached otherwise, AsOf printed per standing.
type StatusRequest struct {
	NoUpdate bool
	Forge    publish.ForgePolicy
}

// StatusResult is what status found, and it is the input to report's
// Standings; app does not render. Settled names the attempts this
// invocation judged (empty under a resident dispatcher or an unknown
// residency); Obligations carries every one with its Standing; Spent is
// publish.Facts.Spent over the store, the allowance a dispatcher has
// used. Disagreeing is every bound record whose ref a foreign hand moved
// or deleted — a person's own `git commit` on a dockhand branch, a `git
// branch -D`, a hand-moved pin — observed by the facts loop and SHOWN
// (rule 7: a change missing from Facts read as "nothing to report"); the
// tool declines, the person dismisses, and the remedy line is cli's:
// "`dockhand verify <branch>` follows the person's commit; `dockhand
// discard <branch>` ends it". Exit is never 84 (Pass's, Q15): 0 unless
// it could not read — status reports; the person acts.
//
// Spent is a publish.Spend and not an int, which is step 8's correction
// carried forward: an int spend admits the whole cap when nobody counted
// it, and Spend's own constructor is what makes "nothing spent" and
// "nobody asked" two values.
type StatusResult struct {
	State       statestore.State
	Settled     []string
	Facts       map[record.ChangeID]publish.Facts
	Disagreeing []*change.TipDisagreement
	Obligations []lease.Obligation
	Residency   Residency
	Spent       publish.Spend
	Vacancy     verify.Vacancy
}

// Run reads, settles by residency, reports what is owed, and gathers the
// forge's word per open change. It returns what it found beside an
// error: a facts loop that could not reach the forge has still settled
// what it settled, and a caller that threw the result away would lose a
// judgment it just wrote.
func (s Status) Run(ctx context.Context, r StatusRequest) (StatusResult, error) {
	st, err := s.State.Read(ctx)
	if err != nil {
		return StatusResult{}, err
	}
	res := StatusResult{State: st, Residency: s.Residency, Facts: map[record.ChangeID]publish.Facts{}}
	if r.NoUpdate {
		return res, nil // one read, and says so on its first line
	}
	prov, provErr := provider(ctx, s.Verifier)
	// settle: by residency, and only NoDispatcher judges.
	if s.Residency.State == NoDispatcher && provErr == nil {
		for _, a := range live(st, s.Me) {
			final, err := run.Finish(ctx, s.State, s.Ledger, prov, s.Local, a, specOf(st, a), nil, s.Claimant(), s.Now)
			if err != nil {
				return res, err
			}
			if final.Settled() {
				res.Settled = append(res.Settled, a.ID)
			}
		}
	}
	// owed: a report, nothing seized.
	if provErr == nil {
		if res.Obligations, err = lease.Outstanding(ctx, s.State, prov, s.Me, "", s.Now()); err != nil {
			return res, err
		}
		res.Vacancy = ask(ctx, prov)
	}
	// facts: cached or refreshed, per open change; a disagreeing ref is a
	// row of its own, never skipped.
	for _, key := range slices.Sorted(maps.Keys(st.Changes)) {
		c := st.Changes[key]
		if c.State.Closed() {
			continue
		}
		ref, err := change.Resolve(ctx, s.Repo, s.State, resolveTarget(c))
		if isTipDisagrees(err) {
			res.Disagreeing = append(res.Disagreeing, disagreementOf(err))
			continue
		}
		if err != nil {
			continue
		}
		f, err := publish.Gather(ctx, s.Env, ref, r.Forge, publish.Asks{}, record.Human, s.Now())
		if err != nil {
			continue
		}
		res.Facts[c.ID], res.Spent = f, f.Spent
	}
	return res, nil
}

// Claimant is who this status is, for the claims its settles take. It
// carries no pass token: a status is not a pass, and a lease it settles
// must not be attributed to one.
func (s Status) Claimant() lease.Claimant { return lease.Claimant{Owner: s.Me} }

// resolveTarget is the name change.Resolve takes for a record: its
// Branch for a branch record, change.PinRef(ID) for a branchless one —
// Resolve's fourth target form, which names the record by id and reads
// the pin. A port is NOT the name for it, since a port may carry a
// snapshot and a branch change at once. Status's facts loop, Discard and
// Cycle's retire share it.
func resolveTarget(c record.Change) string {
	if c.Branch == "" {
		return change.PinRef(c.ID)
	}
	return c.Branch
}
