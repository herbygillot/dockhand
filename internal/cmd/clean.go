package cmd

import (
	"context"

	"github.com/spf13/cobra"

	"github.com/herbygillot/dockhand/internal/engine"
	"github.com/herbygillot/dockhand/internal/runstate"
)

// cleanAction sweeps the dockhand/* namespace by evidence: a branch
// whose pull request merged is done, and everything it holds is
// released through the same demolition discard uses. Merged-ness is
// never sha ancestry — the project's merge styles rewrite commits as
// they land, so `git branch --merged` sees nothing; the PR's own state
// decides, confirmed by comparing the touched files' bytes against the
// primary branch. Every deletion is reported; everything kept says
// why. The branch→PR link is derived, never stored: the tracking
// remote names the fork owner, and the PR is found by head ref — the
// lookup gh itself uses. Head-ref queries return a bounded handful,
// which is what keeps this sweep safe from the silent-truncation trap
// a bulk PR listing invites.
//
// It is the reconciler with everything but the retirement turned off.
// The sweep and the report reached the same verdicts by two code paths
// for as long as both existed, and the only way to know they agreed was
// to run both; now there is one pass and two renderings of it.
type cleanAction struct{}

var _ Action = cleanAction{}

func (cleanAction) Execute(ctx context.Context, rs *runstate.Context) error {
	rep, err := rs.Deps().Reconcile(ctx, engine.ReconcileOpts{RetireOnly: true})
	if err != nil {
		return err
	}
	rep.Sweep(rs.Out, rs.Err)
	return nil
}

// Clean builds the clean subcommand. The verb borrows `port clean`
// safely: both mean removing the tool's own accumulated work-product.
func Clean() *cobra.Command {
	return &cobra.Command{
		Use:   "clean",
		Short: "Sweep away branches whose pull requests merged",
		Args:  noArgs,
		RunE: runE(func(*cobra.Command, []string) (Action, error) {
			return cleanAction{}, nil
		}),
	}
}
