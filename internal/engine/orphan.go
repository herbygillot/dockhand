package engine

import (
	"context"
	"fmt"

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
// refusal in untracked — nothing to ask, a backend that cannot list, a
// listing that failed — means the audit learned nothing about this
// machine's workers, which is a different thing from learning there
// are none. Saying nothing is the honest rendering of both, and it is
// what the audit has always done; the alternative is a report that
// fails because a worker could not be counted.
func (e *Engine) Orphans(ctx context.Context, repo *git.Repo) []render.Orphan {
	live, _, err := e.untracked(ctx, repo)
	if err != nil {
		return nil
	}
	var orphans []render.Orphan
	for _, w := range live {
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

// ReclaimOrphans is `cycle --reclaim-orphans` (D27): every untracked
// worker this checkout may claim is handed back through the backend's
// own Release, and what happened to each is reported as lines.
//
// Which it may claim is the ruling the flag rests on (ruled 2026-09-05
// with D27's implementation, pending the maintainer): a worker nothing
// attributes, or one this checkout started. A worker another checkout
// started is THAT checkout's — its notes may claim it as a kept
// failure, invisible from here — and is skipped with a line naming the
// checkout whose own `cycle --reclaim-orphans` reclaims it. Destroying
// a peer's kept environment on the strength of our notes not
// mentioning it would be the audit's one misreading turned into an
// act.
//
// The release goes through verify.Worker.Job and the Lister's Release,
// never through a name: that a job's id is the environment's name is
// one backend's fact, and the kernel does not learn it. A backend that
// listed a worker but named no job for it is asked nothing and said
// so.
//
// Unlike the audit, a refusal to answer is said out loud: a person
// asked for something to happen, and silence would read as nothing
// needing to.
func (e *Engine) ReclaimOrphans(ctx context.Context, repo *git.Repo) []render.Line {
	live, prov, err := e.untracked(ctx, repo)
	if err != nil {
		return []render.Line{{Stream: render.ToErr,
			Text: "warning: no untracked worker reclaimed: " + err.Error()}}
	}
	var said []render.Line
	line := func(stream render.Stream, format string, a ...any) {
		said = append(said, render.Line{Stream: stream, Text: fmt.Sprintf(format, a...)})
	}
	reclaimed := 0
	for _, w := range live {
		switch {
		case w.Owner != "" && w.Owner != repo.Root:
			line(render.ToOut, "%s is a worker from %s — its own `dockhand cycle --reclaim-orphans` reclaims it", w.Name, w.Owner)
		case w.Job.ID == "":
			line(render.ToErr, "warning: %s cannot be reclaimed: the backend named no job for it", w.Name)
		default:
			if rerr := prov.Release(ctx, w.Job); rerr != nil {
				line(render.ToErr, "warning: reclaiming %s: %v", w.Name, rerr)
				continue
			}
			reclaimed++
			line(render.ToOut, "reclaimed %s", w.Name)
		}
	}
	if reclaimed == 0 {
		line(render.ToOut, "no untracked workers reclaimed")
	}
	return said
}

// untracked is the audit's one reading: every worker the backend is
// running that no note in this repository accounts for, with the
// provider that listed them so a caller can hand one back through it.
// The refusals are typed by the sentinel every backend uses for a
// machine that will not answer, so the two callers can word them
// apart — the audit stays silent, the reclaim says why.
func (e *Engine) untracked(ctx context.Context, repo *git.Repo) ([]verify.Worker, verify.Verifier, error) {
	if e.Lister == nil {
		return nil, nil, fmt.Errorf("%w: no backend is wired to list workers", verify.ErrNoProvider)
	}
	prov, err := e.Lister(ctx)
	if err != nil {
		return nil, nil, err
	}
	lister, ok := prov.(verify.WorkerLister)
	if !ok {
		return nil, nil, fmt.Errorf("%w: the backend cannot list its workers", verify.ErrUnsupported)
	}
	live, err := lister.Workers(ctx)
	if err != nil {
		return nil, nil, err
	}
	tracked := e.trackedEnvironments(ctx, repo)
	var out []verify.Worker
	for _, w := range live {
		if tracked[w.Name] {
			continue
		}
		out = append(out, w)
	}
	return out, prov, nil
}

// trackedEnvironments is every environment this repository's notes
// account for, under both names an environment is recorded by: the
// job's id and the handle a kept failure named. Both, because a kept
// environment is a worker with no running job behind it — counting
// only ids would report every held failure as an orphan, which is the
// one state the audit must not misread.
//
// The jobs and not the runs, which is where schema 3 keeps an
// environment: N subjects share one guest, so the names are per job
// and reading them per run would be counting the same worker once for
// each verdict about it.
//
// A job the note says went back accounts for nothing, and skipping it
// is what makes the release order safe. ReleaseJob puts the flag down
// BEFORE the provider is asked, because handing the same guest back
// twice cannot be undone and a leak can — but "can be undone" is only
// true if something finds the leak. A released job still tracked would
// hold its worker's name against the audit forever, so a release that
// crashed in between, or one whose provider simply refused, would be
// invisible in exactly the state the ordering was chosen to survive.
// The skip is safe in both directions: a guest that really did go back
// is gone and matches no live worker, and a kept failure has Released
// false and stays tracked by its handle, which is the one state the
// audit must not misread.
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
		for _, job := range n.Jobs {
			if job.Released {
				continue
			}
			tracked[job.Job.ID] = true
			tracked[job.Handle] = true
		}
	}
	return tracked
}
