package cmd

import (
	"context"

	"github.com/spf13/cobra"

	"github.com/herbygillot/dockhand/internal/intent/refresh"
	"github.com/herbygillot/dockhand/internal/macports/port"
	"github.com/herbygillot/dockhand/internal/macports/portfetch"
	"github.com/herbygillot/dockhand/internal/plan"
	"github.com/herbygillot/dockhand/internal/runstate"
)

// refreshCaution is printed with every refresh summary. The intent
// applies like any other — the user asking is the human in the loop —
// but a checksum that moves at an unchanged version means upstream
// re-rolled the artifact: possibly a benign re-tar, possibly a
// supply-chain event, and the edit cannot tell you which.
const refreshCaution = "note: these checksums changed at an UNCHANGED version — upstream re-rolled\n" +
	"the artifact. Establish why before this change goes anywhere public: it may\n" +
	"be a benign re-tar, or it may be a supply-chain event.\n"

// RefreshChecksums builds the refresh-checksums subcommand: make a
// port's recorded checksums true again at its unchanged version.
func RefreshChecksums() *cobra.Command {
	var f intentFlags
	c := &cobra.Command{
		Use:     "refresh-checksums <port|subport|portdir>",
		Aliases: []string{"refresh"},
		Short:   "Re-fetch a port's distfiles and repair its recorded checksums",
		Args:    exactArgs(1),
		RunE: runE(func(_ *cobra.Command, args []string) (Action, error) {
			if err := f.check(); err != nil {
				return nil, err
			}
			return intentAction{
				verb: "refresh-checksums", target: args[0],
				opts: f.opts, verify: f.verifyIt, fetches: true,
				caution: refreshCaution,
				prepare: func(context.Context, *runstate.Context, port.Handle, *portfetch.Fetcher) (plan.Planner, error) {
					return refresh.Refresh{}, nil
				},
			}, nil
		}),
	}
	f.register(c)
	return c
}
