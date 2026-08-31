package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/herbygillot/dockhand/internal/macports/port"
	"github.com/herbygillot/dockhand/internal/refresh"
)

// RefreshChecksums builds the refresh-checksums subcommand: make a
// port's recorded checksums true again at its unchanged version.
//
// The command applies by default, as bump does (D16) — the user asking
// is the human in the loop. What the design forbids is this change
// going anywhere PUBLIC unattended, because a checksum that moves at an
// unchanged version means upstream re-rolled the artifact: possibly a
// benign re-tar, possibly a supply-chain event, and the edit cannot
// tell you which. The summary says so every time; nothing here will
// ever auto-promote it.
func RefreshChecksums(rc *RunContext) *cobra.Command {
	var planOnly bool
	c := &cobra.Command{
		Use:     "refresh-checksums <port|subport|portdir>",
		Aliases: []string{"refresh"},
		Short:   "Re-fetch a port's distfiles and repair its recorded checksums",
		Args:    exactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			targets, err := resolveTargets(rc.TreeRoot, false, args)
			if err != nil {
				return err
			}
			if len(targets) != 1 {
				return usagef("refresh-checksums takes exactly one port; %q names %d", args[0], len(targets))
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

			p, err := refresh.Refresh{}.Plan(cmd.Context(), h, fetcher)
			if err != nil {
				return err
			}
			renderPlan(rc.Err, p)
			fmt.Fprintln(rc.Err, "note: these checksums changed at an UNCHANGED version — upstream re-rolled")
			fmt.Fprintln(rc.Err, "the artifact. Establish why before this change goes anywhere public: it may")
			fmt.Fprintln(rc.Err, "be a benign re-tar, or it may be a supply-chain event.")
			if planOnly {
				return p.Encode(rc.Out)
			}
			return applyPlan(cmd.Context(), rc, p)
		},
	}
	c.Flags().BoolVar(&planOnly, "plan", false, "emit the plan on stdout and change nothing")
	return c
}
