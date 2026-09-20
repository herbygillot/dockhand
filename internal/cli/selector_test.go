package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// A token that names a place on disk resolves to the port standing there; a
// token that names a port or a snapshot-relative path is left alone, so the
// spelling every verb has always taken keeps its meaning.
func TestPortNameResolvesFilesystemTokensOnly(t *testing.T) {
	tree := t.TempDir()
	portdir := filepath.Join(tree, "devel", "bashunit")
	require.NoError(t, os.MkdirAll(portdir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(portdir, "Portfile"), []byte("PortSystem 1.0\n"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(tree, "devel", "empty"), 0o755))
	t.Chdir(portdir)

	for token, want := range map[string]string{
		".":                                   "bashunit",
		"./":                                  "bashunit",
		"./Portfile":                          "bashunit",
		"..":                                  "",
		filepath.Join(tree, "devel/bashunit"): "bashunit",
		filepath.Join(tree, "devel/bashunit/Portfile"): "bashunit",
		"bashunit":                "bashunit",
		"devel/bashunit":          "devel/bashunit",
		"devel/bashunit/Portfile": "devel/bashunit/Portfile",
		"rb33-mustache":           "rb33-mustache",
	} {
		name, err := portName(token)
		if want == "" {
			require.Error(t, err, token)
			require.Contains(t, err.Error(), "holds no Portfile", token)
			continue
		}
		require.NoError(t, err, token)
		require.Equal(t, want, name, token)
	}

	t.Chdir(filepath.Join(tree, "devel", "empty"))
	_, err := portName(".")
	require.ErrorContains(t, err, "holds no Portfile")
}
