package cmd

import (
	"context"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/herbygillot/dockhand/internal/intent/bump"
	"github.com/herbygillot/dockhand/internal/macports/port"
	"github.com/herbygillot/dockhand/internal/macports/portfetch"
	"github.com/herbygillot/dockhand/internal/plan"
)

// Bump builds the bump subcommand: move a port to a new version. The
// behavior is intentAction's — this constructor contributes only the
// flags and the planner: resolving "latest" is the one thing the
// command line knows that the intent does not.
func Bump() *cobra.Command {
	var (
		to     string
		latest bool
		f      intentFlags
	)
	c := &cobra.Command{
		Use:   "bump <port|subport|portdir>",
		Short: "Bump a port to a new version, as a branch",
		Args:  exactArgs(1),
		RunE: runE(func(_ *cobra.Command, args []string) (Action, error) {
			switch {
			case to != "" && latest:
				return nil, usagef("--to and --latest are mutually exclusive")
			case to == "latest":
				// The literal string would be planned as a version;
				// resolving the newest release is a different workflow.
				return nil, usagef("use --latest to resolve the newest release")
			}
			if err := f.check(); err != nil {
				return nil, err
			}
			return intentAction{
				verb: "bump", target: args[0],
				opts: f.opts, verify: f.verifyIt, fetches: true,
				prepare: func(ctx context.Context, h port.Handle, pf *portfetch.Fetcher, report io.Writer) (plan.Planner, error) {
					v := to
					if v == "" {
						// No stated version: latest is the intent.
						resolved, rep, err := bump.ResolveLatest(ctx, h, pf)
						if err != nil {
							return nil, err
						}
						fmt.Fprintf(report, "latest: %s (%s)\n", resolved, rep.Verdict)
						v = resolved
					}
					return bump.Bump{Version: v, Force: f.opts.Force}, nil
				},
			}, nil
		}),
	}
	c.Flags().StringVar(&to, "to", "", "the version to bump to")
	c.Flags().BoolVar(&latest, "latest", false, "resolve and bump to the newest upstream release (the default)")
	f.register(c)
	return c
}
