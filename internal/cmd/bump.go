package cmd

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/herbygillot/dockhand/internal/intent"
	"github.com/herbygillot/dockhand/internal/intent/bump"
	"github.com/herbygillot/dockhand/internal/macports/port"
	"github.com/herbygillot/dockhand/internal/macports/portfetch"
	"github.com/herbygillot/dockhand/internal/runstate"
	"github.com/herbygillot/dockhand/internal/upstream"
)

// bumpVerb is the catalogue's entry for moving a port to a new version.
// The behavior is intentAction's — this row contributes only the flags,
// the planner, and the one thing the command line knows that the intent
// does not: what "latest" resolves to.
var bumpVerb = intentVerb{
	Definition: intent.Definition{
		Name:    "bump",
		Fetches: true,
		New: func(p intent.Params) (intent.Planner, error) {
			return bump.Bump{Version: p.Version, Force: p.Recheck, Tools: p.Tools,
				ClosesTicket: p.ClosesTicket, Riders: p.Riders}, nil
		},
	},
	Short: "Bump a port to a new version, as a branch",
	Flags: func(c *cobra.Command, p *intent.Params, f *intentFlags) func() error {
		c.Flags().StringVar(&p.Version, "to", "", "the version to bump to")
		c.Flags().BoolVar(&p.Latest, "latest", false, "resolve and bump to the newest upstream release (the default)")
		c.Flags().BoolVar(&p.Recheck, "recheck", false,
			"re-derive the port at the version it already carries: fetch again, compare, regenerate")
		return func() error {
			switch {
			case p.Version != "" && p.Latest:
				return usagef("--to and --latest are mutually exclusive")
			case p.Version == "latest":
				// The literal string would be planned as a version;
				// resolving the newest release is a different workflow.
				return usagef("use --latest to resolve the newest release")
			}
			// A re-derivation is exactly the case where the port's binary
			// archive is a liar: the version and the revision both stand,
			// so an archive matching them predates the change, and a pass
			// earned by unpacking it would have verified nothing about the
			// distfile the re-derivation went and fetched. refresh-checksums
			// is from source by its nature for the same reason; --recheck is
			// the run asking for it.
			f.opts.FromSource = p.Recheck
			return nil
		}
	},
	Resolve: func(ctx context.Context, rs *runstate.Context, h port.Handle, pf *portfetch.Fetcher, p *intent.Params) error {
		if p.Version != "" {
			return nil
		}
		// No stated version: latest is the intent. The gh seam rides
		// along so the forge's own releases outrank its raw tags where
		// they exist.
		resolved, rep, err := bump.ResolveLatest(ctx, rs.Tools, h, pf, upstream.GhRunner(rs.RunGH))
		if err != nil {
			return err
		}
		fmt.Fprintf(rs.Err, "latest: %s (%s)\n", resolved, rep.Verdict)
		p.Version = resolved
		return nil
	},
}
