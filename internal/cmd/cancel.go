package cmd

import (
	"context"

	"github.com/spf13/cobra"

	"github.com/herbygillot/dockhand/internal/runstate"
)

// cancelAction stops a branch's running verification: worker released,
// note marked canceled, nothing else touched. What that takes is the
// engine's — the same release the stale sweep and a discard perform —
// so what is left here is the branch the user named.
type cancelAction struct {
	target string
}

var _ Action = cancelAction{}

func (a cancelAction) Execute(ctx context.Context, rs *runstate.Context) error {
	repo, err := rs.Repo(ctx)
	if err != nil {
		return err
	}
	return rs.Deps().Cancel(ctx, repo, a.target)
}

// Cancel builds the cancel subcommand.
func Cancel() *cobra.Command {
	return &cobra.Command{
		Use:   "cancel <branch|port>",
		Short: "Stop a running verification, releasing its worker",
		Args:  exactArgs(1),
		RunE: runE(func(_ *cobra.Command, args []string) (Action, error) {
			return cancelAction{target: args[0]}, nil
		}),
	}
}
