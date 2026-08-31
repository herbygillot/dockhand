package cmd

import (
	"context"

	"github.com/spf13/cobra"

	"github.com/herbygillot/dockhand/internal/intent/bumprevision"
	"github.com/herbygillot/dockhand/internal/macports/port"
	"github.com/herbygillot/dockhand/internal/runstate"
)

// bumpRevisionAction increments a port's revision, for a stated reason.
// The edit is trivial; the reason is the part only a human has.
type bumpRevisionAction struct {
	target   string
	reason   string
	planOnly bool
	verify   bool
	on       string
}

var _ Action = bumpRevisionAction{}

func (a bumpRevisionAction) Execute(ctx context.Context, rs *runstate.Context) error {
	targets, err := resolveTargets(rs.TreeRoot, false, []string{a.target})
	if err != nil {
		return err
	}
	if len(targets) != 1 {
		return usagef("bump-revision takes exactly one port; %q names %d", a.target, len(targets))
	}
	ev, err := rs.Evaluator(ctx)
	if err != nil {
		return err
	}
	root, err := rs.TempDir()
	if err != nil {
		return err
	}
	h := port.New(targets[0], ev).WithTempDir(root)

	p, err := bumprevision.BumpRevision{Reason: a.reason}.Plan(ctx, h, nil)
	if err != nil {
		return err
	}
	renderPlan(rs.Err, p)
	if a.verify || a.on != "" {
		release, err := releaseFlag(a.on)
		if err != nil {
			return err
		}
		if err := verifyPlan(ctx, rs, p, release); err != nil {
			return err
		}
	}
	if a.planOnly {
		return p.Encode(rs.Out)
	}
	return applyPlan(ctx, rs, p)
}

// BumpRevisionCmd builds the bump-revision subcommand.
func BumpRevisionCmd() *cobra.Command {
	var (
		reason   string
		planOnly bool
		verifyIt bool
		on       string
	)
	c := &cobra.Command{
		Use:     "bump-revision <port|subport|portdir>",
		Aliases: []string{"revbump"},
		Short:   "Increment a port's revision (requires --reason)",
		Args:    exactArgs(1),
		RunE: runE(func(_ *cobra.Command, args []string) (Action, error) {
			if reason == "" {
				return nil, usagef("a revision bump needs --reason: it says why users must rebuild")
			}
			return bumpRevisionAction{
				target:   args[0],
				reason:   reason,
				planOnly: planOnly,
				verify:   verifyIt,
				on:       on,
			}, nil
		}),
	}
	c.Flags().StringVar(&reason, "reason", "", "why users must rebuild (required; travels in the plan and the eventual commit)")
	c.Flags().BoolVar(&planOnly, "plan", false, "emit the plan on stdout and change nothing")
	c.Flags().BoolVar(&verifyIt, "verify", false, "build the result in a pristine VM before applying")
	c.Flags().StringVar(&on, "on", "", "macOS release to verify on (implies --verify)")
	return c
}
