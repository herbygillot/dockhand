package command

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/command/cmdtest"
	"github.com/herbygillot/dockhand/internal/term"
	"github.com/herbygillot/dockhand/internal/testsupport"
)

func TestSetupStartPathAndAdopt(t *testing.T) {
	t.Parallel()
	w := cmdtest.NewWorld(t)
	out, _, err := cmdtest.Dockhand(t, "setup", "--yes")
	require.NoError(t, err)
	require.Contains(t, out, "Using ~/src/macports-ports; it has no remote for macports/macports-ports")
	require.Regexp(t, `\n  Git          ✓ \d+\.\d+\.\d+ at \S+/git\n`, out)
	require.Contains(t, out, "worktrees in ~/Source/macports-branches")
	require.Contains(t, out, "Records      ~/.dockhand/dockhand.db")
	_, err = os.Stat(filepath.Join(w.Home, ".dockhand", "config.toml"))
	require.ErrorIs(t, err, os.ErrNotExist, "the default needs no configuration file")

	out, _, err = cmdtest.Dockhand(t, "start", "jq-update")
	require.NoError(t, err)
	require.Contains(t, out, "Created dockhand/jq-update from master ")
	require.Contains(t, out, "Directory: ~/Source/macports-branches/jq-update")
	require.Contains(t, out, `Next: cd "$(dockhand path jq-update)"`)

	out, _, err = cmdtest.Dockhand(t, "path", "jq-update")
	require.NoError(t, err)
	require.Equal(t, filepath.Join(w.Home, "Source", "macports-branches", "jq-update")+"\n", out, "path prints only the path")

	testsupport.Git(t, w.Clone, "switch", "-q", "-c", "update-jq")
	require.NoError(t, os.WriteFile(filepath.Join(w.Clone, "textproc/jq/Portfile"), []byte("name jq\nversion 1.8.1\n"), 0o644))
	testsupport.Git(t, w.Clone, "commit", "-q", "-am", "Update jq")
	out, _, err = cmdtest.Dockhand(t, "adopt")
	require.NoError(t, err)
	require.Regexp(t, `^Adopted update-jq: 1 commit above master [0-9a-f]{7}, changing jq\.\n$`, out)
	out, _, err = cmdtest.Dockhand(t, "adopt", "update-jq")
	require.NoError(t, err)
	require.Equal(t, "update-jq is already tracked.\n", out)

	_, _, err = cmdtest.Dockhand(t, "path", "fd-update")
	require.ErrorContains(t, err, "no tracked branch named fd-update")

	// A branch whose work is all uncommitted edits names the ports they
	// change, where it said "changing no ports yet".
	testsupport.Git(t, w.Clone, "switch", "-q", "-c", "edits-only", "master")
	require.NoError(t, os.WriteFile(filepath.Join(w.Clone, "textproc/jq/Portfile"), []byte("name jq\nversion 1.8.2\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(w.Clone, "_resources/port1.0/group/github-1.0.tcl"), []byte("# group, edited\n"), 0o644))
	out, _, err = cmdtest.Dockhand(t, "adopt")
	require.NoError(t, err)
	require.Regexp(t, `^Adopted edits-only: 0 commits above master [0-9a-f]{7}, with edits not yet committed to jq and _resources\.\n`, out)
}

func TestSetupRemembersAChosenWorktreesDirectory(t *testing.T) {
	t.Parallel()
	w := cmdtest.NewWorld(t)
	testsupport.Git(t, w.Clone, "remote", "add", "upstream", "https://github.com/macports/macports-ports.git")
	out, _, err := cmdtest.Dockhand(t, "setup", "--yes", "--worktrees", "~/work/branches")
	require.NoError(t, err)
	require.Contains(t, out, "upstream is macports/macports-ports (remote upstream)")
	require.Contains(t, out, "worktrees in ~/work/branches")
	data, err := os.ReadFile(filepath.Join(w.Home, ".dockhand", "config.toml"))
	require.NoError(t, err)
	require.Equal(t, `worktrees = "`+filepath.Join(w.Home, "work", "branches")+"\"\n", string(data))

	out, _, err = cmdtest.Dockhand(t, "start", "jq-update")
	require.NoError(t, err)
	require.Contains(t, out, "Directory: ~/work/branches/jq-update", "start follows the configuration")
}

func TestCommandsRefuseADirectoryThatIsNotAPortsTree(t *testing.T) {
	t.Parallel()
	cmdtest.NewWorld(t)
	_, _, err := cmdtest.Dockhand(t, "--tree", t.TempDir(), "start", "x")
	require.ErrorContains(t, err, "not in a Git checkout")
}

// setup refuses a Git older than dockhand works with, before recording
// anything, and says how to get a newer one.
func TestSetupRefusesAnOldGit(t *testing.T) {
	t.Parallel()
	w := cmdtest.NewWorld(t)
	old := filepath.Join(w.Home, "bin", "git")
	require.NoError(t, os.MkdirAll(filepath.Dir(old), 0o755))
	require.NoError(t, os.WriteFile(old, []byte("#!/bin/sh\necho 'git version 2.39.5 (Apple Git-154)'\n"), 0o755))
	cmdtest.Setenv(t, "GIT_BIN", old)
	_, _, err := cmdtest.Dockhand(t, "setup", "--yes")
	require.EqualError(t, err, "dockhand needs Git 2.40 or newer, and "+old+" is 2.39.5; install a newer one, such as with: sudo port install git, or name one with GIT_BIN")
	require.NoFileExists(t, filepath.Join(w.Home, ".dockhand", "dockhand.db"), "nothing was recorded")
}

// With MACPORTS_TREE naming the checkout, as a person's shell does, a
// command run in a branch's worktree works on the branch checked out
// there, as start's "cd" suggests; elsewhere, on the checkout named.
// MACPORTS_TREE used to win, so the worktree's branch was never "here".
func TestMacPortsTreeKeepsTheWorktreeYouAreIn(t *testing.T) {
	t.Parallel()
	w := cmdtest.NewWorld(t)
	cmdtest.Setenv(t, "MACPORTS_TREE", w.Clone)
	_, _, err := cmdtest.Dockhand(t, "start", "jq-update")
	require.NoError(t, err)
	worktree := filepath.Join(w.Home, "Source", "macports-branches", "jq-update")
	cmdtest.Chdir(t, worktree)
	out, _, err := cmdtest.Dockhand(t, "path")
	require.NoError(t, err)
	require.Equal(t, worktree+"\n", out)
	cmdtest.Chdir(t, w.Home)
	_, _, err = cmdtest.Dockhand(t, "path")
	require.ErrorContains(t, err, "this is your checkout's master, not one of dockhand's branches", "outside it, the checkout named, which has master out")
	require.Equal(t, 1, strings.Count(err.Error(), "name one with"), "the advice said once: %v", err)
}

// Where Tart is installed, the engine builds with it; the tests find no
// Tart otherwise, so this ran nowhere (the test plan's step 3).
func TestTartIsRegisteredWhereItIsFound(t *testing.T) {
	t.Parallel()
	cmdtest.NewWorld(t)
	s := *cmdtest.TestSettings(t)
	e, err := s.Open(t.Context())
	require.NoError(t, err)
	require.NotContains(t, e.Providers, "tart")
	e.Close()

	cmdtest.Stands(t).LookTart = func(string) (string, error) { return "/opt/homebrew/bin/tart", nil }
	s = *cmdtest.TestSettings(t)
	e, err = s.Open(t.Context())
	require.NoError(t, err)
	defer e.Close()
	require.Contains(t, e.Providers, "tart")
}

// setup registers the checkout and reports, and on a terminal offers
// what's missing, each declined by answering no (the command-line UX
// review's §6); rerun with -y, or in a script, it offers nothing. Its
// github part logs out, and v0.3.0's init, auth, and providers point
// here.
func TestSetupOffersWhatsMissing(t *testing.T) {
	t.Parallel()
	cmdtest.NewWorld(t)
	git, err := exec.LookPath("git")
	require.NoError(t, err)
	cmdtest.WithAuth(t, nil)
	// Git, and no Tart, so no image is offered.
	bin := t.TempDir()
	require.NoError(t, os.Symlink(git, filepath.Join(bin, "git")))
	cmdtest.Setenv(t, "PATH", bin)
	var out, errs bytes.Buffer
	err = cmdtest.RunFor(t, t.Context(), []string{"setup"}, term.Streams{In: strings.NewReader("\nn\n"), Out: &out, Err: &errs}.AsTerminal())
	require.NoError(t, err)
	require.Contains(t, errs.String(), "Keep branch worktrees in ")
	require.Contains(t, errs.String(), "? Log in to GitHub to submit pull requests and run checks in your fork? [Y/n] ")
	require.Contains(t, out.String(), "  Publishing   · not set up: dockhand setup github, when you're ready to submit\n")
	require.Contains(t, out.String(), "· maintainer: set maintainer to you as a Portfile's maintainers line writes you, such as \"@you\" for your GitHub login, or \"{example.org:you @you}\" with your email too, in ~/.dockhand/config.toml, for --mine, create, and serve's daily look\n")
	require.Contains(t, out.String(), "Next: dockhand setup github, then dockhand update <port>, or set maintainer in the configuration file for dockhand outdated --mine\n", "no maintainer, so outdated --mine isn't named alone")

	quiet, said, err := cmdtest.Dockhand(t, "setup", "-y")
	require.NoError(t, err)
	require.NotContains(t, said, "? ")
	require.NotContains(t, quiet, "· maintainer:")

	loggedOut, _, err := cmdtest.Dockhand(t, "setup", "github", "--logout")
	require.NoError(t, err)
	require.Contains(t, loggedOut, "No dockhand GitHub credentials are saved in the Keychain.")

	help, _, err := cmdtest.Dockhand(t, "--help")
	require.NoError(t, err)
	require.Contains(t, help, "setup ")
	for _, hidden := range []string{"\n  init ", "\n  auth ", "\n  providers "} {
		require.NotContains(t, help, hidden)
	}
	// v0.3.0's spellings are pointed at what replaced them, not run.
	for old, now := range map[string]string{"init": "setup", "auth": "setup", "providers": "setup", "restore": "undo"} {
		_, _, err = cmdtest.Dockhand(t, old)
		require.ErrorContains(t, err, "unknown command \""+old+"\"")
		require.ErrorContains(t, err, "Did you mean this?\n\t"+now+"\n", old)
	}
}

// A command naming a branch, run outside any ports checkout, finds the
// one checkout the database registered: dockhand path <branch> run
// elsewhere failed "not a MacPorts ports tree", though the branch's
// worktree was recorded (the exiv2 field notes). path alone still means
// here.
func TestABranchIsFoundFromOutsideTheTree(t *testing.T) {
	t.Parallel()
	w := cmdtest.NewWorld(t)
	_, _, err := cmdtest.Dockhand(t, "start", "jq-update")
	require.NoError(t, err)
	cmdtest.Setenv(t, "MACPORTS_TREE", "")
	cmdtest.Chdir(t, t.TempDir())
	out, _, err := cmdtest.Dockhand(t, "path", "jq-update")
	require.NoError(t, err)
	require.Equal(t, filepath.Join(w.Home, "Source", "macports-branches", "jq-update")+"\n", out)
	out, _, err = cmdtest.Dockhand(t, "status", "-b", "jq-update")
	require.NoError(t, err)
	require.Contains(t, out, "jq-update")
	_, _, err = cmdtest.Dockhand(t, "path")
	require.ErrorContains(t, err, "is not in a Git checkout", "path alone means here")
}
