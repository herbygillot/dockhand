package model

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

// Inputs are identified by content, whatever order the active ports were
// seen in, and stand for another build's only when every one is
// identified.
func TestInputsAreKnownByContent(t *testing.T) {
	zlib := ActivePort{Name: "zlib", Spec: "@1.3.2_0", Directory: "archivers/zlib", Tree: "33", Archive: "sha256:44"}
	xz := ActivePort{Name: "xz", Spec: "@5.8.1_0", Directory: "archivers/xz", Tree: "55", Archive: "sha256:66"}
	one := NewTargetInputs("origin", "devel/libharbor", "11", "22", nil, []ActivePort{zlib, xz})
	other := NewTargetInputs("origin", "devel/libharbor", "11", "22", nil, []ActivePort{xz, zlib})
	require.Equal(t, one.Key(), other.Key())
	require.Equal(t, "xz", one.Active[0].Name, "in name order")
	require.True(t, one.Complete())
	require.NotEqual(t, one.Key(), NewTargetInputs("origin", "devel/libharbor", "11", "22", map[string]bool{"doc": true}, one.Active).Key(), "the variants asked for are an input")

	unkept := zlib
	unkept.Archive = ""
	require.False(t, NewTargetInputs("origin", "devel/libharbor", "11", "22", nil, []ActivePort{unkept}).Complete(), "an image that keeps no archives")
	require.False(t, NewTargetInputs("", "devel/libharbor", "11", "22", nil, []ActivePort{}).Complete(), "an environment that can't say what it is")
	require.True(t, NewTargetInputs("origin", "devel/libharbor", "11", "22", nil, []ActivePort{}).Complete(), "a build that read no other port")
	require.False(t, NewTargetInputs("origin", "devel/libharbor", "11", "22", nil, nil).Complete(), "a build whose provider couldn't say which were active")
}

// Inputs that name what of _resources their build read are complete
// without its tree, and keep, once read back, a build that loaded no
// PortGroup apart from one whose provider didn't say. Inputs recorded
// before them have the form, and so the key, they had.
func TestInputsMayNameWhatOfResourcesTheyRead(t *testing.T) {
	old := NewTargetInputs("origin", "devel/libharbor", "11", "22", nil, []ActivePort{})
	data, err := json.Marshal(old)
	require.NoError(t, err)
	require.Equal(t, `{"Environment":"origin","Directory":"devel/libharbor","Tree":"11","Resources":"22","Active":[]}`, string(data), "the form, and key, of inputs recorded before")

	read := NewTargetInputs("origin", "devel/libharbor", "11", "", nil, []ActivePort{})
	require.False(t, read.Complete(), "_resources named neither way")
	read.ResourcesRead = &ResourcesRead{Shared: map[string]ObjectID{"_resources/port1.0/compilers": "44"}, PortGroups: map[string]ObjectID{}}
	require.True(t, read.Complete(), "a build that loaded no PortGroup")
	data, err = json.Marshal(read)
	require.NoError(t, err)
	var back TargetInputs
	require.NoError(t, json.Unmarshal(data, &back))
	require.Equal(t, read, back)
	require.True(t, back.Complete(), "none loaded, read back, is still none")
	require.NotEqual(t, old.Key(), read.Key())

	read.ResourcesRead = &ResourcesRead{Shared: map[string]ObjectID{"_resources/port1.0/compilers": "44"}}
	require.False(t, read.Complete(), "which PortGroups it loaded isn't known")
	read.ResourcesRead = &ResourcesRead{PortGroups: map[string]ObjectID{}}
	require.False(t, read.Complete(), "what every port reads isn't known")
}
