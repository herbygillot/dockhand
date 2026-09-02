package cmd

import (
	"context"

	"github.com/spf13/cobra"

	"github.com/herbygillot/dockhand/internal/render"
	"github.com/herbygillot/dockhand/internal/runstate"
)

// discardAction deletes one in-flight branch: the point deletion, the
// user's explicit act. Everything the branch accumulated goes with it —
// running verifications are canceled and their workers released, kept
// failure environments are released, and the notes on the branch's own
// commits are removed rather than left as debris for gc to misjudge.
// The fork copy of a promoted branch is deliberately untouched: it may
// back an open PR, and deleting it is a louder decision than this verb
// makes.
type discardAction struct {
	target string
}

var _ Action = discardAction{}

func (a discardAction) Execute(ctx context.Context, rs *runstate.Context) error {
	repo, err := rs.Repo(ctx)
	if err != nil {
		return err
	}
	eng := rs.Deps()
	branch, err := eng.Resolve(ctx, repo, a.target)
	if err != nil {
		return err
	}
	// The demolition reports what it did as lines rather than printing
	// them, because its four callers put them in four different places.
	// Here they fall where they were written: the deletion on stdout,
	// the advisories about what was left behind on stderr. Printed on
	// the failure path too — a demolition that stopped halfway still did
	// the half it did, and the user is owed the account of it.
	said, err := eng.Discard(ctx, repo, branch, false)
	render.Prose(said, rs.Out, rs.Err)
	return err
}

// Discard builds the discard subcommand.
func Discard() *cobra.Command {
	return &cobra.Command{
		Use:   "discard <branch|port>",
		Short: "Delete an in-flight branch, releasing everything it holds",
		Args:  exactArgs(1),
		RunE: runE(func(_ *cobra.Command, args []string) (Action, error) {
			return discardAction{target: args[0]}, nil
		}),
	}
}
