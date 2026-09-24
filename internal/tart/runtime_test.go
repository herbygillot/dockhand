package tart

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRuntimeResolvesAliasesWithoutCreatingDirectories(t *testing.T) {
	root := t.TempDir()
	real := filepath.Join(root, "real")
	require.NoError(t, os.Mkdir(real, 0700))
	alias := filepath.Join(root, "alias")
	require.NoError(t, os.Symlink(real, alias))
	t.Setenv("DOCKHAND_TART_HOME", filepath.Join(alias, "new", "home"))
	fromEnv, err := (Client{}).Resolve()
	require.NoError(t, err)
	explicit, err := (Client{Executable: "custom-tart", Home: filepath.Join(real, "new", "home")}).Resolve()
	require.NoError(t, err)
	require.Equal(t, fromEnv.Home, explicit.Home)
	require.Equal(t, "tart", fromEnv.Executable)
	require.Equal(t, "custom-tart", explicit.Executable)
	require.NoDirExists(t, filepath.Join(real, "new"))
	require.NoError(t, os.MkdirAll(explicit.Home, 0700))
	after, err := (Client{}).Resolve()
	require.NoError(t, err)
	require.Equal(t, fromEnv, after, "identity must remain stable after initialization")
}

// Dockhand's Tart home is its own, whatever the person's TART_HOME says;
// the person's is read only for counting and migration.
func TestRuntimeDefaultsToDockhandsOwnHome(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", root)
	t.Setenv("DOCKHAND_TART_HOME", "")
	t.Setenv("TART_HOME", filepath.Join(root, "persons"))
	c, err := (Client{}).Resolve()
	require.NoError(t, err)
	home, err := filepath.EvalSymlinks(root)
	require.NoError(t, err)
	require.Equal(t, filepath.Join(home, ".dockhand", "tart"), c.Home)
	require.NoDirExists(t, c.Home)
	personal, err := PersonalHome()
	require.NoError(t, err)
	require.Equal(t, filepath.Join(home, "persons"), personal)
	t.Setenv("TART_HOME", "")
	personal, err = PersonalHome()
	require.NoError(t, err)
	require.Equal(t, filepath.Join(home, ".tart"), personal)
}

func TestDirectoryResolutionRejectsBrokenLinksAndFiles(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "file")
	require.NoError(t, os.WriteFile(file, nil, 0600))
	broken := filepath.Join(root, "broken")
	require.NoError(t, os.Symlink(filepath.Join(root, "absent"), broken))
	for _, path := range []string{file, filepath.Join(file, "child"), broken, filepath.Join(broken, "child")} {
		_, err := CanonicalDirectory(path)
		require.Error(t, err, path)
	}
}
