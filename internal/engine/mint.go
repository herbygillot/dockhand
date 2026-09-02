package engine

import (
	"context"
	"errors"
	"fmt"

	"github.com/herbygillot/dockhand/internal/exitcode"
	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/plan"
	"github.com/herbygillot/dockhand/internal/render"
	"github.com/herbygillot/dockhand/internal/verdict"
)

// planOnBase resolves a plan against the repository it will land in:
// the repo, its primary branch, the Portfile's repo-relative path, and
// the edited bytes — computed from the base commit's blob, never the
// working file, with the plan's precondition hash held against that
// blob. Both realizations that speak git — mint and diff — start here.
// The repository it opens is the run's, resolved once, and anchored on
// the portdir the plan named: an intent may name one outside the tree.
func (e *Engine) planOnBase(ctx context.Context, p *plan.Plan) (repo *git.Repo, primary, path string, edited []byte, err error) {
	repo, err = e.RepoFor(ctx, p.Portdir)
	if err != nil {
		if errors.Is(err, git.ErrNotARepo) {
			// Wrapped, not swallowed: the identity is what routes this
			// to the tree exit band.
			return nil, "", "", nil, fmt.Errorf("%w — the branch workflow needs a git checkout; --in-place edits the tree directly", err)
		}
		return nil, "", "", nil, err
	}
	primary, err = repo.PrimaryBranch(ctx)
	if err != nil {
		return nil, "", "", nil, err
	}
	rel, err := repo.RelPath(p.Portdir)
	if err != nil {
		return nil, "", "", nil, err
	}
	path = rel + "/" + macports.PortfileName
	base, err := repo.BlobAt(ctx, primary, path)
	if err != nil {
		return nil, "", "", nil, err
	}
	edited, err = p.Materialize(base)
	if errors.Is(err, plan.ErrDrift) {
		return nil, "", "", nil, fmt.Errorf("%w: the Portfile on %s is not the one planned against — commit your work there first, or use --in-place", plan.ErrDrift, primary)
	}
	if err != nil {
		return nil, "", "", nil, err
	}
	return repo, primary, path, edited, nil
}

// BranchInFlightError is the refusal an intent gives when its port
// already has a branch: refusal is a feature (exit 5), not a failure —
// the user asked for one thing, and the reason they did not get it is
// a judgment with a remedy, not something broken.
type BranchInFlightError struct {
	Branch string
	// Reason overrides the default message: --force's narrower refusal
	// speaks here.
	Reason string
}

func (e *BranchInFlightError) Error() string {
	if e.Reason != "" {
		return e.Reason
	}
	return fmt.Sprintf("a change for this port is already in flight: %s — discard it, pick up where it left off, or --force to replace it", e.Branch)
}

// ExitCode: refusal is a feature — a decline, never a failure.
func (e *BranchInFlightError) ExitCode() int { return exitcode.Declined }

// replaceInFlight clears the way for --force: the standing branch goes
// through discard's own demolition — running verification canceled,
// workers released, notes removed — but only when the branch is
// exactly what dockhand minted. Commits the user added are theirs;
// destroying them silently is what the refusal exists to prevent, and
// discard remains the explicit act for that.
func (e *Engine) replaceInFlight(ctx context.Context, repo *git.Repo, primary, branch string) error {
	own, err := repo.OwnCommits(ctx, branch, primary)
	if err != nil {
		return err
	}
	if len(own) > 1 {
		return &BranchInFlightError{Branch: branch, Reason: fmt.Sprintf(
			"%s carries %d commit(s) beyond the mint — --force replaces only what dockhand placed; `dockhand discard %s` first if you mean to drop your own work",
			branch, len(own)-1, branch)}
	}
	fmt.Fprintf(e.Err, "replacing in-flight %s (--force)\n", branch)
	// The demolition's own words follow the announcement, on the streams
	// they chose: --force is one of the four places discard's report
	// lands, and it lands here rather than being swallowed because the
	// user asked to destroy a branch and is owed the sentence saying it
	// happened. Printed even when the demolition failed — the half it
	// did is still what it did.
	said, err := e.Discard(ctx, repo, branch, false)
	render.Prose(said, e.Out, e.Err)
	return err
}

// Minted is what a realized branch hands back: enough for the caller
// to submit verification against the sha and tell the user where the
// change lives.
type Minted struct {
	Repo    *git.Repo
	Branch  string
	Sha     string // full commit sha
	RelPort string // repo-relative portdir path
}

// mint realizes a plan as a branch (D21): the edited Portfile is
// committed onto the tree's primary branch at its local position,
// under dockhand's namespace, entirely in the object database — the
// user's HEAD and working tree are never touched. A plan with no edits
// mints nothing and returns nil, nil.
func (e *Engine) mint(ctx context.Context, p *plan.Plan, force bool) (*Minted, error) {
	branch, message := git.MintBranchName(p.Slug), p.Summary
	hasEdits := len(p.Edits) > 0
	// The decision is asked twice, and the order is the reason: the
	// empty-plan answer is reached before the plan is resolved against
	// the repository at all, so a plan with nothing in it never reports
	// drift, while the branch probe below happens after, so a drift
	// refusal precedes a replacement. The first call cannot need the
	// probe; the second is the same question with the probe's answer in
	// it.
	switch verdict.DecideMint(hasEdits, force, false) {
	case verdict.NothingToMint:
		// A no-op realized as a branch would be an empty commit.
		fmt.Fprintln(e.Out, "no edits; no branch minted")
		return nil, nil
	case verdict.NothingToReplace:
		fmt.Fprintln(e.Out, "no edits; no branch minted")
		fmt.Fprintln(e.Err, "an existing in-flight branch, if any, stands: --force replaces only when there is something to replace it with")
		return nil, nil
	case verdict.MintBranch, verdict.ReplaceThenMint:
		// Both mint; which of the two it is cannot be settled until the
		// plan has been resolved against the repository below.
	}
	repo, primary, path, edited, err := e.planOnBase(ctx, p)
	if err != nil {
		return nil, err
	}
	hasBranch := verdict.MintProbesBranch(hasEdits, force) && repo.HasBranch(ctx, branch)
	if verdict.DecideMint(hasEdits, force, hasBranch) == verdict.ReplaceThenMint {
		if err := e.replaceInFlight(ctx, repo, primary, branch); err != nil {
			return nil, err
		}
	}
	sha, err := repo.Mint(ctx, git.MintRequest{
		Branch:  branch,
		Base:    primary,
		Path:    path,
		Content: edited,
		Message: message,
	})
	if err != nil {
		if errors.Is(err, git.ErrBranchExists) {
			return nil, &BranchInFlightError{Branch: branch}
		}
		return nil, err
	}
	rel, err := repo.RelPath(p.Portdir)
	if err != nil {
		return nil, err
	}
	fmt.Fprintf(e.Out, "branch: %s (%s)\n", branch, git.Abbrev(sha))
	fmt.Fprintf(e.Err, "your checkout is untouched — `git checkout %s` to add changes\n", branch)
	return &Minted{Repo: repo, Branch: branch, Sha: sha, RelPort: rel}, nil
}
