package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAMissingFileIsEmptyAndCreatesNothing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	f, err := Load(path)
	require.NoError(t, err)
	require.Equal(t, File{}, f)
	_, err = os.Stat(path)
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestUnknownAndMalformedSettingsAreRefusedByName(t *testing.T) {
	dir := t.TempDir()
	unknown := filepath.Join(dir, "unknown.toml")
	require.NoError(t, os.WriteFile(unknown, []byte("worktrees = \"/w\"\n[serve]\nupdates = \"check\"\n"), 0o600))
	_, err := Load(unknown)
	require.ErrorContains(t, err, "unknown setting serve.updates")

	relative := filepath.Join(dir, "relative.toml")
	require.NoError(t, os.WriteFile(relative, []byte("worktrees = \"src/branches\"\n"), 0o600))
	_, err = Load(relative)
	require.ErrorContains(t, err, "not an absolute path")

	wrongType := filepath.Join(dir, "type.toml")
	require.NoError(t, os.WriteFile(wrongType, []byte("worktrees = 3\n"), 0o600))
	_, err = Load(wrongType)
	require.Error(t, err)
}

func TestHomeIsExpanded(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	path := filepath.Join(t.TempDir(), "config.toml")
	require.NoError(t, os.WriteFile(path, []byte("worktrees = \"~/src/macports-branches\"\n"), 0o600))
	f, err := Load(path)
	require.NoError(t, err)
	require.Equal(t, filepath.Join(home, "src", "macports-branches"), f.Worktrees)
}

func TestSetWorktreesKeepsTheRestOfTheFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "config.toml")
	require.NoError(t, SetWorktrees(path, "/src/branches"))
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, "worktrees = \"/src/branches\"\n", string(data))
	info, err := os.Stat(path)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), info.Mode().Perm())

	require.NoError(t, os.WriteFile(path, []byte("# mine\nworktrees = \"/old\"   \n"), 0o600))
	require.NoError(t, SetWorktrees(path, "/new"))
	data, err = os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, "# mine\nworktrees = \"/new\"\n", string(data), "the line is replaced in place")

	require.Error(t, SetWorktrees(path, "relative"), "only absolute directories")
}

func TestPathFollowsTheEnvironment(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv(PathVariable, "")
	path, err := Path()
	require.NoError(t, err)
	require.Equal(t, filepath.Join(home, ".dockhand", "config.toml"), path)
	t.Setenv(PathVariable, "/elsewhere/config.toml")
	path, err = Path()
	require.NoError(t, err)
	require.Equal(t, "/elsewhere/config.toml", path)
}
