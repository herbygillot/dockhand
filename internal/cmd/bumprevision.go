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
			return bumprevision.BumpRevision{Reason: p.Reason, ClosesTicket: p.ClosesTicket}, nil
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
}
