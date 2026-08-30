package cmd

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/herbygillot/dockhand/internal/bump"
	"github.com/herbygillot/dockhand/internal/macports/eval/pool"
	"github.com/herbygillot/dockhand/internal/macports/port"
	"github.com/herbygillot/dockhand/internal/macports/portfetch"
	"github.com/herbygillot/dockhand/internal/plan"
	"github.com/herbygillot/dockhand/internal/tempdir"
)

// Bump builds the bump subcommand: plan a version bump. Per the
// read/write boundary, bump emits a complete plan on stdout — edits,
// fetched checksums, exact predicted delta — and changes nothing;
// apply consumes it.
func Bump() *cobra.Command {
	var (
		to     string
		latest bool
	)
	c := &cobra.Command{
		Use:   "bump <port|subport|portdir>",
		Short: "Plan a version bump (emits a plan; changes nothing)",
		Args:  exactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			switch {
			case to != "" && latest:
				return usagef("--to and --latest are mutually exclusive")
			case to == "latest":
				// The literal string would be planned as a version;
				// resolving the newest release is a different workflow.
				return usagef("use --latest to resolve the newest release")
			}
			treeRoot, err := cmd.Flags().GetString("tree")
			if err != nil {
				return err
			}
			targets, err := resolveTargets(treeRoot, false, args)
			if err != nil {
				return err
			}
			if len(targets) != 1 {
				return usagef("bump takes exactly one port; %q names %d", args[0], len(targets))
			}
			pfx, err := resolvePrefix(cmd)
			if err != nil {
				return err
			}
			evs, err := pool.New(cmd.Context(), pfx, 1)
			if err != nil {
				return err
			}
			defer evs.Close()
			fetcher, err := portfetch.New(cmd.Context(), pfx, tempdir.Root{})
			if err != nil {
				return err
			}
			defer fetcher.Close()
			h := port.New(targets[0], evs.Evaluators()[0])

			if to == "" {
				// No stated version: latest is the intent.
				resolved, report, err := bump.ResolveLatest(cmd.Context(), h, fetcher)
				if err != nil {
					return err
				}
				fmt.Fprintf(cmd.ErrOrStderr(), "latest: %s (%s)\n", resolved, report.Verdict)
				to = resolved
			}

			p, err := bump.Bump{Version: to}.Plan(cmd.Context(), h, fetcher)
			if err != nil {
				return err
			}
			renderPlan(cmd.ErrOrStderr(), p)
			return p.Encode(cmd.OutOrStdout())
		},
	}
	c.Flags().StringVar(&to, "to", "", "the version to bump to")
	c.Flags().BoolVar(&latest, "latest", false, "resolve and bump to the newest upstream release (the default)")
	return c
}

// Apply builds the apply subcommand: execute a plan.
func Apply() *cobra.Command {
	return &cobra.Command{
		Use:   "apply <plan.json|->",
		Short: "Apply a plan, verifying it does exactly what it predicted",
		Args:  exactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			r := cmd.InOrStdin()
			if args[0] != "-" {
				f, err := os.Open(args[0])
				if err != nil {
					return err
				}
				defer f.Close() //nolint:errcheck // read-path close
				r = f
			}
			p, err := plan.Decode(r)
			if err != nil {
				return err
			}
			ev, done, err := oneEvaluator(cmd)
			if err != nil {
				return err
			}
			defer done()

			if _, err := p.Apply(cmd.Context(), ev); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "applied: %s %s (%d edits, delta as predicted)\n",
				p.Intent, p.Portdir, len(p.Edits))
			return nil
		},
	}
}

// renderPlan writes the human-facing summary of a plan.
func renderPlan(w io.Writer, p *plan.Plan) {
	target := p.Portdir
	if p.Subport != "" {
		target += " (subport " + p.Subport + ")"
	}
	fmt.Fprintf(w, "plan: %s %s, %d edits\n", p.Intent, target, len(p.Edits))
	for _, e := range p.Edits {
		fmt.Fprintf(w, "  %-16s %s -> %s\n", e.Reason+":", e.Old, e.New)
	}
	fmt.Fprintln(w, "predicted delta:")
	for _, cd := range p.Predicted {
		var parts []string
		for _, ch := range cd.Changes {
			parts = append(parts, fmt.Sprintf("%s %s -> %s",
				ch.Field, strings.Join(ch.Old, " "), strings.Join(ch.New, " ")))
		}
		fmt.Fprintf(w, "  %s: %s\n", cd.Subport, strings.Join(parts, "; "))
	}
}
