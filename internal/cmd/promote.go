package cmd

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/herbygillot/dockhand/internal/engine"
	"github.com/herbygillot/dockhand/internal/record"
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
	// force force-pushes the fork branch, with lease, and refreshes the
	// PR. It keeps the name the intents' switch gave up in S10 because it
	// is git's own word for what it does — a force-push — and it is a
	// different act on a different thing: --replace destroys a local
	// branch dockhand minted, this one moves a remote branch dockhand
	// already published. The lease is the difference that matters: a fork
	// copy moved from another machine refuses rather than being trampled.
	force bool
}

var _ Action = promoteAction{}

// promoteClosesNote is what a late ticket costs, said once at the point
// it is typed.
//
// The commit message was written at mint and promote does not rewrite
// it: an amend would move the sha out from under a branch the user may
// have built on, and out from under the evidence that names it. So the
// ticket reaches the pull request body and nothing else, and the
// checklist box that asks for the ticket in the COMMIT message stays
// unchecked — honestly, because the commit does not have it.
const promoteClosesNote = "note: --closes reaches the pull request body only. The commit message was\n" +
	"written when the branch was minted and is not rewritten here, so the\n" +
	"\"referenced existing tickets ... in commit message\" box stays unchecked.\n" +
	"Plan the change with --closes to put the trailer in the commit itself.\n"

func (a promoteAction) Execute(ctx context.Context, rs *runstate.Context) error {
	// Before the repository and before the network: who is asking is not
	// a question about this checkout, and a refusal that first had to
	// open a repository would answer a different one on a machine that
	// has none.
	//
	// promote is the human road by construction — it is not that a
	// machine promotes as itself and is gated afterwards. There is one
	// machine publish path, the reconciler's slot, and letting this verb
	// be a second one would make `dockhand promote --auto` the bypass
	// around everything guarding the first.
	if rs.Invoker == record.Machine {
		return &PromoteIsHumanError{}
	}
	repo, err := rs.Repo(ctx)
	if err != nil {
		return err
	}
	if a.closes != "" {
		fmt.Fprint(rs.Err, promoteClosesNote)
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
		"force-push the fork branch (with lease) and refresh the open PR's title and body; not the intents' --replace, which demolishes a local branch")
	return c
}
