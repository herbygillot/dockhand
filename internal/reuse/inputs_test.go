package reuse

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/model"
)

type trees map[string]string

func (t trees) Directories(_ context.Context, tree string, paths []string) (map[string]string, error) {
	found := map[string]string{}
	for _, path := range paths {
		if id, ok := t[tree+":"+path]; ok {
			found[path] = id
		}
	}
	return found, nil
}

// A build's inputs are what the provider saw active, with the revision's
// trees for every directory it read: its own, _resources, and each active
// port's.
func TestInputsNameEachDirectoryByItsTree(t *testing.T) {
	revision := trees{"t1:devel/libharbor": "11", "t1:_resources": "22", "t1:archivers/zlib": "33"}
	target := model.PlanTarget{ID: "libharbor", Target: model.Target{Name: "libharbor", Variants: map[string]bool{"doc": true}}, Directory: "devel/libharbor"}
	active := []model.ActivePort{
		{Name: "zlib", Spec: "@1.3.2_0", Directory: "archivers/zlib", Tree: "forged", Archive: "sha256:44"},
		{Name: "gone", Spec: "@1.0_0", Directory: "devel/gone", Archive: "sha256:55"},
	}
	inputs, err := Inputs(t.Context(), revision, "t1", "origin", target, active)
	require.NoError(t, err)
	require.Equal(t, model.TargetInputs{Environment: "origin", Directory: "devel/libharbor", Tree: "11", Resources: "22", Variants: map[string]bool{"doc": true},
		Active: []model.ActivePort{
			{Name: "gone", Spec: "@1.0_0", Directory: "devel/gone", Archive: "sha256:55"},
			{Name: "zlib", Spec: "@1.3.2_0", Directory: "archivers/zlib", Tree: "33", Archive: "sha256:44"},
		}}, inputs, "a tree is the revision's, never the provider's")
	require.False(t, inputs.Complete(), "a directory the revision doesn't hold")

	none, err := Inputs(t.Context(), revision, "t1", "origin", target, []model.ActivePort{})
	require.NoError(t, err)
	require.NotNil(t, none.Active, "a build that read no other port")
	require.True(t, none.Complete())
	unknown, err := Inputs(t.Context(), revision, "t1", "origin", target, nil)
	require.NoError(t, err)
	require.Nil(t, unknown.Active, "a provider that couldn't say which ports were active, as a command reporting only what it fetched")
	require.False(t, unknown.Complete())
}
