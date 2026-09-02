package engine

import (
	"context"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/render"
	"github.com/herbygillot/dockhand/internal/verify"
)

// Orphans names the environments the provider is running that no note
// in this repository accounts for: a pre-mint gate failure keeps its
// environment with no branch to record it against, another checkout's
// jobs are invisible from here, and with a two-guest cap a forgotten
// worker is an expensive kind of quiet.
//
// Asked of the backend through verify.WorkerLister rather than of a
// named one. The audit used to run tart itself, which is silently
// wrong the day a job's provider is not tart, and which no test could
// stand in for. The division is the capability's: the backend lists
// what it is running, and deciding which of those is unaccounted for
// needs this repository's notes, which the backend does not have.
//
// Asked through Lister and not Verifier, because a machine whose base
// images are gone can still be running workers, and that is the machine
// the audit is for. Composing a verifier there fails — there is nothing
// to verify on — and a gate that took the failure for an answer would
// go quiet in precisely the state a forgotten worker is most likely and
// most expensive.
//
// Best-effort throughout, and silent when it cannot answer. Every
// refusal below — nothing to ask, a backend that cannot list, a listing
// that failed — means the audit learned nothing about this machine's
// workers, which is a different thing from learning there are none.
// Saying nothing is the honest rendering of both, and it is what the
// audit has always done; the alternative is a report that fails
// because a worker could not be counted.
func (e *Engine) Orphans(ctx context.Context, repo *git.Repo) []render.Orphan {
	if e.Lister == nil {
		return nil
	}
	prov, err := e.Lister(ctx)
	if err != nil {
		return nil
	}
	lister, ok := prov.(verify.WorkerLister)
	if !ok {
		return nil
	}
	live, err := lister.Workers(ctx)
	if err != nil {
		return nil
	}
	tracked := e.trackedEnvironments(ctx, repo)
	var orphans []render.Orphan
	for _, w := range live {
		if tracked[w.Name] {
			continue
		}
		o := render.Orphan{Name: w.Name}
		// A worker this checkout started is named without an owner: the
		// cross-repo sentence exists to point somewhere else, and
		// pointing a reader back at the checkout they are standing in
		// says nothing.
		if w.Owner != "" && w.Owner != repo.Root {
			o.Owner = w.Owner
		}
		orphans = append(orphans, o)
	}
	return orphans
}

// trackedEnvironments is every environment this repository's notes
// account for, under both names an environment is recorded by: a
// running run's job and a kept failure's handle. Both, because a kept
// environment is a worker with no running job behind it — counting
// only jobs would report every held failure as an orphan, which is the
// one state the audit must not misread.
//
// Unreadable notes are skipped rather than reported. The audit is
// advisory, and a note that cannot be read is a branch's problem,
// stated where that branch is described.
func (e *Engine) trackedEnvironments(ctx context.Context, repo *git.Repo) map[string]bool {
	tracked := map[string]bool{}
	l := e.Ledger(repo)
	noted, err := l.All(ctx)
	if err != nil {
		return tracked
	}
	for _, sha := range noted {
		n, err := l.Read(ctx, sha)
		if err != nil {
			continue
		}
		for _, r := range n.Runs {
			tracked[r.Job.ID] = true
			tracked[r.Handle] = true
		}
	}
	return tracked
}
