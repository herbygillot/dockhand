package cmd

import (
	"context"

	"github.com/spf13/cobra"

	"github.com/herbygillot/dockhand/internal/engine"
	"github.com/herbygillot/dockhand/internal/exitcode"
	"github.com/herbygillot/dockhand/internal/runstate"
)

// statusAction is the report, and only the report (D27). It reads the
// branches dockhand observes — the dockhand/* namespace and every
// other local branch whose tip carries a verify note — polls their
// workers, writes what the workers said into the ledger, releases the
// guest of a run whose verdict says so, and renders. That is the whole
// of it. It makes no change anybody else can see: no branch is deleted
// here or on the fork, no queued run is started, nothing is published.
// Where work is waiting the report says so and names `dockhand cycle`
// beside the finding, because with the split nothing begins on its
// own.
//
// Settling stays, and stays on purpose. It is the one write that makes
// the report truthful — every other write in the pass changes the
// world; settle changes the report to match a world that already
// changed — and a status that showed "verifying" over a guest that
// finished an hour ago would be a worse lie than the write it avoided.
// Releasing that guest is the last step of the verdict being written
// rather than an act of its own; a failure keeps its environment by
// rule, and a pass whose run asked with --keep-env keeps it by request,
// and the report names the kept environment either way.
//
// --no-update is the pure read: the ledger as written. It polls
// nothing, writes nothing, takes no lock, and asks no forge and no
// provider — so the pull request standings and the worker audit are
// not shown, and the report says so once at the top rather than
// letting a missing line read as an answer.
//
// The pass itself is the engine's and the wording is render's; what is
// left here is the choice between the two renderings. --json changes
// nothing about what the pass does — it polls and settles either way —
// only where the words go.
//
// The worker audit is asked for separately because only this verb
// wants it rendered, and because it is a provider call the pure read
// may not make.
type statusAction struct {
	json     bool
	noUpdate bool
}

var _ Action = statusAction{}

func (a statusAction) Execute(ctx context.Context, rs *runstate.Context) error {
	repo, err := rs.Repo(ctx)
	if err != nil {
		return a.failed(rs, err)
	}
	e := rs.Deps()
	rep, err := e.Reconcile(ctx, engine.ReconcileOpts{NoUpdate: a.noUpdate})
	if err != nil {
		return a.failed(rs, err)
	}
	if !a.noUpdate {
		rep.Orphans = e.Orphans(ctx, repo)
	}
	if a.json {
		// The document says success because reaching here is success:
		// every way this pass can fail has already returned above, and a
		// write that fails from inside JSON publishes nothing for a twin
		// to be wrong about.
		return rep.JSON(rs.Out, rs.Err, exitcode.Of(exitcode.OK, ""))
	}
	rep.Text(rs.Out, rs.Err)
	return nil
}

// failed publishes the twin when the pass never reached a report, and
// returns the error either way.
//
// A caller that asked for JSON gets JSON however the run ends — the
// same decision --plan made when it started emitting the decline
// itself. Without it, `status --json` against a directory that is not a
// checkout wrote nothing at all to stdout and left the reason in an
// English sentence, so a consumer that captured stdout through a pipe
// and lost $? had a blind spot exactly where it most needed an answer.
// The document says only the twin: what the pass would have reported is
// precisely what could not be learned.
func (a statusAction) failed(rs *runstate.Context, err error) error {
	if !a.json {
		return err
	}
	return sayExit(rs, err)
}

// Status builds the status subcommand.
func Status() *cobra.Command {
	var asJSON, noUpdate bool
	c := &cobra.Command{
		Use:   "status",
		Short: "Report every dockhand branch and its verification standing",
		Args:  noArgs,
		RunE: runE(func(*cobra.Command, []string) (Action, error) {
			return statusAction{json: asJSON, noUpdate: noUpdate}, nil
		}),
	}
	c.Flags().BoolVar(&asJSON, "json", false, "emit the report as JSON on stdout")
	c.Flags().BoolVar(&noUpdate, "no-update", false,
		"show the ledger as written: poll nothing, write nothing, take no locks, ask no forge or provider (PR standings and the worker audit are not shown)")
	return c
}
