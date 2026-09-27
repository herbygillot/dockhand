package model

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

// The selection is embedded so that a release keeps its flat shape.
func TestReleaseSelectionIsFlat(t *testing.T) {
	release := Release{ReleaseSelection: ReleaseSelection{Requested: "2.0", CurrentVersion: "1.0", Stability: "stable"}, Version: "2.0", Tag: "v2.0"}
	raw, err := json.Marshal(release)
	require.NoError(t, err)
	var flat map[string]any
	require.NoError(t, json.Unmarshal(raw, &flat))
	require.Equal(t, "2.0", flat["Requested"])
	require.Equal(t, "1.0", flat["CurrentVersion"])
	require.NotContains(t, flat, "ReleaseSelection")
	var back Release
	require.NoError(t, json.Unmarshal(raw, &back))
	require.Equal(t, release, back)
}

// Moving a member of a shared release takes authorization, but for the
// port that initiated the update and an obsolete follower.
func TestSharedReleaseAuthorizationExemptsOnlyFollowers(t *testing.T) {
	initiating := ReleaseMember{Target: Target{Name: "kubectl-1.37"}}
	follower := ReleaseMember{Target: Target{Name: "kubectl"}, Follower: true}
	sibling := ReleaseMember{Target: Target{Name: "kubectl-1.36"}}
	require.False(t, initiating.NeedsAuthorization("kubectl-1.37"))
	require.False(t, follower.NeedsAuthorization("kubectl-1.37"))
	require.True(t, sibling.NeedsAuthorization("kubectl-1.37"))
}
