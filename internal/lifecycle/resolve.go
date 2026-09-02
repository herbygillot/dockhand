package lifecycle

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/herbygillot/dockhand/internal/git"
)

// ErrAmbiguousTarget marks a port name that names several in-flight
// branches: branchable state, because verify falls through to state
// mode when no branch exists but must refuse — not silently verify the
// working tree — when several do.
var ErrAmbiguousTarget = errors.New("ambiguous target")

// ResolveBranch accepts a branch name outright, or a port name
// that names exactly one in-flight branch.
func ResolveBranch(ctx context.Context, repo *git.Repo, target string) (string, error) {
	if repo.HasBranch(ctx, target) {
		return target, nil
	}
	branches, err := repo.Branches(ctx, git.BranchNamespace)
	if err != nil {
		return "", err
	}
	var matches []string
	for _, br := range branches {
		if strings.HasPrefix(br, git.BranchNamespace+target+"-") {
			matches = append(matches, br)
		}
	}
	switch len(matches) {
	case 1:
		return matches[0], nil
	case 0:
		return "", fmt.Errorf("no dockhand branch for %q; `dockhand status` lists what is in flight", target)
	default:
		return "", fmt.Errorf("%w: %q names %d branches (%s); use the full branch name", ErrAmbiguousTarget, target, len(matches), strings.Join(matches, ", "))
	}
}
