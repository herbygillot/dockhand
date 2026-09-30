package command

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/engine"
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
	// What would ask GitHub, such as an update looking for other open pull
	// requests, asks a fake that knows of none, unless a test gives its own.
	testForge = func(*engine.Engine) engine.Forge { return &fakeGitHub{} }
	testHTTPS = httpsAnswers{}
	t.Cleanup(func() { testForge, testHTTPS = nil, nil })
	// Git reports resolved paths; on macOS the temporary directory is
	// reached through /var, a link to /private/var.
	root, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
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
	require.NoError(t, os.MkdirAll(w.home, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(w.home, ".gitconfig"), []byte("[user]\n\tname = Ada\n\temail = ada@example.org\n"), 0o644))
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
	require.Regexp(t, `\n  Git          ✓ \d+\.\d+\.\d+ at \S+/git\n`, out)
	require.Contains(t, out, "worktrees in ~/Source/macports-branches")
	require.Contains(t, out, "Records      ~/.dockhand/dockhand.db")
	_, err = os.Stat(filepath.Join(w.home, ".dockhand", "config.toml"))
	require.ErrorIs(t, err, os.ErrNotExist, "the default needs no configuration file")

	out, _, err = dockhand(t, "start", "jq-update")
	require.NoError(t, err)
	require.Contains(t, out, "Created dockhand/jq-update from master ")
	require.Contains(t, out, "Directory: ~/Source/macports-branches/jq-update")
	require.Contains(t, out, `Next: cd "$(dockhand path jq-update)"`)

	out, _, err = dockhand(t, "path", "jq-update")
	require.NoError(t, err)
	require.Equal(t, filepath.Join(w.home, "Source", "macports-branches", "jq-update")+"\n", out, "path prints only the path")

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

// init refuses a Git older than dockhand works with, before recording
// anything, and says how to get a newer one.
func TestInitRefusesAnOldGit(t *testing.T) {
	w := newWorld(t)
	old := filepath.Join(w.home, "bin", "git")
	require.NoError(t, os.MkdirAll(filepath.Dir(old), 0o755))
	require.NoError(t, os.WriteFile(old, []byte("#!/bin/sh\necho 'git version 2.39.5 (Apple Git-154)'\n"), 0o755))
	t.Setenv("GIT_BIN", old)
	_, _, err := dockhand(t, "init")
	require.EqualError(t, err, "dockhand needs Git 2.40 or newer, and "+old+" is 2.39.5; install a newer one, such as with: sudo port install git, or name one with GIT_BIN")
	require.NoFileExists(t, filepath.Join(w.home, ".dockhand", "dockhand.db"), "nothing was recorded")
}

// With MACPORTS_TREE naming the checkout, as a person's shell does, a
// command run in a branch's worktree works on the branch checked out
// there, as start's "cd" suggests; elsewhere, on the checkout named.
// MACPORTS_TREE used to win, so the worktree's branch was never "here".
func TestMacPortsTreeKeepsTheWorktreeYouAreIn(t *testing.T) {
	w := newWorld(t)
	t.Setenv("MACPORTS_TREE", w.clone)
	_, _, err := dockhand(t, "start", "jq-update")
	require.NoError(t, err)
	worktree := filepath.Join(w.home, "Source", "macports-branches", "jq-update")
	t.Chdir(worktree)
	out, _, err := dockhand(t, "path")
	require.NoError(t, err)
	require.Equal(t, worktree+"\n", out)
	t.Chdir(w.home)
	_, _, err = dockhand(t, "path")
	require.ErrorContains(t, err, "master is not tracked", "outside it, the checkout named, which has master out")
}

// httpsAnswers stands in for asking URLs over HTTPS: those it holds true
// answer.
type httpsAnswers map[string]bool

func (a httpsAnswers) Answers(_ context.Context, url string) bool { return a[url] }
