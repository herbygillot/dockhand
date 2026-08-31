package cmd

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/herbygillot/dockhand/internal/intent/bump"
	"github.com/herbygillot/dockhand/internal/macports/port"
	"github.com/herbygillot/dockhand/internal/plan"
	"github.com/herbygillot/dockhand/internal/runstate"
)

// bumpAction moves a port to a new version. An empty to means resolve
// the newest upstream release.
type bumpAction struct {
	target   string
	to       string
	force    bool
	planOnly bool
	inPlace  bool
	verify   bool
	on       string
}

var _ Action = bumpAction{}

// A plan is always produced — the edits, the fetched checksums, and the
// exact predicted delta — because that is what makes the change
// verifiable; under D21 it is internal interchange, never a user
// artifact. The default realizes it as a branch: dockhand/<port>-<to>,
// minted in the object database, the user's checkout untouched.
// --in-place applies it to the working tree instead (with the
// prediction check), and --plan prints it and stops.
func (a bumpAction) Execute(ctx context.Context, rs *runstate.Context) error {
	targets, err := resolveTargets(rs.TreeRoot, false, []string{a.target})
	if err != nil {
		return err
	}
	if len(targets) != 1 {
		return usagef("bump takes exactly one port; %q names %d", a.target, len(targets))
	}
	ev, err := rs.Evaluator(ctx)
	if err != nil {
		return err
	}
	fetcher, err := rs.Fetcher(ctx)
	if err != nil {
		return err
	}
	root, err := rs.TempDir()
	if err != nil {
		return err
	}
	h := port.New(targets[0], ev).WithTempDir(root)

	to := a.to
	if to == "" {
		// No stated version: latest is the intent.
		resolved, report, err := bump.ResolveLatest(ctx, h, fetcher)
		if err != nil {
			return err
		}
		fmt.Fprintf(rs.Err, "latest: %s (%s)\n", resolved, report.Verdict)
		to = resolved
	}

	p, err := bump.Bump{Version: to, Force: a.force}.Plan(ctx, h, fetcher)
	if err != nil {
		return err
	}
	// The summary comes first either way: when the plan is about to be
	// carried out, it is the only chance to see what is being done
	// before it is done.
	renderPlan(rs.Err, p)
	if a.verify || a.on != "" {
		// Before apply, not after: a Portfile known not to build never
		// lands in the tree.
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
	if a.inPlace {
		// The deliberate opt-out (D21): edit where the user stands,
		// uncommitted — for the user running their own workflow, and
		// the only write mode a non-git tree has.
		return applyPlan(ctx, rs, p)
	}
	portName := p.Subport
	if portName == "" {
		portName = filepath.Base(filepath.Clean(p.Portdir))
	}
	return mintFromPlan(ctx, rs, p,
		"dockhand/"+portName+"-"+to,
		fmt.Sprintf("%s: update to %s", portName, to))
}

// Bump builds the bump subcommand: move a port to a new version.
func Bump() *cobra.Command {
	var (
		to       string
		latest   bool
		planOnly bool
		inPlace  bool
		force    bool
		verifyIt bool
		on       string
	)
	c := &cobra.Command{
		Use:   "bump <port|subport|portdir>",
		Short: "Bump a port to a new version, as a branch (D21)",
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
			return bumpAction{
				target:   args[0],
				to:       to,
				force:    force,
				planOnly: planOnly,
				inPlace:  inPlace,
				verify:   verifyIt,
				on:       on,
			}, nil
		}),
	}
	c.Flags().StringVar(&to, "to", "", "the version to bump to")
	c.Flags().BoolVar(&latest, "latest", false, "resolve and bump to the newest upstream release (the default)")
	c.Flags().BoolVar(&planOnly, "plan", false, "emit the plan on stdout and change nothing")
	c.Flags().BoolVar(&inPlace, "in-place", false,
		"edit the Portfile where it stands, uncommitted — no branch, no commit")
	c.Flags().BoolVar(&verifyIt, "verify", false,
		"build the result in a pristine VM before applying; failure applies nothing")
	c.Flags().StringVar(&on, "on", "",
		"macOS release to verify on (implies --verify)")
	c.Flags().BoolVar(&force, "force", false,
		"proceed even if the port is already at the target version, re-deriving checksums and vendored blocks")
	return c
}

// applyPlan carries out a plan and reports what it did. Every intent
// that applies arrives here, so a plan is executed the same way
// whichever produced it.
func applyPlan(ctx context.Context, rs *runstate.Context, p *plan.Plan) error {
	ev, err := rs.Evaluator(ctx)
	if err != nil {
		return err
	}
	if _, err := p.Apply(ctx, ev); err != nil {
		return err
	}
	_, err = fmt.Fprintf(rs.Out, "applied: %s %s (%d edits, delta as predicted)\n",
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
