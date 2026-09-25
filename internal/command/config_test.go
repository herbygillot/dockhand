package command

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestConfigShowsWhatIsInEffect(t *testing.T) {
	w := newWorld(t)
	out, _, err := dockhand(t, "config")
	require.NoError(t, err)
	require.Contains(t, out, "File      ~/.dockhand/config.toml\nDatabase  ~/.dockhand/dockhand.db\n")
	require.Contains(t, out, "submit.rerequest_review  ask  (default)\n")
	require.Contains(t, out, "cleanup.after            7d  (default)\n")

	require.NoError(t, os.MkdirAll(filepath.Join(w.home, ".dockhand"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(w.home, ".dockhand", "config.toml"), []byte("maintainer = \"{@ada example.org:ada} openmaintainer\"\n[cleanup]\nautomatic = false\n"), 0o644))
	out, _, err = dockhand(t, "config")
	require.NoError(t, err)
	require.Contains(t, out, "maintainer               {@ada example.org:ada} openmaintainer\n")
	require.Contains(t, out, "cleanup.automatic        false\n")

	result, err := jsonOf(t, "config")
	require.NoError(t, err)
	require.Equal(t, "maintainer", dig(t, result.Result, "settings", 1, "key"))
	require.Equal(t, "file", dig(t, result.Result, "settings", 1, "source"))

	require.NoError(t, os.WriteFile(filepath.Join(w.home, ".dockhand", "config.toml"), []byte("maintainer = \"{@ada\"\n"), 0o644))
	_, _, err = dockhand(t, "config")
	require.ErrorContains(t, err, "maintainer: \"{@ada\" leaves a group open")
}
