package lifecycle

import (
	"context"
	"errors"
	"fmt"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/ledger"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/runstate"
)

// DiscardBranch is the shared demolition: cancel and release whatever
// the branch's commits hold, remove their notes, delete the branch.
// clean and status's merged-PR autoclean arrive here too, and they
// pass dropFork: dockhand placed the fork copy, so once the PR merged
// dockhand deletes it. Plain discard leaves it, because there the copy
// may back an open PR, and deleting it closes the PR — a louder
// decision than discard makes.
func DiscardBranch(ctx context.Context, rs *runstate.Context, repo *git.Repo, branch string, dropFork bool) error {
	tip, err := repo.RevParse(ctx, branch)
	if err != nil {
		return err
	}
	primary, err := repo.PrimaryBranch(ctx)
	if err != nil {
		return err
	}
	own, err := repo.OwnCommits(ctx, branch, primary)
	if err != nil {
		return err
	}
	// The read-release-remove over each commit's note is one critical
	// section: a run recorded between the read and the removal would be
	// a leaked worker nobody can see.
	unlock, err := repo.LockNotes(ctx)
	if err != nil {
		return err
	}
	defer unlock()
	l := ledger.Open(repo)
	for _, sha := range own {
		n, err := l.Read(ctx, sha)
		if errors.Is(err, git.ErrNoNote) {
			continue
		}
		if err != nil {
			return err
		}
		// A running job's worker, or a failed job's kept environment:
		// either way a VM this branch owns, released with it.
		for _, run := range n.Runs {
			if run.State != record.Running && run.Handle == "" {
				continue
			}
			if prov, perr := rs.VerifyProvider(ctx); perr == nil {
				if rerr := prov.Release(ctx, run.Job); rerr != nil {
					fmt.Fprintf(rs.Err, "warning: releasing %s: %v\n", run.Job.ID, rerr)
				}
			} else {
				fmt.Fprintf(rs.Err, "warning: %s holds worker %s, and no provider is available to release it\n", git.Abbrev(sha), run.Job.ID)
			}
		}
		if err := l.Remove(ctx, sha); err != nil {
			return err
		}
	}
	if tracked := repo.TrackedRemote(ctx, branch); tracked != "" {
		if !dropFork {
			fmt.Fprintf(rs.Err, "the fork copy on %q is untouched — `git push %s --delete %s` removes it\n",
				tracked, tracked, branch)
		} else if derr := repo.PushDelete(ctx, tracked, branch); derr != nil {
			// Advisory: the ref may already be gone (GitHub's own
			// delete-branch button, an earlier sweep), and a network
			// refusal must not leave the local demolition half-done.
			fmt.Fprintf(rs.Err, "warning: the fork copy on %q was not removed: %v\n", tracked, derr)
		} else {
			fmt.Fprintf(rs.Err, "removed %s from %q\n", branch, tracked)
		}
	}
	if err := repo.DeleteBranch(ctx, branch); err != nil {
		return err
	}
	fmt.Fprintf(rs.Out, "discarded %s (%s)\n", branch, git.Abbrev(tip))
	return nil
}
