package cmd

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/herbygillot/dockhand/internal/intent/refresh"
	"github.com/herbygillot/dockhand/internal/macports/port"
	"github.com/herbygillot/dockhand/internal/runstate"
)

// refreshAction makes a port's recorded checksums true again at its
// unchanged version.
type refreshAction struct {
	target   string
	planOnly bool
	diff     bool
	inPlace  bool
	verify   bool
	noVerify bool
	on       string
}

var _ Action = refreshAction{}

// The action applies by default, as bump does (D16) — the user asking
// is the human in the loop. What the design forbids is this change
// going anywhere PUBLIC unattended, because a checksum that moves at an
// unchanged version means upstream re-rolled the artifact: possibly a
// benign re-tar, possibly a supply-chain event, and the edit cannot
// tell you which. The summary says so every time; nothing here will
// ever auto-promote it.
func (a refreshAction) Execute(ctx context.Context, rs *runstate.Context) error {
	targets, err := resolveTargets(rs.TreeRoot, false, []string{a.target})
	if err != nil {
		return err
	}
	if len(targets) != 1 {
		return usagef("refresh-checksums takes exactly one port; %q names %d", a.target, len(targets))
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

	p, err := refresh.Refresh{}.Plan(ctx, h, fetcher)
	if err != nil {
		return err
	}
	renderPlan(rs.Err, p)
	fmt.Fprintln(rs.Err, "note: these checksums changed at an UNCHANGED version — upstream re-rolled")
	fmt.Fprintln(rs.Err, "the artifact. Establish why before this change goes anywhere public: it may")
	fmt.Fprintln(rs.Err, "be a benign re-tar, or it may be a supply-chain event.")
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
	return realizePlan(ctx, rs, p, realizeOpts{
		planOnly: a.planOnly, diff: a.diff, inPlace: a.inPlace,
		noVerify: a.noVerify, on: a.on,
		branch:  "dockhand/" + portName + "-checksums",
		message: portName + ": update checksums",
	})
}

// RefreshChecksums builds the refresh-checksums subcommand.
func RefreshChecksums() *cobra.Command {
	var (
		planOnly bool
		diffOnly bool
		inPlace  bool
		verifyIt bool
		noVerify bool
		on       string
	)
	c := &cobra.Command{
		Use:     "refresh-checksums <port|subport|portdir>",
		Aliases: []string{"refresh"},
		Short:   "Re-fetch a port's distfiles and repair its recorded checksums",
		Args:    exactArgs(1),
		RunE: runE(func(_ *cobra.Command, args []string) (Action, error) {
			switch {
			case diffOnly && inPlace, diffOnly && planOnly:
				return nil, usagef("--diff is an output mode of its own; combine it with neither --plan nor --in-place")
			case verifyIt && noVerify:
				return nil, usagef("--verify and --no-verify are mutually exclusive")
			}
			return refreshAction{
				target:   args[0],
				planOnly: planOnly,
				diff:     diffOnly,
				inPlace:  inPlace,
				verify:   verifyIt,
				noVerify: noVerify,
				on:       on,
			}, nil
		}),
	}
	c.Flags().BoolVar(&planOnly, "plan", false, "emit the plan on stdout and change nothing")
	c.Flags().BoolVar(&diffOnly, "diff", false,
		"print the patch the branch would carry, as a git diff; write nothing")
	c.Flags().BoolVar(&inPlace, "in-place", false,
		"edit the Portfile where it stands, uncommitted — no branch, no commit")
	c.Flags().BoolVar(&noVerify, "no-verify", false,
		"mint the branch without submitting background verification")
	c.Flags().BoolVar(&verifyIt, "verify", false,
		"build the result in a pristine VM before applying; failure applies nothing")
	c.Flags().StringVar(&on, "on", "",
		"macOS release to verify on (implies --verify)")
	return c
}
