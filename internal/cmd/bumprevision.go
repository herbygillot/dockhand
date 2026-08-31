package cmd

import (
	"github.com/spf13/cobra"

	"github.com/herbygillot/dockhand/internal/intent/bumprevision"
	"github.com/herbygillot/dockhand/internal/macports/port"
)

// BumpRevisionCmd builds the bump-revision subcommand: increment a
// port's revision, for a stated reason. The edit is trivial; the reason
// is the part only a human has, so the flag is required.
func BumpRevisionCmd(rc *RunContext) *cobra.Command {
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
		RunE: func(cmd *cobra.Command, args []string) error {
			if reason == "" {
				return usagef("a revision bump needs --reason: it says why users must rebuild")
			}
			targets, err := resolveTargets(rc.TreeRoot, false, args)
			if err != nil {
				return err
			}
			if len(targets) != 1 {
				return usagef("bump-revision takes exactly one port; %q names %d", args[0], len(targets))
			}
			ev, err := rc.Evaluator(cmd.Context())
			if err != nil {
				return err
			}
			root, err := rc.TempDir()
			if err != nil {
				return err
			}
			h := port.New(targets[0], ev).WithTempDir(root)

			p, err := bumprevision.BumpRevision{Reason: reason}.Plan(cmd.Context(), h, nil)
			if err != nil {
				return err
			}
			renderPlan(rc.Err, p)
			if verifyIt || on != "" {
				release, err := releaseFlag(on)
				if err != nil {
					return err
				}
				if err := verifyPlan(cmd.Context(), rc, p, release); err != nil {
					return err
				}
			}
			if planOnly {
				return p.Encode(rc.Out)
			}
			return applyPlan(cmd.Context(), rc, p)
		},
	}
	c.Flags().StringVar(&reason, "reason", "", "why users must rebuild (required; travels in the plan and the eventual commit)")
	c.Flags().BoolVar(&planOnly, "plan", false, "emit the plan on stdout and change nothing")
	c.Flags().BoolVar(&verifyIt, "verify", false, "build the result in a pristine VM before applying")
	c.Flags().StringVar(&on, "on", "", "macOS release to verify on (implies --verify)")
	return c
}
