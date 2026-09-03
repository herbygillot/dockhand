package engine

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/render"
)

// Discard is the shared demolition: cancel and release whatever the
// branch's commits hold, remove their notes, delete the branch. clean
// and status's merged-PR autoclean arrive here too, and they pass
// dropFork: dockhand placed the fork copy, so once the PR merged
// dockhand deletes it. Plain discard leaves it, because there the copy
// may back an open PR, and deleting it closes the PR — a louder
// decision than discard makes.
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
// because its four callers put them in four different places: the verb
// prints them where they fall, --replace prints them under its own
// announcement, the sweep prints them above the branch's line, and
// `status --json` prints all of them to stderr, since prose inside the
// document breaks the consumer. The lines are returned on the error
// paths too — a demolition that failed halfway still did the half.
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
	if tracked := repo.TrackedRemote(ctx, branch); tracked != "" {
		if !dropFork {
			warn("the fork copy on %q is untouched — `git push %s --delete %s` removes it", tracked, tracked, branch)
		} else if derr := repo.PushDelete(ctx, tracked, branch); derr != nil {
			// Advisory: the ref may already be gone (GitHub's own
			// delete-branch button, an earlier sweep), and a network
			// refusal must not leave the local demolition half-done.
			warn("warning: the fork copy on %q was not removed: %v", tracked, derr)
		} else {
			warn("removed %s from %q", branch, tracked)
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

// ContentLanded reports whether every file the branch touched reads
// byte-identical on the primary branch — the confirmation half of the
// merged verdict, at the branch's local view of upstream.
func ContentLanded(ctx context.Context, repo *git.Repo, branch string) (bool, error) {
	primary, err := repo.PrimaryBranch(ctx)
	if err != nil {
		return false, err
	}
	base, err := repo.MergeBase(ctx, primary, branch)
	if err != nil {
		return false, err
	}
	paths, err := repo.DiffNames(ctx, base, branch)
	if err != nil {
		return false, err
	}
	for _, p := range paths {
		if strings.HasPrefix(p, "_") {
			continue
		}
		ours, err := repo.BlobAt(ctx, branch, p)
		if err != nil {
			return false, err
		}
		theirs, err := repo.BlobAt(ctx, primary, p)
		if err != nil || string(ours) != string(theirs) {
			return false, nil
		}
	}
	return true, nil
}
