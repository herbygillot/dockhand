package cmd

import (
	"context"
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/lifecycle"
	"github.com/herbygillot/dockhand/internal/runstate"
)

// cancelAction stops a branch's running verification: worker released,
// note marked canceled, nothing else touched. Field evidence made the
// case — a bad first attempt kicked off an hours-long universal build,
// and the only lever was tart surgery behind dockhand's back.
type cancelAction struct {
	target string
}

var _ Action = cancelAction{}

func (a cancelAction) Execute(ctx context.Context, rs *runstate.Context) error {
	repo, err := rs.Repo(ctx)
	if err != nil {
		return err
	}
	branch, err := lifecycle.ResolveBranch(ctx, repo, a.target)
	if err != nil {
		return err
	}
	tip, err := repo.RevParse(ctx, branch)
	if err != nil {
		return err
	}
	// The stale sweep releases running jobs the branch moved past; the
	// tip's own running job is the one cancel exists for.
	if err := lifecycle.CancelStale(ctx, rs, repo, branch, tip); err != nil {
		return err
	}
	unlock, err := repo.LockNotes(ctx)
	if err != nil {
		return err
	}
	defer unlock()
	n, err := lifecycle.ReadNote(ctx, repo, tip)
	if errors.Is(err, git.ErrNoNote) || (err == nil && !n.AnyState("running")) {
		fmt.Fprintf(rs.Err, "%s has no running verification\n", branch)
		return nil
	}
	if err != nil {
		return err
	}
	prov, err := lifecycle.VMProvider(ctx)
	if err != nil {
		return err
	}
	for plat, run := range n.Runs {
		if run.State != "running" {
			continue
		}
		if rerr := prov.Release(ctx, run.Job); rerr != nil {
			fmt.Fprintf(rs.Err, "warning: releasing %s: %v\n", run.Job.ID, rerr)
		}
		run.State, run.Detail = "canceled", "canceled by the user"
		n.Runs[plat] = run
		fmt.Fprintf(rs.Out, "canceled verification of %s on %s (worker %s released)\n", branch, plat, run.Job.ID)
	}
	return lifecycle.WriteNote(ctx, repo, n)
}

// Cancel builds the cancel subcommand.
func Cancel() *cobra.Command {
	return &cobra.Command{
		Use:   "cancel <branch|port>",
		Short: "Stop a running verification, releasing its worker",
		Args:  exactArgs(1),
		RunE: runE(func(_ *cobra.Command, args []string) (Action, error) {
			return cancelAction{target: args[0]}, nil
		}),
	}
}
