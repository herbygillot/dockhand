package cmd

import (
	"context"

	"github.com/spf13/cobra"

	"github.com/herbygillot/dockhand/internal/engine"
	"github.com/herbygillot/dockhand/internal/runstate"
)

// statusAction reconciles the dockhand/* namespace: every branch, its
// tip's verification record, and the drift between them. It is a
// reconciler, not a daemon: running jobs are polled here, their
// verdicts written back to the notes, workers released on pass, and —
// the one deletion status performs — a branch whose PR merged is
// cleaned, announced, because a merged PR is GitHub's own word that
// the work landed. Every other cleanup is the user's explicit act.
//
// The pass itself is the engine's and the wording is render's; what is
// left here is the choice between the two renderings. --json changes
// nothing about what the pass does — it polls, settles and cleans
// either way — only where the words go.
//
// The worker audit is asked for separately because only this verb
// wants it: `clean` runs the same pass and has nothing to say about
// workers, and a listing nobody renders is a provider composed for
// nothing.
type statusAction struct {
	json    bool
	noClean bool
}

var _ Action = statusAction{}

func (a statusAction) Execute(ctx context.Context, rs *runstate.Context) error {
	repo, err := rs.Repo(ctx)
	if err != nil {
		return err
	}
	e := rs.Deps()
	rep, err := e.Reconcile(ctx, engine.ReconcileOpts{NoClean: a.noClean, Drain: true})
	if err != nil {
		return err
	}
	rep.Orphans = e.Orphans(ctx, repo)
	if a.json {
		return rep.JSON(rs.Out, rs.Err)
	}
	rep.Text(rs.Out, rs.Err)
	return nil
}

// Status builds the status subcommand.
func Status() *cobra.Command {
	var asJSON, noClean bool
	c := &cobra.Command{
		Use:   "status",
		Short: "Report every dockhand branch and its verification standing",
		Args:  noArgs,
		RunE: runE(func(*cobra.Command, []string) (Action, error) {
			return statusAction{json: asJSON, noClean: noClean}, nil
		}),
	}
	c.Flags().BoolVar(&asJSON, "json", false, "emit the report as JSON on stdout")
	c.Flags().BoolVar(&noClean, "no-clean", false, "report merged PRs without deleting their branches")
	return c
}
