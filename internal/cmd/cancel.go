package cmd

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/ledger"
	"github.com/herbygillot/dockhand/internal/lifecycle"
	"github.com/herbygillot/dockhand/internal/record"
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
	l := ledger.Open(repo)
	n, err := l.Read(ctx, tip)
	if errors.Is(err, git.ErrNoNote) {
		fmt.Fprintf(rs.Err, "%s has no verification to cancel\n", branch)
		return nil
	}
	if err != nil {
		return err
	}
	// Two things a cancel can free: a running job, and a failed run's
	// kept debug environment — "done debugging, the slot back please"
	// previously had no verb short of discarding the branch. The
	// failure verdict stays; only the environment goes.
	touched := false
	for plat, run := range n.Runs {
		switch {
		case run.State == record.Running:
			prov, perr := rs.VerifyProvider(ctx)
			if perr != nil {
				return perr
			}
			if rerr := prov.Release(ctx, run.Job); rerr != nil {
				fmt.Fprintf(rs.Err, "warning: releasing %s: %v\n", run.Job.ID, rerr)
			}
			run.State, run.Detail = record.Canceled, "canceled by the user"
			fmt.Fprintf(rs.Out, "canceled verification of %s on %s (worker %s released)\n", branch, plat, run.Job.ID)
		case run.State == record.Failed && run.Handle != "":
			prov, perr := rs.VerifyProvider(ctx)
			if perr != nil {
				return perr
			}
			if rerr := prov.Release(ctx, run.Job); rerr != nil {
				fmt.Fprintf(rs.Err, "warning: releasing kept environment %s: %v\n", run.Handle, rerr)
			}
			run.Handle = ""
			run.Detail = strings.TrimSuffix(run.Detail, "\n") + " — kept environment released"
			fmt.Fprintf(rs.Out, "released kept environment of %s on %s (the failed verdict stands)\n", branch, plat)
		default:
			continue
		}
		n.Runs[plat] = run
		touched = true
	}
	if !touched {
		fmt.Fprintf(rs.Err, "%s has no running verification or kept environment\n", branch)
		return nil
	}
	return l.Write(ctx, n)
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
