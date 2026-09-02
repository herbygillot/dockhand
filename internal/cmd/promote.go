package cmd

import (
	"context"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/herbygillot/dockhand/internal/forge"
	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/ledger"
	"github.com/herbygillot/dockhand/internal/lifecycle"
	"github.com/herbygillot/dockhand/internal/runstate"
	"github.com/herbygillot/dockhand/internal/verdict"
)

// promoteAction publishes a verified branch: push it to the user's
// fork under its own name, then open the pull request against the
// upstream repository. The PR is ring 3 — other people's attention —
// and this is the only verb that spends it (cli.md); everything before
// the PR is the user's own fork, deletable at will.
//
// Nothing is stored: the branch→PR link is derived (D21) — the push
// writes ordinary tracking config, and any later lookup queries pulls
// by head ref, the same way gh itself does.
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
	branch, err := lifecycle.ResolveBranch(ctx, repo, a.target)
	if err != nil {
		return err
	}
	tip, err := repo.RevParse(ctx, branch)
	if err != nil {
		return err
	}

	// Promote refuses an unverified tip: the PR spends reviewer
	// attention, and the private backends exist to predict the shared
	// one's verdict before that happens. Without a local verify
	// provider there is nothing to refuse toward — the machine cannot
	// verify — so the promotion proceeds unverified, says so, and the
	// PR body says so too, which is the candour reviewers accept.
	// A promote issued mid-verification is itself the user's answer
	// about the running build: cancel it with a warning and proceed —
	// the tool removes friction, the note records the cancellation,
	// and the PR simply reads as whatever evidence remains. Local
	// state is the local user's business; the PR only ever says
	// verified or not.
	canceled, err := lifecycle.CancelRunning(ctx, rs, repo, tip, "canceled: promoted without waiting")
	if err != nil {
		return err
	}
	if canceled > 0 {
		fmt.Fprintf(rs.Err, "canceled %d running verification(s) — promoting without waiting\n", canceled)
	}
	n, verified, err := ledger.Open(repo).EvidenceFor(ctx, tip)
	if err != nil {
		return err
	}
	// The gate itself is a judgment about the verdict set, so it is made
	// where the other judgments are; what is left here is saying so.
	gate := verdict.DecidePublish(n, verified, branch, git.Abbrev(tip), a.noVerify)
	if gate.Refusal != nil {
		return gate.Refusal
	}
	for _, line := range gate.Blocked {
		fmt.Fprintln(rs.Err, line)
	}
	if gate.SayUnverified {
		fmt.Fprintln(rs.Err, "promoting unverified; the PR will say so")
	}

	forkRemote, forkOwner, err := forge.ForkRemote(ctx, rs.RunGH, repo, a.remote)
	if err != nil {
		return err
	}
	if a.noPR {
		if err := a.push(ctx, rs, repo, forkRemote, forkOwner, branch); err != nil {
			return err
		}
		return nil
	}

	upstream, err := forge.UpstreamRepo(ctx, repo)
	if err != nil {
		return err
	}
	// The branch's own commits, oldest last (rev-list order): the
	// oldest is the one dockhand minted, and its subject is already in
	// project format (`<port>: <description>`) — later commits are
	// fixups whose subjects would make bad titles. The count also
	// answers the template's squashed-and-minimized checkbox.
	primary, err := repo.PrimaryBranch(ctx)
	if err != nil {
		return err
	}
	ownCommits, err := repo.OwnCommits(ctx, tip, primary)
	if err != nil {
		return err
	}
	title := a.title
	if title == "" {
		subject := tip
		if len(ownCommits) > 0 {
			subject = ownCommits[len(ownCommits)-1]
		}
		title, err = repo.Subject(ctx, subject)
		if err != nil {
			return err
		}
	}
	// A branch that already has its own open PR is re-promotion, not
	// duplication: the push below updates that PR in place, and opening
	// a second one would be the duplicate this verb refuses elsewhere.
	// Looked up by the fork owner, never by tracking config — a branch
	// --force just re-minted has none until the push restores it.
	ownPR, ownFound, err := forge.QueryPR(ctx, rs.RunGH, upstream, forkOwner, branch)
	if err != nil {
		fmt.Fprintf(rs.Err, "warning: could not check for this branch's own PR: %v\n", err)
		ownFound = false
	}
	own := prFact(ownPR, ownFound)
	if err := verdict.MergedDeadEnd(own, branch); err != nil {
		return err
	}

	checkedPRs := false
	if !a.noPRCheck {
		port := verdict.PortName(n.Port, title)
		switch prs, serr := forge.OpenPortPRs(ctx, rs.RunGH, upstream, port); {
		case port == "":
			fmt.Fprintln(rs.Err, "warning: no port name to search open PRs by; skipping the duplicate check")
		case serr != nil:
			// The search is advisory: a rate-limited or offline lookup
			// must not block a promotion, it just leaves the checklist
			// box for the human.
			fmt.Fprintf(rs.Err, "warning: could not search for open PRs: %v\n", serr)
		default:
			checkedPRs = true
			// The advisories are for the PRs the search walked past
			// before the duplicate, so they are said before the refusal
			// is returned — the same order the walk itself produced.
			dup := verdict.CheckDuplicates(prFacts(prs), own, title)
			for _, note := range dup.Notes {
				fmt.Fprintln(rs.Err, note)
			}
			if dup.Refusal != nil {
				return dup.Refusal
			}
		}
	}

	if err := a.push(ctx, rs, repo, forkRemote, forkOwner, branch); err != nil {
		return err
	}
	body := forge.PromoteBody(n, verified, a.closes, len(ownCommits), checkedPRs)
	if own.Found && own.Open {
		if a.force {
			// A replaced branch usually means a new version: the PR's
			// commits moved with the push, and its title and body are
			// stale until told otherwise.
			if _, err := rs.RunGH(ctx, "pr", "edit", fmt.Sprint(own.Number), "--repo", upstream,
				"--title", title, "--body", body); err != nil {
				return fmt.Errorf("the branch is pushed; refreshing PR #%d failed: %w", own.Number, err)
			}
			fmt.Fprintf(rs.Err, "PR #%d replaced: branch force-pushed, title and body refreshed\n", own.Number)
		} else {
			fmt.Fprintf(rs.Err, "PR #%d already open for this branch; the push updated it\n", own.Number)
		}
		fmt.Fprintln(rs.Out, own.URL)
		return nil
	}

	args := []string{"pr", "create", "--repo", upstream,
		"--head", forkOwner + ":" + branch, "--title", title, "--body", body}
	url, err := rs.RunGH(ctx, args...)
	if err != nil {
		return fmt.Errorf("the branch is pushed; opening the PR failed: %w", err)
	}
	fmt.Fprintln(rs.Out, strings.TrimSpace(url))
	return nil
}

// push publishes the branch to the fork: an ordinary push, or the
// with-lease force that replaces a re-minted branch's copy.
func (a promoteAction) push(ctx context.Context, rs *runstate.Context, repo *git.Repo, remote, owner, branch string) error {
	if a.force {
		if err := repo.PushForce(ctx, remote, branch); err != nil {
			return err
		}
		fmt.Fprintf(rs.Err, "force-pushed %s to %s (%s)\n", branch, remote, owner)
		return nil
	}
	if err := repo.Push(ctx, remote, branch); err != nil {
		return err
	}
	fmt.Fprintf(rs.Err, "pushed %s to %s (%s)\n", branch, remote, owner)
	return nil
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
