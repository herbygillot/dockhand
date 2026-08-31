package cmd

import (
	"context"
	"path/filepath"
	"strings"

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
	diff     bool
	inPlace  bool
	verify   bool
	noVerify bool
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
	portName := p.Subport
	if portName == "" {
		portName = filepath.Base(filepath.Clean(p.Portdir))
	}
	// The branch is named for the revision it reaches; the reason is
	// the commit's description — it is why users must rebuild, which
	// is exactly what the log should say.
	next := "next"
	for _, e := range p.Edits {
		if strings.HasPrefix(e.Reason, "revision") {
			next = e.New
		}
	}
	return realizePlan(ctx, rs, p, realizeOpts{
		planOnly: a.planOnly, diff: a.diff, inPlace: a.inPlace,
		noVerify: a.noVerify, on: a.on,
		branch:  "dockhand/" + portName + "-rev" + next,
		message: portName + ": " + a.reason,
	})
}

// BumpRevisionCmd builds the bump-revision subcommand.
func BumpRevisionCmd() *cobra.Command {
	var (
		reason   string
		planOnly bool
		diffOnly bool
		inPlace  bool
		verifyIt bool
		noVerify bool
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
			switch {
			case diffOnly && inPlace, diffOnly && planOnly:
				return nil, usagef("--diff is an output mode of its own; combine it with neither --plan nor --in-place")
			case verifyIt && noVerify:
				return nil, usagef("--verify and --no-verify are mutually exclusive")
			}
			return bumpRevisionAction{
				target:   args[0],
				reason:   reason,
				planOnly: planOnly,
				diff:     diffOnly,
				inPlace:  inPlace,
				verify:   verifyIt,
				noVerify: noVerify,
				on:       on,
			}, nil
		}),
	}
	c.Flags().StringVar(&reason, "reason", "", "why users must rebuild (required; travels in the plan and the eventual commit)")
	c.Flags().BoolVar(&planOnly, "plan", false, "emit the plan on stdout and change nothing")
	c.Flags().BoolVar(&diffOnly, "diff", false,
		"print the patch the branch would carry, as a git diff; write nothing")
	c.Flags().BoolVar(&inPlace, "in-place", false,
		"edit the Portfile where it stands, uncommitted — no branch, no commit")
	c.Flags().BoolVar(&noVerify, "no-verify", false,
		"mint the branch without submitting background verification")
	c.Flags().BoolVar(&verifyIt, "verify", false, "build the result in a pristine VM before applying")
	c.Flags().StringVar(&on, "on", "", "macOS release to verify on (implies --verify)")
	return c
}
