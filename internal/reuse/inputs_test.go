package reuse

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/model"
)

// trees is a revision's directories, "tree:path" to its tree, and trees'
// entries, "ls:object" to "name type object" entries, one after another.
type trees map[string]string

func (t trees) ReadTree(_ context.Context, object string) ([]git.TreeEntry, error) {
	var entries []git.TreeEntry
	fields := strings.Fields(t["ls:"+object])
	for i := 0; i+2 < len(fields); i += 3 {
		entries = append(entries, git.TreeEntry{Name: fields[i], Type: fields[i+1], Object: fields[i+2]})
	}
	return entries, nil
}

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
	inputs, err := Inputs(t.Context(), revision, "t1", "origin", target, active, nil)
	require.NoError(t, err)
	require.Equal(t, model.TargetInputs{Environment: "origin", Directory: "devel/libharbor", Tree: "11", Resources: "22", Variants: map[string]bool{"doc": true},
		Active: []model.ActivePort{
			{Name: "gone", Spec: "@1.0_0", Directory: "devel/gone", Archive: "sha256:55"},
			{Name: "zlib", Spec: "@1.3.2_0", Directory: "archivers/zlib", Tree: "33", Archive: "sha256:44"},
		}}, inputs, "a tree is the revision's, never the provider's")
	require.False(t, inputs.Complete(), "a directory the revision doesn't hold")

	none, err := Inputs(t.Context(), revision, "t1", "origin", target, []model.ActivePort{}, nil)
	require.NoError(t, err)
	require.NotNil(t, none.Active, "a build that read no other port")
	require.True(t, none.Complete())
	unknown, err := Inputs(t.Context(), revision, "t1", "origin", target, nil, nil)
	require.NoError(t, err)
	require.Nil(t, unknown.Active, "a provider that couldn't say which ports were active, as a command reporting only what it fetched")
	require.False(t, unknown.Complete())
}

// Blob IDs of PortGroup files, and of the files every port reads.
var (
	cmakeBlob   = strings.Repeat("c", 40)
	golangBlob  = strings.Repeat("d", 40)
	cmakeEdited = strings.Repeat("e", 40)
)

// resources is a revision, tree, whose _resources holds the cmake and
// golang PortGroups at blobs, the compiler lists at a tree, compilers, and
// the variant descriptions, and whose sysutils/jq is at tree 11.
func resources(tree, cmake, golang, compilers string) trees {
	return trees{
		tree + ":sysutils/jq": "11", tree + ":_resources": tree + "r",
		"ls:" + tree + "r": "macports1.0 tree m1 port1.0 tree " + tree + "p",
		"ls:" + tree + "p": "compilers tree " + compilers + " group tree " + tree + "g variant_descriptions.conf blob v1",
		"ls:" + tree + "g": "cmake-1.1.tcl blob " + cmake + " golang-1.0.tcl blob " + golang,
	}
}

// A build whose provider said which PortGroups it loaded names those, by
// the blobs it recorded, and every entry of _resources beside the
// PortGroups, by the revision's objects, in place of _resources' tree. One
// that didn't say, or named a file that isn't one of the tree's
// PortGroups, names the tree, as before.
func TestInputsNameThePortGroupsTheBuildLoaded(t *testing.T) {
	revision := resources("t1", cmakeBlob, golangBlob, "c1")
	target := model.PlanTarget{ID: "jq", Target: model.Target{Name: "jq"}, Directory: "sysutils/jq"}
	inputs, err := Inputs(t.Context(), revision, "t1", "origin", target, []model.ActivePort{}, map[string]model.ObjectID{"_resources/port1.0/group/cmake-1.1.tcl": model.ObjectID(cmakeBlob)})
	require.NoError(t, err)
	require.Empty(t, inputs.Resources, "not the whole tree")
	require.Equal(t, &model.ResourcesRead{
		Shared:     map[string]model.ObjectID{"_resources/macports1.0": "m1", "_resources/port1.0/compilers": "c1", "_resources/port1.0/variant_descriptions.conf": "v1"},
		PortGroups: map[string]model.ObjectID{"_resources/port1.0/group/cmake-1.1.tcl": model.ObjectID(cmakeBlob)},
	}, inputs.ResourcesRead)
	require.True(t, inputs.Complete())

	none, err := Inputs(t.Context(), revision, "t1", "origin", target, []model.ActivePort{}, map[string]model.ObjectID{})
	require.NoError(t, err)
	require.Equal(t, map[string]model.ObjectID{}, none.ResourcesRead.PortGroups, "a build that loaded none")
	require.True(t, none.Complete())

	for why, sourced := range map[string]map[string]model.ObjectID{
		"the provider didn't say":   nil,
		"a file of no PortGroup":    {"_resources/port1.0/compilers/gcc_compilers.tcl": model.ObjectID(cmakeBlob)},
		"a blob that isn't a blob":  {"_resources/port1.0/group/cmake-1.1.tcl": "forged"},
		"a file outside _resources": {"sysutils/jq/files/local-1.0.tcl": model.ObjectID(cmakeBlob)},
	} {
		whole, err := Inputs(t.Context(), revision, "t1", "origin", target, []model.ActivePort{}, sourced)
		require.NoError(t, err)
		require.Equal(t, model.ObjectID("t1r"), whole.Resources, why)
		require.Nil(t, whole.ResourcesRead, why)
	}
}
