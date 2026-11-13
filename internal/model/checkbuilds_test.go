package model

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// Port names select build identities by the same rule in planning and
// reuse, including subports and an explicit selection (#236).
func TestCheckBuildsResolvesTheConsumedBuild(t *testing.T) {
	ordinary := PlanTarget{ID: "lib", Target: Target{Name: "lib"}}
	docs := PlanTarget{ID: "lib +docs", Target: Target{Name: "lib", Variants: map[string]bool{"docs": true}}}
	tests := PlanTarget{ID: "lib +tests", Target: Target{Name: "lib", Variants: map[string]bool{"tests": true}}}
	builds := CheckBuilds{Targets: []PlanTarget{ordinary, docs, tests}}
	id, err := builds.Resolve("cli", "LIB")
	require.NoError(t, err)
	require.Equal(t, ordinary.ID, id)
	builds.Dependencies = map[TargetID][]TargetID{"cli": {docs.ID}}
	id, err = builds.Resolve("cli", "lib")
	require.NoError(t, err)
	require.Equal(t, docs.ID, id)
	builds.Targets = []PlanTarget{docs, tests}
	builds.Dependencies = nil
	_, err = builds.Resolve("cli", "lib")
	require.ErrorContains(t, err, "lib +docs")
	require.ErrorContains(t, err, "lib +tests")
	builds.Targets = []PlanTarget{{ID: "tools-cli +docs", Target: Target{Name: "tools", Subport: "tools-cli", Variants: map[string]bool{"docs": true}}}}
	id, err = builds.Resolve("cli", "tools-cli")
	require.NoError(t, err)
	require.Equal(t, TargetID("tools-cli +docs"), id)
	id, err = builds.Resolve("cli", "tools")
	require.NoError(t, err)
	require.Empty(t, id)
}

// Input keys change only where consumption of an in-check build is
// recorded, and unknown or changed consumption stands for nothing (#236).
func TestInputsRequireTheSameConsumedBuilds(t *testing.T) {
	builds := CheckBuilds{
		Targets:      []PlanTarget{{ID: "b +docs", Target: Target{Name: "b", Variants: map[string]bool{"docs": true}}}},
		Dependencies: map[TargetID][]TargetID{"a": {"b +docs"}},
	}
	inputs := TargetInputs{Active: []ActivePort{{Name: "B"}}}
	old := inputs.Key()
	require.False(t, builds.SameInputs("a", inputs), "old inputs do not establish consumption")
	var err error
	inputs.Targets, err = builds.Consumed("a", inputs.Active)
	require.NoError(t, err)
	require.Equal(t, map[string]TargetID{"b": "b +docs"}, inputs.Targets)
	require.NotEqual(t, old, inputs.Key())
	require.True(t, builds.SameInputs("a", inputs))
	inputs.Targets["b"] = "b"
	require.False(t, builds.SameInputs("a", inputs))
	_, err = builds.Consumed("a", nil)
	require.Error(t, err, "the planned prerequisite was not reported active")
	inputs = TargetInputs{Active: []ActivePort{}}
	old = inputs.Key()
	inputs.Targets, err = (CheckBuilds{}).Consumed("a", inputs.Active)
	require.NoError(t, err)
	require.Equal(t, old, inputs.Key(), "no in-check consumption keeps its key")
}
