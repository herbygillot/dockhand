package cmdtest

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/herbygillot/dockhand/internal/engine"
	"github.com/herbygillot/dockhand/internal/forge"
	"github.com/herbygillot/dockhand/internal/forge/forgetest"
	"github.com/herbygillot/dockhand/internal/testsupport"
	"github.com/stretchr/testify/require"
)

func WithGitHub(t *testing.T, w World) *forgetest.GitHub {
	fork := filepath.Join(filepath.Dir(w.Upstream), "fork.git")
	testsupport.Git(t, filepath.Dir(w.Upstream), "clone", "-q", "--bare", w.Upstream, fork)
	testsupport.Git(t, w.Clone, "remote", "add", "fork", fork)
	g := forgetest.New(w.Upstream, fork)
	g.Others = []forge.PullRequestSummary{{Number: 34777, Title: "jq: update to 1.8.0"}}
	Stands(t).Forge = func(*engine.Engine) engine.Forge { return g }
	return g
}

// asMaintainer names the person as the fixture's maintainers lines do,
// for --mine.
func AsMaintainer(t *testing.T, w World) {
	t.Helper()
	config := filepath.Join(w.Home, ".dockhand", "config.toml")
	data, err := os.ReadFile(config)
	if !errors.Is(err, fs.ErrNotExist) {
		require.NoError(t, err)
	}
	require.NoError(t, os.MkdirAll(filepath.Dir(config), 0o755))
	require.NoError(t, os.WriteFile(config, append([]byte("maintainer = \"@ada\"\n"), data...), 0o644))
}
