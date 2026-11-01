package checks

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/model"
)

// An unmet minimum Xcode says what the environment has, where the plan
// kept it: "needs Xcode 26.0, which Tart macOS 15 hasn't" left the person
// to find 16.4 (batch 28's leftover).
func TestAnUnmetMinimumSaysWhatTheEnvironmentHas(t *testing.T) {
	t.Parallel()
	unmet := model.Unmet{Target: "sand-runner", Needs: model.RequiresXcodeVersion("26.0"), Has: "16.4"}
	require.Equal(t, "needs Xcode 26.0 (Xcode 16.4 there)", UnmetWords(unmet))
	unmet.Has, unmet.Through = "none", "sand-runner"
	require.Equal(t, "needs Xcode 26.0 (only the Command Line Tools there), through sand-runner", UnmetWords(unmet))
	unmet.Has = ""
	require.Equal(t, "needs Xcode 26.0, through sand-runner", UnmetWords(unmet), "a plan made before it was kept")
}

// A plan nothing can be built from says why: each unresolved target and
// its reason, or that it builds nothing.
func TestAnUnrunnablePlanSaysWhy(t *testing.T) {
	t.Parallel()
	require.Equal(t, "it builds nothing", UnresolvedWords(model.Plan{}))
	require.Equal(t, "jq: no Portfile; fd: excluded on arm64", UnresolvedWords(model.Plan{Unresolved: []model.Unresolved{
		{Target: model.Target{Name: "jq"}, Reason: "no Portfile"}, {Target: model.Target{Name: "fd"}, Reason: "excluded on arm64"}}}))
}
