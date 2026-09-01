package cmd

import (
	"context"

	"github.com/spf13/cobra"

	"github.com/herbygillot/dockhand/internal/intent/bumprevision"
	"github.com/herbygillot/dockhand/internal/macports/port"
	"github.com/herbygillot/dockhand/internal/macports/portfetch"
	"github.com/herbygillot/dockhand/internal/plan"
	"github.com/herbygillot/dockhand/internal/runstate"
)

// BumpRevisionCmd builds the bump-revision subcommand: increment a
// port's revision, for a stated reason. The edit is trivial; the
// reason is the part only a human has, so the flag is required — and
// it becomes the commit message, because why users must rebuild is
// exactly what the log should say.
func BumpRevisionCmd() *cobra.Command {
	var (
		reason string
		f      intentFlags
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
			if err := f.check(); err != nil {
				return nil, err
			}
			return intentAction{
				verb: "bump-revision", target: args[0],
				opts: f.opts, verify: f.verifyIt,
				prepare: func(context.Context, *runstate.Context, port.Handle, *portfetch.Fetcher) (plan.Planner, error) {
					return bumprevision.BumpRevision{Reason: reason}, nil
				},
			}, nil
		}),
	}
	c.Flags().StringVar(&reason, "reason", "", "why users must rebuild (required; becomes the commit message)")
	f.register(c)
	return c
}
