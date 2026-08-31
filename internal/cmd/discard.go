package cmd

import (
	"context"
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/runstate"
)

// discardAction deletes one in-flight branch: the point deletion, the
// user's explicit act. Everything the branch accumulated goes with it —
// running verifications are canceled and their workers released, kept
// failure environments are released, and the notes on the branch's own
// commits are removed rather than left as debris for gc to misjudge.
// The fork copy of a promoted branch is deliberately untouched: it may
// back an open PR, and deleting it is a louder decision than this verb
// makes.
type discardAction struct {
	target string
}

var _ Action = discardAction{}

func (a discardAction) Execute(ctx context.Context, rs *runstate.Context) error {
	repo, err := rs.Repo(ctx)
	if err != nil {
		return err
	}
	branch, err := resolveDockhandBranch(ctx, repo, a.target)
	if err != nil {
		return err
	}
	return discardBranch(ctx, rs, repo, branch)
}

// discardBranch is the shared demolition: cancel and release whatever
// the branch's commits hold, remove their notes, delete the branch.
// clean arrives here too, once its evidence says a branch is done.
func discardBranch(ctx context.Context, rs *runstate.Context, repo *git.Repo, branch string) error {
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
	for _, sha := range own {
		n, err := readNote(ctx, repo, sha)
		if errors.Is(err, git.ErrNoNote) {
			continue
		}
		if err != nil {
			return err
		}
		// A running job's worker, or a failed job's kept environment:
		// either way a VM this branch owns, released with it.
		if n.State == "running" || n.Handle != "" {
			if prov, perr := vmProvider(ctx); perr == nil {
				if rerr := prov.Release(ctx, n.Job); rerr != nil {
					fmt.Fprintf(rs.Err, "warning: releasing %s: %v\n", n.Job.ID, rerr)
				}
			} else {
				fmt.Fprintf(rs.Err, "warning: %s holds worker %s, and no provider is available to release it\n", sha[:12], n.Job.ID)
			}
		}
		if err := repo.NoteRemove(ctx, git.VerifyNotesRef, sha); err != nil {
			return err
		}
	}
	if tracked := repo.TrackedRemote(ctx, branch); tracked != "" {
		fmt.Fprintf(rs.Err, "the fork copy on %q is untouched — `git push %s --delete %s` removes it\n",
			tracked, tracked, branch)
	}
	if err := repo.DeleteBranch(ctx, branch); err != nil {
		return err
	}
	fmt.Fprintf(rs.Out, "discarded %s (%s)\n", branch, tip[:12])
	return nil
}

// Discard builds the discard subcommand.
func Discard() *cobra.Command {
	return &cobra.Command{
		Use:   "discard <branch|port>",
		Short: "Delete an in-flight branch, releasing everything it holds",
		Args:  exactArgs(1),
		RunE: runE(func(_ *cobra.Command, args []string) (Action, error) {
			return discardAction{target: args[0]}, nil
		}),
	}
}
