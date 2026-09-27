package macports

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/model"
)

// Moving a member of a shared release takes authorization, but for the
// port that initiated the update and an obsolete follower.
func TestSharedReleaseAuthorizationExemptsOnlyFollowers(t *testing.T) {
	initiating := ReleaseMember{Target: model.Target{Name: "kubectl-1.37"}}
	follower := ReleaseMember{Target: model.Target{Name: "kubectl"}, Follower: true}
	sibling := ReleaseMember{Target: model.Target{Name: "kubectl-1.36"}}
	require.False(t, initiating.NeedsAuthorization("kubectl-1.37"))
	require.False(t, follower.NeedsAuthorization("kubectl-1.37"))
	require.True(t, sibling.NeedsAuthorization("kubectl-1.37"))
}
