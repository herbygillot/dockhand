package cmdtest

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/herbygillot/dockhand/internal/engine"
	"github.com/herbygillot/dockhand/internal/forge/forgetest"
	"github.com/herbygillot/dockhand/internal/store/sqlite/sqlitetest"
	"github.com/herbygillot/dockhand/internal/term"
	"github.com/herbygillot/dockhand/internal/testsupport"
	"github.com/stretchr/testify/require"
)

// World is a home directory, an upstream standing in for
// macports/macports-ports, and the person's clone of it at ~/src.
type World struct{ Home, Upstream, Clone string }

func NewWorld(t *testing.T) World {
	t.Helper()
	// What would ask GitHub, such as an update looking for other open pull
	// requests, asks a fake that knows of none, unless a test gives its own;
	// so does ports.macports.org's build history.
	Stands(t).Forge = func(*engine.Engine) engine.Forge { return forgetest.New("", "") }
	Stands(t).HTTPS = testsupport.HTTPSAnswers{}
	Stands(t).Builders = BuildHistories{}
	// Git reports resolved paths; on macOS the temporary directory is
	// reached through /var, a link to /private/var.
	root, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	w := World{Home: filepath.Join(root, "home"), Upstream: filepath.Join(root, "upstream")}
	w.Clone = filepath.Join(w.Home, "src", "macports-ports")
	testsupport.WriteFiles(t, w.Upstream, map[string]string{"_resources/port1.0/group/github-1.0.tcl": "# group\n", "textproc/jq/Portfile": "name jq\n"})
	testsupport.Git(t, w.Upstream, "init", "-q")
	testsupport.Git(t, w.Upstream, "add", "-A")
	testsupport.Git(t, w.Upstream, "commit", "-q", "-m", "init")
	require.NoError(t, os.MkdirAll(filepath.Dir(w.Clone), 0o755))
	testsupport.Git(t, root, "clone", "-q", w.Upstream, w.Clone)
	require.NoError(t, os.MkdirAll(w.Home, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(w.Home, ".gitconfig"), []byte("[user]\n\tname = Ada\n\temail = ada@example.org\n"), 0o644))
	// The default database is a copy of one the test binary migrated once.
	sqlitetest.Fresh(t, filepath.Join(w.Home, ".dockhand", "dockhand.db"))
	Setenv(t, "HOME", w.Home)
	Setenv(t, "DOCKHAND_UPSTREAM", w.Upstream)
	Setenv(t, "DOCKHAND_DB", "")
	Setenv(t, "DOCKHAND_CONFIG", "")
	Setenv(t, "MACPORTS_TREE", w.Clone)
	return w
}

// Settle bounds a wait for what a running serve or watch says
// (testsupport.Settle).
const Settle = testsupport.Settle

func Dockhand(t *testing.T, args ...string) (string, string, error) {
	t.Helper()
	var out, errs bytes.Buffer
	err := RunFor(t, t.Context(), args, term.Streams{In: strings.NewReader(""), Out: &out, Err: &errs})
	return out.String(), errs.String(), err
}

// cleanupStamp is the one checkout's cleanup stamp, kept for it beside the
// database.
func CleanupStamp(t *testing.T, home string) string {
	t.Helper()
	stamps, err := filepath.Glob(filepath.Join(home, ".dockhand", "serve", "*", "cleanup.stamp"))
	require.NoError(t, err)
	require.Len(t, stamps, 1)
	return stamps[0]
}
