package cmd

import (
	"context"

	"github.com/spf13/cobra"

	"github.com/herbygillot/dockhand/internal/engine"
	"github.com/herbygillot/dockhand/internal/runstate"
)

// promoteAction publishes a verified branch: push it to the user's
// fork under its own name, then open the pull request against the
// upstream repository.
//
// The flags are the whole of this verb. What a promotion IS — the gate
// over the verdict set, the duplicate search, the push, the
// convergence on a branch's own open PR, the audit row it leaves — is
// the engine's, because a sweep will one day publish the same way and
// the two must not be two implementations.
type promoteAction struct {
	target    string
	remote    string // fork remote; empty means detect by gh login
	title     string
	closes    string
	noPR      bool
	noVerify  bool // promote an unverified tip deliberately
	noPRCheck bool // skip the duplicate-PR search deliberately
	force     bool // replace the fork branch and refresh the PR
}

var _ Action = promoteAction{}

func (a promoteAction) Execute(ctx context.Context, rs *runstate.Context) error {
	repo, err := rs.Repo(ctx)
	if err != nil {
		return err
	}
	return rs.Deps().Promote(ctx, repo, a.target, engine.PromoteOpts{
		Remote:    a.remote,
		Title:     a.title,
		Closes:    a.closes,
		NoPR:      a.noPR,
		NoVerify:  a.noVerify,
		NoPRCheck: a.noPRCheck,
		Force:     a.force,
	})
}

func Promote() *cobra.Command {
	var (
		remote    string
		title     string
		closes    string
		noPR      bool
		noVerify  bool
		noPRCheck bool
		force     bool
	)
	c := &cobra.Command{
		Use:   "promote <branch|port>",
		Short: "Push a verified branch to your fork and open the pull request",
		Args:  exactArgs(1),
		RunE: runE(func(_ *cobra.Command, args []string) (Action, error) {
			return promoteAction{
				target: args[0], remote: remote,
				title: title, closes: closes, noPR: noPR, noVerify: noVerify,
				noPRCheck: noPRCheck, force: force,
			}, nil
		}),
	}
	c.Flags().StringVar(&remote, "remote", "", "the fork remote to push to (default: the remote owned by your gh login)")
	c.Flags().StringVar(&title, "title", "", "PR title (default: the tip commit's subject)")
	c.Flags().StringVar(&closes, "closes", "", "Trac ticket number the PR closes")
	c.Flags().BoolVar(&noPR, "no-pr", false, "push to the fork without opening a pull request")
	c.Flags().BoolVar(&noVerify, "no-verify", false,
		"promote past a FAILED verification; an unverified branch needs no flag")
	c.Flags().BoolVar(&noPRCheck, "no-pr-check", false,
		"skip the search for pre-existing open PRs on the same port")
	c.Flags().BoolVar(&force, "force", false,
		"replace the fork branch (force-push with lease) and refresh the open PR's title and body")
	return c
}
