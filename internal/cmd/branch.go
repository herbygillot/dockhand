package cmd

import (
	"context"
	"errors"
	"fmt"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/plan"
	"github.com/herbygillot/dockhand/internal/runstate"
)

// mintFromPlan realizes a plan as a branch (D21): the edited Portfile
// is committed onto the tree's primary branch at its local position,
// under dockhand's namespace, entirely in the object database — the
// user's HEAD and working tree are never touched. The plan's
// precondition hash is held against the base commit's blob, not the
// working file: the commit's parent must contain exactly what was
// planned against, or the mint refuses.
func mintFromPlan(ctx context.Context, rs *runstate.Context, p *plan.Plan, branch, message string) error {
	if len(p.Edits) == 0 {
		// A no-op realized as a branch would be an empty commit.
		fmt.Fprintln(rs.Out, "no edits; no branch minted")
		return nil
	}
	repo, err := git.Open(ctx, p.Portdir)
	if err != nil {
		if errors.Is(err, git.ErrNotARepo) {
			return fmt.Errorf("%s is not in a git checkout: the branch workflow needs one; --in-place edits the tree directly", p.Portdir)
		}
		return err
	}
	primary, err := repo.PrimaryBranch(ctx)
	if err != nil {
		return err
	}
	rel, err := repo.RelPath(p.Portdir)
	if err != nil {
		return err
	}
	path := rel + "/" + macports.PortfileName
	base, err := repo.BlobAt(ctx, primary, path)
	if err != nil {
		return err
	}
	if plan.FileSHA256(base) != p.PortfileSHA256 {
		return fmt.Errorf("%w: the Portfile on %s is not the one planned against — commit your work there first, or use --in-place", plan.ErrDrift, primary)
	}
	edited, err := plan.ApplyEdits(base, p.Edits)
	if err != nil {
		return err
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
			return fmt.Errorf("a change for this port is already in flight: %s — discard it or pick up where it left off", branch)
		}
		return err
	}
	if len(sha) > 12 {
		sha = sha[:12]
	}
	fmt.Fprintf(rs.Out, "branch: %s (%s)\n", branch, sha)
	fmt.Fprintf(rs.Err, "your checkout is untouched — `git checkout %s` to add changes\n", branch)
	return nil
}
