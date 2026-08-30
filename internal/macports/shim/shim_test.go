package shim

import (
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func shims(versions ...string) fstest.MapFS {
	fsys := fstest.MapFS{}
	for _, v := range versions {
		fsys["shims/"+v+".tcl"] = &fstest.MapFile{Data: []byte("# shim " + v + "\n")}
	}
	return fsys
}

func TestSelect(t *testing.T) {
	fsys := shims("2.9.0", "2.11.0", "2.12.6")
	cases := map[string]string{
		// The newest shim not newer than the installation.
		"2.12.6": "2.12.6",
		"2.12.9": "2.12.6",
		"2.11.4": "2.11.0",
		"2.10.0": "2.9.0",
		// Ordering is MacPorts', not lexical: 2.9 < 2.11.
		"2.9.9": "2.9.0",
		// Undetermined takes the newest.
		"": "2.12.6",
		// Older than every shim falls back to the oldest.
		"2.5.0": "2.9.0",
	}
	for version, want := range cases {
		script, named, err := Select(fsys, "shims", version)
		require.NoError(t, err, version)
		assert.Equal(t, want, named, "macports %q", version)
		assert.Contains(t, script, "# shim "+want)
	}
}

func TestSelectNoShims(t *testing.T) {
	_, _, err := Select(fstest.MapFS{"shims/README": &fstest.MapFile{}}, "shims", "2.12.6")
	require.ErrorIs(t, err, ErrNoShims)

	_, _, err = Select(fstest.MapFS{}, "nowhere", "2.12.6")
	require.Error(t, err)
}
