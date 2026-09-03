package cmd

import (
	"github.com/spf13/cobra"

	"github.com/herbygillot/dockhand/internal/intent"
	"github.com/herbygillot/dockhand/internal/intent/bumprevision"
)

// bumpRevisionVerb is the catalogue's entry for incrementing a port's
// revision, for a stated reason. The edit is trivial; the reason is the
// part only a human has, so the flag is required — and it becomes the
// commit message, because why users must rebuild is exactly what the
// log should say.
var bumpRevisionVerb = intentVerb{
	Definition: intent.Definition{
		Name:    "bump-revision",
		Aliases: []string{"revbump"},
		New: func(p intent.Params) (intent.Planner, error) {
			return bumprevision.BumpRevision{Reason: p.Reason, ClosesTicket: p.ClosesTicket,
				Riders: p.Riders, Dependents: p.Dependents}, nil
		},
	},
	Short: "Increment a port's revision (requires --reason)",
	Flags: func(c *cobra.Command, p *intent.Params, _ *intentFlags) func() error {
		c.Flags().StringVar(&p.Reason, "reason", "", "why users must rebuild (required; becomes the commit message)")
		return func() error {
			if p.Reason == "" {
				return usagef("a revision bump needs --reason: it says why users must rebuild")
			}
			return nil
		}
	},
	// The plural invocation: `bump-revision --for <branch>` accepts the
	// revbump proposal a verification measured, revbumping the
	// dependents it put forward as one more commit on that branch.
	//
	// It is this verb and not one of its own because the edit is this
	// verb's edit. What changes is who chose the ports and who wrote the
	// reason: on the single-port road a person names both, and here the
	// proposal holds both — which is why --for needs no --reason and
	// takes no port.
	Plural: cohortMode,
}
