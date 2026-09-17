package workflow

import (
	"context"

	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/state"
)

// remoteBranch names the fork branch a contribution already publishes as its
// PR head. A local rename changes the branch locator but not that remote
// identity, so forge verification keeps pushing where the PR looks.
func remoteBranch(ctx context.Context, r state.Reader, changeID record.ChangeID) (string, error) {
	if changeID == "" {
		return "", nil
	}
	change, err := r.Change(ctx, changeID)
	if err != nil {
		return "", err
	}
	if change.PullRequestID == "" {
		return "", nil
	}
	pr, err := r.PullRequest(ctx, change.PullRequestID)
	if err != nil {
		return "", err
	}
	return pr.HeadBranch, nil
}

// withRemoteBranch records the PR head on a build whose local branch differs from it.
func withRemoteBranch(build record.BuildSpec, head string) record.BuildSpec {
	if head != "" && head != build.Branch {
		build.RemoteBranch = head
	}
	return build
}
