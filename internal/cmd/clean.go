package cmd

import (
	"context"

	"github.com/spf13/cobra"

	"github.com/herbygillot/dockhand/internal/engine"
	"github.com/herbygillot/dockhand/internal/render"
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
type cleanAction struct {
	// superseded adds the second sweep: the branches a newer sibling
	// replaced. It is a flag and not the default because a supersede is
	// dockhand's OWN inference from two branch names about one port, made
	// without asking anybody, and inference is not grounds to delete
	// work. Nothing else in the tool removes a superseded branch; this
	// flag is the person saying they meant it.
	superseded bool
}

var _ Action = cleanAction{}

func (a cleanAction) Execute(ctx context.Context, rs *runstate.Context) error {
	rep, err := rs.Deps().Reconcile(ctx, engine.ReconcileOpts{RetireOnly: true})
	if err != nil {
		return err
	}
	rep.Sweep(rs.Out, rs.Err)
	if !a.superseded {
		return nil
	}
	// After the merged sweep and never instead of it. A branch whose pull
	// request merged is retired as merged — the forge's own word about
	// the work, and the more informative of the two answers — and only
	// what survives that is asked whether a sibling replaced it.
	//
	// A separate pass rather than a phase of the reconciler, because it
	// asks a different question of a different source: being superseded
	// is a local fact about two branches in one namespace, so this costs
	// no gh call and works with no network, while every phase of the
	// reconciler is about what GitHub said.
	repo, err := rs.Repo(ctx)
	if err != nil {
		return err
	}
	said, err := rs.Deps().CleanSuperseded(ctx, repo)
	render.Prose(said, rs.Out, rs.Err)
	return err
}

// Clean builds the clean subcommand. The verb borrows `port clean`
// safely: both mean removing the tool's own accumulated work-product.
func Clean() *cobra.Command {
	var superseded bool
	c := &cobra.Command{
		Use:   "clean",
		Short: "Sweep away branches whose pull requests merged",
		Long: `Sweep away the branches whose work is done.

By default that is exactly one thing: a branch whose pull request
merged. Merged-ness is GitHub's own word and never sha ancestry, the
fork copy goes with the branch because dockhand put it there, and
everything kept says why it was kept.

--superseded adds the branches a newer sibling replaced. It is opt-in
because a supersede is dockhand's own inference — two branches about
one port, the newer one minted second — and nothing else in the tool
removes a branch on the strength of it. Their fork copies are left
alone, since one of them may back a pull request somebody is reading,
and a held branch is kept and says so.`,
		Args: noArgs,
		RunE: runE(func(*cobra.Command, []string) (Action, error) {
			return cleanAction{superseded: superseded}, nil
		}),
	}
	c.Flags().BoolVar(&superseded, "superseded", false,
		"also remove branches a newer sibling replaced")
	return c
}
