package command

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func gitRun(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "user.name=Test", "-c", "user.email=test@example.org", "-c", "init.defaultBranch=master"}, args...)...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))
	return strings.TrimSpace(string(out))
}

// world is a home directory, an upstream standing in for
// macports/macports-ports, and the person's clone of it at ~/src.
type world struct{ home, upstream, clone string }

func newWorld(t *testing.T) world {
	t.Helper()
	root := t.TempDir()
	w := world{home: filepath.Join(root, "home"), upstream: filepath.Join(root, "upstream")}
	w.clone = filepath.Join(w.home, "src", "macports-ports")
	for name, content := range map[string]string{"_resources/port1.0/group/github-1.0.tcl": "# group\n", "textproc/jq/Portfile": "name jq\n"} {
		require.NoError(t, os.MkdirAll(filepath.Join(w.upstream, filepath.Dir(name)), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(w.upstream, name), []byte(content), 0o644))
	}
	gitRun(t, w.upstream, "init", "-q")
	gitRun(t, w.upstream, "add", "-A")
	gitRun(t, w.upstream, "commit", "-q", "-m", "init")
	require.NoError(t, os.MkdirAll(filepath.Dir(w.clone), 0o755))
	gitRun(t, root, "clone", "-q", w.upstream, w.clone)
	t.Setenv("HOME", w.home)
	t.Setenv("DOCKHAND_UPSTREAM", w.upstream)
	t.Setenv("DOCKHAND_DB", "")
	t.Setenv("DOCKHAND_CONFIG", "")
	t.Setenv("MACPORTS_TREE", w.clone)
	return w
}

func dockhand(t *testing.T, args ...string) (string, string, error) {
	t.Helper()
	var out, errs bytes.Buffer
	err := Run(t.Context(), args, Streams{In: strings.NewReader(""), Out: &out, Err: &errs})
	return out.String(), errs.String(), err
}

func TestInitStartPathAndAdopt(t *testing.T) {
	w := newWorld(t)
	out, _, err := dockhand(t, "init")
	require.NoError(t, err)
	require.Contains(t, out, "Using ~/src/macports-ports; it has no remote for macports/macports-ports")
	require.Contains(t, out, "worktrees in ~/src/macports-branches")
	require.Contains(t, out, "Records      ~/.dockhand/dockhand.db")
	_, err = os.Stat(filepath.Join(w.home, ".dockhand", "config.toml"))
	require.ErrorIs(t, err, os.ErrNotExist, "the default needs no configuration file")

	out, _, err = dockhand(t, "start", "jq-update")
	require.NoError(t, err)
	require.Contains(t, out, "Created dockhand/jq-update from master ")
	require.Contains(t, out, "Directory: ~/src/macports-branches/jq-update")
	require.Contains(t, out, `Next: cd "$(dockhand path jq-update)"`)

	out, _, err = dockhand(t, "path", "jq-update")
	require.NoError(t, err)
	require.Equal(t, filepath.Join(w.home, "src", "macports-branches", "jq-update")+"\n", out, "path prints only the path")

	gitRun(t, w.clone, "switch", "-q", "-c", "update-jq")
	require.NoError(t, os.WriteFile(filepath.Join(w.clone, "textproc/jq/Portfile"), []byte("name jq\nversion 1.8.1\n"), 0o644))
	gitRun(t, w.clone, "commit", "-q", "-am", "Update jq")
	out, _, err = dockhand(t, "adopt")
	require.NoError(t, err)
	require.Regexp(t, `^Adopted update-jq: 1 commit above master [0-9a-f]{7}, changing jq\.\n$`, out)
	out, _, err = dockhand(t, "adopt", "update-jq")
	require.NoError(t, err)
	require.Equal(t, "update-jq is already tracked.\n", out)

	_, _, err = dockhand(t, "path", "fd-update")
	require.ErrorContains(t, err, "no tracked branch named fd-update")
}

func TestInitRemembersAChosenWorktreesDirectory(t *testing.T) {
	w := newWorld(t)
	gitRun(t, w.clone, "remote", "add", "upstream", "https://github.com/macports/macports-ports.git")
	out, _, err := dockhand(t, "init", "--worktrees", "~/work/branches")
	require.NoError(t, err)
	require.Contains(t, out, "upstream is macports/macports-ports (remote upstream)")
	require.Contains(t, out, "worktrees in ~/work/branches")
	data, err := os.ReadFile(filepath.Join(w.home, ".dockhand", "config.toml"))
	require.NoError(t, err)
	require.Equal(t, `worktrees = "`+filepath.Join(w.home, "work", "branches")+"\"\n", string(data))

	out, _, err = dockhand(t, "start", "jq-update")
	require.NoError(t, err)
	require.Contains(t, out, "Directory: ~/work/branches/jq-update", "start follows the configuration")
}

func TestCommandsRefuseADirectoryThatIsNotAPortsTree(t *testing.T) {
	newWorld(t)
	_, _, err := dockhand(t, "--tree", t.TempDir(), "start", "x")
	require.ErrorContains(t, err, "not in a Git checkout")
}

func TestDevNullIsNotATerminal(t *testing.T) {
	null, err := os.Open(os.DevNull)
	require.NoError(t, err)
	defer null.Close()
	require.False(t, Streams{In: null}.terminal(), "commands never prompt when input is /dev/null")
	require.False(t, Streams{In: strings.NewReader("y\n")}.terminal())
}

func TestAnEndedInputTakesTheDefault(t *testing.T) {
	answer, err := ask(Streams{In: strings.NewReader(""), Err: &bytes.Buffer{}}, "Keep? [Y/n] ")
	require.NoError(t, err)
	require.Empty(t, answer)
}
