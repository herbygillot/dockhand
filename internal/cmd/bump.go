package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/herbygillot/dockhand/internal/bump"
	"github.com/herbygillot/dockhand/internal/macports/port"
	"github.com/herbygillot/dockhand/internal/plan"
)

// Bump builds the bump subcommand: move a port to a new version.
//
// A plan is always produced — the edits, the fetched checksums, and the
// exact predicted delta — because that is what makes the change
// verifiable. What --plan decides is whether the plan is carried out or
// handed back. Applying it here is the same code path apply runs on a
// saved plan, including the check that the result matches the
// prediction, so the default is not a shortcut around verification.
func Bump(rc *RunContext) *cobra.Command {
	var (
		to       string
		latest   bool
		planOnly bool
	)
	c := &cobra.Command{
		Use:   "bump <port|subport|portdir>",
		Short: "Bump a port to a new version (--plan to emit a plan instead)",
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
			targets, err := resolveTargets(rc.TreeRoot, false, args)
			if err != nil {
				return err
			}
			if len(targets) != 1 {
				return usagef("bump takes exactly one port; %q names %d", args[0], len(targets))
			}
			ev, err := rc.Evaluator(cmd.Context())
			if err != nil {
				return err
			}
			fetcher, err := rc.Fetcher(cmd.Context())
			if err != nil {
				return err
			}
			root, err := rc.TempDir()
			if err != nil {
				return err
			}
			h := port.New(targets[0], ev).WithTempDir(root)

			if to == "" {
				// No stated version: latest is the intent.
				resolved, report, err := bump.ResolveLatest(cmd.Context(), h, fetcher)
				if err != nil {
					return err
				}
				fmt.Fprintf(rc.Err, "latest: %s (%s)\n", resolved, report.Verdict)
				to = resolved
			}

			p, err := bump.Bump{Version: to}.Plan(cmd.Context(), h, fetcher)
			if err != nil {
				return err
			}
			// The summary comes first either way: when the plan is
			// about to be carried out, it is the only chance to see
			// what is being done before it is done.
			renderPlan(rc.Err, p)
			if planOnly {
				return p.Encode(rc.Out)
			}
			return applyPlan(cmd.Context(), rc, p)
		},
	}
	c.Flags().StringVar(&to, "to", "", "the version to bump to")
	c.Flags().BoolVar(&latest, "latest", false, "resolve and bump to the newest upstream release (the default)")
	c.Flags().BoolVar(&planOnly, "plan", false, "emit the plan on stdout and change nothing")
	return c
}

// Apply builds the apply subcommand: execute a plan.
func Apply(rc *RunContext) *cobra.Command {
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
			return applyPlan(cmd.Context(), rc, p)
		},
	}
}

// applyPlan carries out a plan and reports what it did. Both the bump
// that just produced one and the apply that read one from disk arrive
// here, so a plan is executed the same way whichever made it.
func applyPlan(ctx context.Context, rc *RunContext, p *plan.Plan) error {
	ev, err := rc.Evaluator(ctx)
	if err != nil {
		return err
	}
	if _, err := p.Apply(ctx, ev); err != nil {
		return err
	}
	_, err = fmt.Fprintf(rc.Out, "applied: %s %s (%d edits, delta as predicted)\n",
		p.Intent, p.Portdir, len(p.Edits))
	return err
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
