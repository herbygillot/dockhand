package model

import (
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
	require.False(t, NewTargetInputs("", "devel/libharbor", "11", "22", nil, nil).Complete(), "an environment that can't say what it is")
	require.True(t, NewTargetInputs("origin", "devel/libharbor", "11", "22", nil, nil).Complete(), "a build that read no other port")
}
