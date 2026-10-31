package cmdtest

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
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

// RefusingSSH points the fork's push URL at GitHub over SSH and has ssh
// refuse this Mac's key, in ssh's own words, as a fork that refuses the
// key does.
func RefusingSSH(t *testing.T, w World, g *forgetest.GitHub) {
	t.Helper()
	for _, name := range strings.Fields(testsupport.Git(t, w.Clone, "remote")) {
		if strings.TrimSpace(testsupport.Git(t, w.Clone, "remote", "get-url", "--push", name)) == g.Fork {
			testsupport.Git(t, w.Clone, "remote", "set-url", "--push", name, "ssh://git@github.com/ada/macports-ports.git")
			g.Fork = "ssh://git@github.com/ada/macports-ports.git"
		}
	}
	refusing := filepath.Join(t.TempDir(), "ssh")
	testsupport.WriteExecutable(t, refusing, "#!/bin/sh\necho 'git@github.com: Permission denied (publickey).' >&2\nexit 255\n")
	Setenv(t, "GIT_SSH_COMMAND", refusing)
}
