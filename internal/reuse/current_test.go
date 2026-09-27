package reuse

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/model"
)

// A result stands for a build now only while everything it recorded
// reading is the same: the environment, the variants, and each directory's
// tree, its own, _resources, and every active port's (decision 28).
func TestARecordedBuildStandsWhileItsInputsDo(t *testing.T) {
	target := model.PlanTarget{ID: "jq", Target: model.Target{Name: "jq"}, Directory: "sysutils/jq"}
	recorded := model.NewTargetInputs("origin a", "sysutils/jq", "11", "22", nil,
		[]model.ActivePort{{Name: "oniguruma6", Spec: "@6.9.10_0", Directory: "devel/oniguruma6", Tree: "33", Archive: "sha256:44"}})
	require.Equal(t, []string{"sysutils/jq", "_resources", "devel/oniguruma6"}, Paths(recorded))
	now := map[string]model.ObjectID{"sysutils/jq": "11", "_resources": "22", "devel/oniguruma6": "33"}
	require.True(t, Current(recorded, "origin a", target, now))

	require.False(t, Current(recorded, "origin b", target, now), "the environment was made again")
	require.False(t, Current(recorded, "", target, now), "the environment can't say what it is")
	for path, why := range map[string]string{"sysutils/jq": "the port changed", "_resources": "a PortGroup changed", "devel/oniguruma6": "a dependency changed"} {
		changed := map[string]model.ObjectID{"sysutils/jq": "11", "_resources": "22", "devel/oniguruma6": "33"}
		changed[path] = "99"
		require.False(t, Current(recorded, "origin a", target, changed), why)
	}
	gone := map[string]model.ObjectID{"sysutils/jq": "11", "_resources": "22"}
	require.False(t, Current(recorded, "origin a", target, gone), "a dependency's directory is gone")
	variant := target
	variant.Target.Variants = map[string]bool{"doc": true}
	require.False(t, Current(recorded, "origin a", variant, now), "other variants asked for")

	incomplete := recorded
	incomplete.Active = []model.ActivePort{{Name: "oniguruma6", Spec: "@6.9.10_0", Directory: "devel/oniguruma6", Tree: "33"}}
	require.False(t, Current(incomplete, "origin a", target, now), "an archive wasn't known, so the inputs are incomplete")
}
