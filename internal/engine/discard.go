package engine

import (
	"context"
	"errors"
	"fmt"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/render"
)

// Discard is the shared demolition: cancel and release whatever the
// branch's commits hold, remove their notes, delete the branch. The
// merged-PR retirement in `cycle` arrives here too, and it passes
// dropFork: dockhand placed the fork copy, so once the PR merged
// dockhand deletes it. Plain discard leaves it, because there the copy
// may back an open PR, and deleting it closes the PR — a louder
// decision than discard makes. `status` never arrives here (D27).
//
// What it removes is the verification notes and nothing else. The audit
// rows on git.OutcomeNotesRef are exempt, and that exemption is the
// whole reason there are two refs: a verification record is working
// state that dies with the branch, while a row saying this change was
// published and merged has to survive the branch or it records nothing
// worth having. The exemption is structural rather than a condition
// here — the ledger's Remove speaks only to the verification ref — and
// the test that holds it is in outcome_test.go, because a rule nobody
// can accidentally break is still one somebody can deliberately move.
//
// What it did comes back as lines rather than going to a stream,
// because its callers put them in different places: the verb prints
// them where they fall, --replace prints them under its own
// announcement, and `cycle` keeps them with the branch's own row. The
// lines are returned on the error paths too — a demolition that failed
// halfway still did the half.
func (e *Engine) Discard(ctx context.Context, repo *git.Repo, branch string, dropFork bool) ([]render.Line, error) {
	var said []render.Line
	warn := func(format string, a ...any) {
		said = append(said, render.Line{Stream: render.ToErr, Text: fmt.Sprintf(format, a...)})
	}
	tip, err := repo.RevParse(ctx, branch)
	if err != nil {
		return said, err
	}
	primary, err := repo.PrimaryBranch(ctx)
	if err != nil {
		return said, err
	}
	own, err := repo.OwnCommits(ctx, branch, primary)
	if err != nil {
		return said, err
	}
	if err := e.releaseAndForget(ctx, repo, own, warn); err != nil {
		return said, err
	}
	// Deliberately outside the critical section above: deleting the fork
	// copy is a network round trip to GitHub, and nothing about it needs
	// the notes to hold still. Holding a flock every peer's status waits
	// on across a push is how one hung remote stalls a whole checkout.
	//
	// And deliberately outside the machine gate, which is a separate
	// claim worth making here so it is not re-litigated at the next read.
	// GateRing3 refuses a machine's PUBLICATION, not a machine's use of
	// git: ring 3 is other people's attention, spent by the pull request
	// and by the branch a reviewer is looking at, and this deletes the
	// user's OWN fork copy of work that has already merged. It is the
	// retirement's whole job, it runs unattended under `cycle --auto`,
	// and a gate worded as "a machine may not push" rather than "a
	// machine may not publish" would stop it working on a timer and buy
	// nobody anything.
	//
	// The remote named is the one actually holding a copy — the
	// remote-tracking ref, which every push writes — and not the one
	// the branch tracks, which a branch cut from origin/master has
	// before it was ever pushed. Advising `git push origin --delete`
	// for a copy that does not exist is advice that fails when taken.
	remote, err := repo.PushedTo(ctx, branch)
	if err != nil {
		return said, err
	}
	if remote != "" {
		if !dropFork {
			warn("the fork copy on %q is untouched — `git push %s --delete %s` removes it", remote, remote, branch)
		} else if derr := repo.PushDelete(ctx, remote, branch); derr != nil {
			// Advisory: the ref may already be gone (GitHub's own
			// delete-branch button, an earlier sweep), and a network
			// refusal must not leave the local demolition half-done.
			warn("warning: the fork copy on %q was not removed: %v", remote, derr)
		} else {
			warn("removed %s from %q", branch, remote)
		}
	}
	if err := repo.DeleteBranch(ctx, branch); err != nil {
		return said, err
	}
	return append(said, render.Line{
		Stream: render.ToOut,
		Text:   fmt.Sprintf("discarded %s (%s)", branch, git.Abbrev(tip)),
	}), nil
}

// releaseAndForget hands back every worker the branch's own commits
// still hold and removes their notes. The read-release-remove over each
// commit is one critical section: a run recorded between the read and
// the removal would be a leaked worker nobody can see.
//
// It is its own function so the lock's scope is its body and no more.
// What follows a discard — a push to the fork, the branch deletion — is
// network and refs, and neither wants a flock the whole checkout
// queues behind.
func (e *Engine) releaseAndForget(ctx context.Context, repo *git.Repo, own []string, warn func(string, ...any)) error {
	unlock, err := repo.LockNotes(ctx)
	if err != nil {
		return err
	}
	defer unlock()
	l := e.Ledger(repo)
	for _, sha := range own {
		n, err := l.Read(ctx, sha)
		if errors.Is(err, git.ErrNoNote) {
			continue
		}
		if err != nil {
			return err
		}
		// Every guest the branch still owns, released with it. The jobs
		// and not the runs: an environment is one thing however many
		// subjects were building in it, and a job already given back is
		// not this branch's to give back twice.
		for _, rel := range releasesIn(n) {
			job, ok := n.Jobs[rel]
			if !ok || job.Released {
				continue
			}
			if prov, perr := e.Verifier(ctx); perr == nil {
				if rerr := prov.Release(ctx, job.Job); rerr != nil {
					warn("warning: releasing %s: %v", job.Job.ID, rerr)
				}
			} else {
				// A seam that was never wired reads the same here as a
				// machine with no provider, and the sentence is the same
				// either way: the worker outlives the branch and somebody
				// has to be told.
				warn("warning: %s holds worker %s, and no provider is available to release it", git.Abbrev(sha), job.Job.ID)
			}
		}
		if err := l.Remove(ctx, sha); err != nil {
			return err
		}
	}
	return nil
}
