package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/herbygillot/dockhand/internal/edit"
	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/plan"
	"github.com/herbygillot/dockhand/internal/runstate"
)

// planOnBase resolves a plan against the repository it will land in:
// the repo, its primary branch, the Portfile's repo-relative path, and
// the edited bytes — computed from the base commit's blob, never the
// working file, with the plan's precondition hash held against that
// blob. Both realizations that speak git — mint and diff — start here.
func planOnBase(ctx context.Context, p *plan.Plan) (repo *git.Repo, primary, path string, edited []byte, err error) {
	repo, err = git.Open(ctx, p.Portdir)
	if err != nil {
		if errors.Is(err, git.ErrNotARepo) {
			return nil, "", "", nil, fmt.Errorf("%s is not in a git checkout: the branch workflow needs one; --in-place edits the tree directly", p.Portdir)
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
	if edit.FileSHA256(base) != p.PortfileSHA256 {
		return nil, "", "", nil, fmt.Errorf("%w: the Portfile on %s is not the one planned against — commit your work there first, or use --in-place", plan.ErrDrift, primary)
	}
	edited, err = edit.Apply(base, p.Edits)
	if err != nil {
		return nil, "", "", nil, err
	}
	return repo, primary, path, edited, nil
}

// mintFromPlan realizes a plan as a branch (D21): the edited Portfile
// is committed onto the tree's primary branch at its local position,
// under dockhand's namespace, entirely in the object database — the
// user's HEAD and working tree are never touched.
func mintFromPlan(ctx context.Context, rs *runstate.Context, p *plan.Plan, branch, message string) error {
	if len(p.Edits) == 0 {
		// A no-op realized as a branch would be an empty commit.
		fmt.Fprintln(rs.Out, "no edits; no branch minted")
		return nil
	}
	repo, primary, path, edited, err := planOnBase(ctx, p)
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

// diffFromPlan renders a plan as the patch its branch would carry,
// writing nothing the workspace can see: the edited blob is grafted
// into the base tree exactly as a mint would, and the two trees are
// diffed instead of committed. Repo-relative a/ and b/ paths come out
// correct because the trees carry the full structure.
func diffFromPlan(ctx context.Context, rs *runstate.Context, p *plan.Plan) error {
	if len(p.Edits) == 0 {
		fmt.Fprintln(rs.Err, "no edits; nothing to diff")
		return nil
	}
	repo, primary, path, edited, err := planOnBase(ctx, p)
	if err != nil {
		return err
	}
	tree, err := repo.GraftTree(ctx, primary, path, edited)
	if err != nil {
		return err
	}
	patch, err := repo.DiffTrees(ctx, primary+"^{tree}", tree)
	if err != nil {
		return err
	}
	// Page the way git diff would: through the user's own pager
	// (GIT_PAGER, core.pager, PAGER — git's chain), and only when
	// stdout is a terminal. diff-tree itself runs behind a pipe, so
	// the pager decision is dockhand's to make at its own stdout.
	if f, ok := rs.Out.(*os.File); ok {
		if fi, err := f.Stat(); err == nil && fi.Mode()&os.ModeCharDevice != 0 {
			if pager := repo.Pager(ctx); pager != "" && pager != "cat" {
				return git.RunPager(ctx, pager, patch, rs.Out, rs.Err)
			}
		}
	}
	_, err = rs.Out.Write(patch)
	return err
}
