package engine

import (
	"context"
	"errors"
	"fmt"
	"path"
	"strconv"
	"strings"

	"github.com/herbygillot/dockhand/internal/exitcode"
	"github.com/herbygillot/dockhand/internal/gh"
	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/render"
	"github.com/herbygillot/dockhand/internal/verdict"
)

// The two ways a promotion ends half done. Both leave the fork branch
// published and the pull request not, and that is the whole reason
// they are types: re-running a promote is not free — it force-pushes,
// it retitles, it spends a review notification — so a script has to be
// able to tell "nothing happened" from "the branch is up, finish the
// PR by hand". They carry the branch and the remote for the same
// reason: a caller told only that something failed cannot say where
// the work is standing.
//
// The words are unchanged from when these were plain errors; only
// their identity is new.
type PushedPRError struct {
	Branch string
	Remote string
	Err    error
}

func (e *PushedPRError) Error() string {
	return fmt.Sprintf("the branch is pushed; opening the PR failed: %v", e.Err)
}

func (e *PushedPRError) Unwrap() error { return e.Err }

// DockhandExit: the partial band — half the work stands.
func (e *PushedPRError) DockhandExit() int { return exitcode.PushedPRFailed }

// Code names the outcome for a machine.
func (e *PushedPRError) Code() string { return "pushed-pr-failed" }

// PRRefreshError is the same partial publication met by a branch whose
// pull request already existed: the push landed and the PR still
// describes the change it used to carry.
type PRRefreshError struct {
	Branch string
	Remote string
	Number int
	Err    error
}

func (e *PRRefreshError) Error() string {
	return fmt.Sprintf("the branch is pushed; refreshing PR #%d failed: %v", e.Number, e.Err)
}

func (e *PRRefreshError) Unwrap() error { return e.Err }

// DockhandExit: the partial band — the branch moved, its description did
// not.
func (e *PRRefreshError) DockhandExit() int { return exitcode.PRRefreshFailed }

// Code names the outcome for a machine.
func (e *PRRefreshError) Code() string { return "pr-refresh-failed" }

// PromoteOpts is everything a promotion takes besides the branch:
// where it pushes, what the pull request says, and which of promote's
// refusals the caller has already answered.
type PromoteOpts struct {
	// Remote is the fork remote to push to; empty detects it by gh
	// login.
	Remote string
	// Title overrides the minted commit's subject.
	Title string
	// Closes is the Trac ticket the pull request closes.
	Closes string

	// NoPR pushes to the fork and stops there.
	NoPR bool
	// NoVerify publishes past a FAILED verification — the one refusal
	// that stands on evidence rather than on absence.
	NoVerify bool
	// NoPRCheck skips the search for a pre-existing open PR on the same
	// port.
	NoPRCheck bool
	// Force replaces the fork branch and refreshes the open PR.
	Force bool
}

// Promote publishes a verified branch: push it to the user's fork under
// its own name, then open the pull request against the upstream
// repository. The PR is ring 3 — other people's attention — and this is
// the only thing that spends it (cli.md); everything before the PR is
// the user's own fork, deletable at will.
//
// Nothing about the pull request is stored: the branch→PR link is
// derived (D21) — the push writes ordinary tracking config, and any
// later lookup queries pulls by head ref, the same way gh itself does.
// That derivation is also what makes a second promotion of one branch
// converge on the pull request the first opened instead of opening a
// second one.
//
// What IS recorded is the publication itself, as an audit row: the act
// is the thing history wants, and it cannot be reconstructed later from
// a pull request that has since been edited, merged or closed.
func (e *Engine) Promote(ctx context.Context, repo *git.Repo, target string, o PromoteOpts) error {
	branch, err := e.Resolve(ctx, repo, target)
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
	freed, err := e.cancelRuns(ctx, repo, tip, "canceled: promoted without waiting", false)
	if err != nil && !errors.Is(err, git.ErrNoNote) {
		// A tip with no note is the ordinary unverified shape and holds
		// nothing to cancel; anything else is a ledger this promotion
		// cannot reason about.
		return err
	}
	if len(freed) > 0 {
		fmt.Fprintf(e.Err, "canceled %d running verification(s) — promoting without waiting\n", len(freed))
	}
	n, verified, err := e.Ledger(repo).EvidenceFor(ctx, tip)
	if err != nil {
		return err
	}
	// The gate itself is a judgment about the verdict set, so it is made
	// where the other judgments are; what is left here is saying so.
	gate := verdict.DecidePublish(n, verified, branch, git.Abbrev(tip), o.NoVerify)
	if gate.Refusal != nil {
		return gate.Refusal
	}
	for _, line := range gate.Blocked {
		fmt.Fprintln(e.Err, line)
	}
	if gate.SayUnverified {
		fmt.Fprintln(e.Err, "promoting unverified; the PR will say so")
	}
	// The publication as the audit will record it, filled in as it
	// happens: a push with no pull request is a publication too, and it
	// is the one whose number stays zero.
	pub := Publication{MintSha: tip, Branch: branch, Port: n.Port,
		Verified: verified, Invoker: record.Human}

	forkRemote, forkOwner, err := gh.ForkRemote(ctx, e.Gh, repo, o.Remote)
	if err != nil {
		return err
	}
	if o.NoPR {
		if err := e.push(ctx, repo, forkRemote, forkOwner, branch, o.Force); err != nil {
			return err
		}
		e.recordPublication(ctx, repo, pub)
		return nil
	}

	upstream, err := gh.UpstreamRepo(ctx, repo)
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
	title := o.Title
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
	ownPR, ownFound, err := gh.QueryPR(ctx, e.Gh, upstream, forkOwner, branch)
	if err != nil {
		fmt.Fprintf(e.Err, "warning: could not check for this branch's own PR: %v\n", err)
		ownFound = false
	}
	own := PRFact(ownPR, ownFound)
	if err := verdict.MergedDeadEnd(own, branch); err != nil {
		return err
	}

	checkedPRs := false
	if !o.NoPRCheck {
		port := verdict.PortName(n.Port, title)
		switch prs, serr := gh.OpenPortPRs(ctx, e.Gh, upstream, port); {
		case port == "":
			fmt.Fprintln(e.Err, "warning: no port name to search open PRs by; skipping the duplicate check")
		case serr != nil:
			// The search is advisory: a rate-limited or offline lookup
			// must not block a promotion, it just leaves the checklist
			// box for the human.
			fmt.Fprintf(e.Err, "warning: could not search for open PRs: %v\n", serr)
		default:
			checkedPRs = true
			// The advisories are for the PRs the search walked past
			// before the duplicate, so they are said before the refusal
			// is returned — the same order the walk itself produced.
			dup := verdict.CheckDuplicates(prFacts(prs), own, title)
			for _, note := range dup.Notes {
				fmt.Fprintln(e.Err, note)
			}
			if dup.Refusal != nil {
				return dup.Refusal
			}
		}
	}

	if err := e.push(ctx, repo, forkRemote, forkOwner, branch, o.Force); err != nil {
		return err
	}
	body := render.PRBody(n, verified, o.Closes, len(ownCommits), checkedPRs)
	if own.Found && own.Open {
		if o.Force {
			// A replaced branch usually means a new version: the PR's
			// commits moved with the push, and its title and body are
			// stale until told otherwise.
			if _, err := e.Gh(ctx, "pr", "edit", fmt.Sprint(own.Number), "--repo", upstream,
				"--title", title, "--body", body); err != nil {
				return &PRRefreshError{Branch: branch, Remote: forkRemote, Number: own.Number, Err: err}
			}
			fmt.Fprintf(e.Err, "PR #%d replaced: branch force-pushed, title and body refreshed\n", own.Number)
		} else {
			fmt.Fprintf(e.Err, "PR #%d already open for this branch; the push updated it\n", own.Number)
		}
		fmt.Fprintln(e.Out, own.URL)
		pub.PRNumber = own.Number
		e.recordPublication(ctx, repo, pub)
		return nil
	}

	args := []string{"pr", "create", "--repo", upstream,
		"--head", forkOwner + ":" + branch, "--title", title, "--body", body}
	url, err := e.Gh(ctx, args...)
	if err != nil {
		return &PushedPRError{Branch: branch, Remote: forkRemote, Err: err}
	}
	url = strings.TrimSpace(url)
	fmt.Fprintln(e.Out, url)
	pub.PRNumber = prNumberIn(url)
	e.recordPublication(ctx, repo, pub)
	return nil
}

// recordPublication appends the audit's opening row, reporting a
// failure to write it as a warning and nothing more. By the time this
// runs the change is public: telling the user their promotion failed
// because a note could not be appended would be the more misleading of
// the two answers, and the exit code would be a lie about the PR.
func (e *Engine) recordPublication(ctx context.Context, repo *git.Repo, p Publication) {
	if err := e.Publish(ctx, repo, p); err != nil {
		fmt.Fprintf(e.Err, "warning: recording the publication: %v\n", err)
	}
}

// prNumberIn reads the pull request's number out of the URL `gh pr
// create` prints, which is the only thing it prints. A URL that does
// not end in a number yields zero — the audit row would rather say it
// does not know the number than claim a wrong one.
func prNumberIn(url string) int {
	n, err := strconv.Atoi(path.Base(url))
	if err != nil {
		return 0
	}
	return n
}

// push publishes the branch to the fork: an ordinary push, or the
// with-lease force that replaces a re-minted branch's copy.
func (e *Engine) push(ctx context.Context, repo *git.Repo, remote, owner, branch string, force bool) error {
	if force {
		if err := repo.PushForce(ctx, remote, branch); err != nil {
			return err
		}
		fmt.Fprintf(e.Err, "force-pushed %s to %s (%s)\n", branch, remote, owner)
		return nil
	}
	if err := repo.Push(ctx, remote, branch); err != nil {
		return err
	}
	fmt.Fprintf(e.Err, "pushed %s to %s (%s)\n", branch, remote, owner)
	return nil
}

// prFacts maps a list gh returned. Every entry exists, so every one of
// them is Found.
func prFacts(prs []gh.PullRequest) []verdict.PRFact {
	out := make([]verdict.PRFact, 0, len(prs))
	for _, pr := range prs {
		out = append(out, PRFact(pr, true))
	}
	return out
}
