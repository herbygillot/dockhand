package cmd

import (
	"context"
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/herbygillot/dockhand/internal/git"
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
	branch, err := resolveDockhandBranch(ctx, repo, a.target)
	if err != nil {
		return err
	}
	tip, err := repo.RevParse(ctx, branch)
	if err != nil {
		return err
	}
	// The stale sweep releases running jobs the branch moved past; the
	// tip's own running job is the one cancel exists for.
	if err := cancelStale(ctx, rs, repo, branch, tip); err != nil {
		return err
	}
	n, err := readNote(ctx, repo, tip)
	if errors.Is(err, git.ErrNoNote) || (err == nil && n.State != "running") {
		fmt.Fprintf(rs.Err, "%s has no running verification\n", branch)
		return nil
	}
	if err != nil {
		return err
	}
	prov, err := vmProvider(ctx)
	if err != nil {
		return err
	}
	if rerr := prov.Release(ctx, n.Job); rerr != nil {
		fmt.Fprintf(rs.Err, "warning: releasing %s: %v\n", n.Job.ID, rerr)
	}
	n.State, n.Detail = "canceled", "canceled by the user"
	if err := writeNote(ctx, repo, n); err != nil {
		return err
	}
	fmt.Fprintf(rs.Out, "canceled verification of %s (worker %s released)\n", branch, n.Job.ID)
	return nil
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
