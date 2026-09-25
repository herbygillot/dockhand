package engine

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/store"
)

func TestMain(m *testing.M) {
	// Keep the tests' transient directories out of the user's own.
	temp, err := os.MkdirTemp("", "dockhand-test-")
	if err != nil {
		panic(err)
	}
	os.Setenv("TMPDIR", temp)
	// Commits tidy writes need an identity, whatever the machine has.
	identity := filepath.Join(temp, "gitconfig")
	if err := os.WriteFile(identity, []byte("[user]\n\tname = Test\n\temail = test@example.org\n"), 0o644); err != nil {
		panic(err)
	}
	os.Setenv("GIT_CONFIG_GLOBAL", identity)
	code := m.Run()
	os.RemoveAll(temp)
	os.Exit(code)
}

var at = time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)

func run(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "user.name=Test", "-c", "user.email=test@example.org", "-c", "init.defaultBranch=master"}, args...)...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))
	return strings.TrimSpace(string(out))
}

func write(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for name, content := range files {
		require.NoError(t, os.MkdirAll(filepath.Join(root, filepath.Dir(name)), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(root, name), []byte(content), 0o644))
	}
}

type fixture struct {
	upstream string // stands in for macports/macports-ports
	clone    string // the person's checkout
	options  Options
}

// setup makes an upstream ports tree, clones it as the person's checkout,
// then moves upstream master on, so a stale local master is detectable.
func setup(t *testing.T) fixture {
	t.Helper()
	// Git reports resolved paths; on macOS the temporary directory is
	// reached through /var, a link to /private/var.
	root, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	f := fixture{upstream: filepath.Join(root, "upstream"), clone: filepath.Join(root, "src", "macports-ports")}
	require.NoError(t, os.MkdirAll(f.upstream, 0o755))
	run(t, f.upstream, "init", "-q")
	write(t, f.upstream, map[string]string{
		"_resources/port1.0/group/github-1.0.tcl": "# group\n",
		"textproc/jq/Portfile":                    "name jq\nversion 1.7.1\n",
		"devel/libharbor/Portfile":                "name libharbor\n",
	})
	run(t, f.upstream, "add", "-A")
	run(t, f.upstream, "commit", "-q", "-m", "init")
	run(t, root, "clone", "-q", f.upstream, f.clone)
	write(t, f.upstream, map[string]string{"devel/libharbor/Portfile": "name libharbor\nversion 2\n"})
	run(t, f.upstream, "commit", "-q", "-am", "libharbor: update to 2")
	f.options = Options{Tree: f.clone, Database: filepath.Join(root, "home", ".dockhand", "dockhand.db"), Upstream: f.upstream, Now: func() time.Time { return at }}
	return f
}

func (f fixture) open(t *testing.T) *Engine {
	t.Helper()
	e, err := Open(t.Context(), f.options)
	require.NoError(t, err)
	t.Cleanup(func() { e.Close() })
	return e
}

func (f fixture) upstreamMaster(t *testing.T) model.ObjectID {
	return model.ObjectID(run(t, f.upstream, "rev-parse", "master"))
}

func files(t *testing.T, root string) []string {
	t.Helper()
	var found []string
	require.NoError(t, filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Name() == ".git" {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.IsDir() {
			rel, _ := filepath.Rel(root, path)
			found = append(found, rel)
		}
		return nil
	}))
	sort.Strings(found)
	return found
}

func TestOpenRefusesADirectoryThatIsNotAPortsTree(t *testing.T) {
	root := t.TempDir()
	run(t, root, "init", "-q")
	write(t, root, map[string]string{"README.md": "dockhand\n", "internal/cli/main.go": "package cli\n"})
	run(t, root, "add", "-A")
	run(t, root, "commit", "-q", "-m", "init")
	database := filepath.Join(t.TempDir(), "dockhand.db")
	_, err := Open(t.Context(), Options{Tree: root, Database: database})
	require.ErrorIs(t, err, ErrNotPortsTree)
	require.ErrorContains(t, err, "--tree")
	_, err = os.Stat(database)
	require.ErrorIs(t, err, os.ErrNotExist, "nothing is recorded for it")

	_, err = Open(t.Context(), Options{Tree: t.TempDir(), Database: database})
	require.ErrorContains(t, err, "not in a Git checkout")
}

func TestStartMakesASparseWorktreeFromFreshMaster(t *testing.T) {
	f := setup(t)
	e := f.open(t)
	require.Equal(t, filepath.Join(filepath.Dir(f.clone), "macports-branches"), e.Worktrees(), "beside the clone")

	branch, err := e.Start(t.Context(), StartRequest{Name: "jq-update"})
	require.NoError(t, err)
	require.Equal(t, "dockhand/jq-update", branch.Name)
	require.Equal(t, "jq-update", branch.ShortName())
	require.Equal(t, f.upstreamMaster(t), branch.Base, "upstream's master, not the clone's stale one")
	require.True(t, branch.Managed)
	require.Equal(t, filepath.Join(e.Worktrees(), "jq-update"), branch.Worktree)
	require.Equal(t, []string{"_resources/port1.0/group/github-1.0.tcl"}, files(t, branch.Worktree))
	require.Equal(t, string(branch.Base), run(t, branch.Worktree, "rev-parse", "HEAD"))
	require.Len(t, files(t, f.clone), 3, "the person's checkout is untouched")
	require.Equal(t, "master", run(t, f.clone, "branch", "--show-current"))

	_, err = e.Start(t.Context(), StartRequest{Name: "dockhand/jq-update"})
	require.ErrorContains(t, err, "already a tracked branch")
	_, err = e.Start(t.Context(), StartRequest{Name: "bad name"})
	require.ErrorContains(t, err, "not a usable branch name")

	run(t, f.clone, "branch", "dockhand/mine")
	_, err = e.Start(t.Context(), StartRequest{Name: "mine"})
	require.ErrorIs(t, err, git.ErrBranchExists)
	require.ErrorContains(t, err, "dockhand adopt dockhand/mine")

	require.NoError(t, os.MkdirAll(filepath.Join(e.Worktrees(), "occupied"), 0o755))
	_, err = e.Start(t.Context(), StartRequest{Name: "occupied"})
	require.ErrorContains(t, err, "already exists")
	require.Empty(t, run(t, f.clone, "branch", "--list", "dockhand/occupied"), "a refusal creates no branch")

	require.NoError(t, e.Store.View(t.Context(), e.Repository, func(r store.Reader) error {
		events, err := r.Events(0, 0)
		require.NoError(t, err)
		require.Equal(t, "branch.start", events[len(events)-1].Kind)
		return nil
	}))
}

func TestAFailedStartLeavesNothingBehind(t *testing.T) {
	f := setup(t)
	blocker := filepath.Join(t.TempDir(), "not-a-directory")
	require.NoError(t, os.WriteFile(blocker, []byte("x"), 0o644))
	f.options.Worktrees = filepath.Join(blocker, "branches")
	e := f.open(t)
	_, err := e.Start(t.Context(), StartRequest{Name: "jq-update"})
	require.Error(t, err)
	require.Empty(t, run(t, f.clone, "branch", "--list", "dockhand/jq-update"), "the branch it created is deleted again")

	f.options.Worktrees, f.options.Upstream = "", filepath.Join(t.TempDir(), "no-such-upstream")
	e = f.open(t)
	_, err = e.Start(t.Context(), StartRequest{Name: "jq-update"})
	require.ErrorContains(t, err, "fetching master")
	require.Empty(t, run(t, f.clone, "branch", "--list", "dockhand/jq-update"))
}

func TestStartHereUsesThePersonsCheckoutOnlyWhenNothingWouldBeDisplaced(t *testing.T) {
	f := setup(t)
	e := f.open(t)
	write(t, f.clone, map[string]string{"textproc/jq/Portfile": "name jq\nversion 1.8.1\n"})
	_, err := e.Start(t.Context(), StartRequest{Name: "jq-here", Here: true})
	require.ErrorContains(t, err, "uncommitted changes to textproc/jq/Portfile")
	require.Empty(t, run(t, f.clone, "branch", "--list", "dockhand/jq-here"))

	run(t, f.clone, "checkout", "-q", "--", ".")
	write(t, f.clone, map[string]string{"notes.txt": "untracked work is not displaced\n"})
	branch, err := e.Start(t.Context(), StartRequest{Name: "jq-here", Here: true})
	require.NoError(t, err)
	require.False(t, branch.Managed)
	require.Equal(t, f.clone, branch.Worktree)
	require.Equal(t, "dockhand/jq-here", run(t, f.clone, "branch", "--show-current"))
	require.Equal(t, f.upstreamMaster(t), branch.Base)
}

func TestAdoptTracksABranchAsItStands(t *testing.T) {
	f := setup(t)
	run(t, f.clone, "switch", "-q", "-c", "update-jq")
	write(t, f.clone, map[string]string{"textproc/jq/Portfile": "name jq\nversion 1.8.1\n"})
	run(t, f.clone, "commit", "-q", "-am", "Update jq")
	write(t, f.clone, map[string]string{"textproc/jq/files/patch-fix.diff": "--- a\n", "_resources/port1.0/group/github-1.0.tcl": "# group, fixed\n"})
	run(t, f.clone, "add", "-A")
	run(t, f.clone, "commit", "-q", "-m", "oops")
	head := run(t, f.clone, "rev-parse", "HEAD")

	e := f.open(t)
	adoption, err := e.Adopt(t.Context(), AdoptRequest{})
	require.NoError(t, err)
	require.False(t, adoption.Already)
	require.Equal(t, "update-jq", adoption.Branch.Name)
	require.Equal(t, 2, adoption.Commits)
	require.Equal(t, []string{"textproc/jq"}, adoption.Scope.Ports)
	require.Equal(t, []string{"jq"}, adoption.Scope.PortNames())
	require.True(t, adoption.Scope.Resources)
	require.Equal(t, f.clone, adoption.Branch.Worktree)
	require.False(t, adoption.Branch.Managed)
	require.Equal(t, run(t, f.clone, "merge-base", "HEAD", string(f.upstreamMaster(t))), string(adoption.Branch.Base))
	require.Equal(t, head, run(t, f.clone, "rev-parse", "HEAD"), "adopting moves nothing")

	again, err := e.Adopt(t.Context(), AdoptRequest{Branch: "update-jq"})
	require.NoError(t, err)
	require.True(t, again.Already)
	require.Equal(t, adoption.Branch.ID, again.Branch.ID)

	run(t, f.clone, "branch", "elsewhere", "master")
	elsewhere, err := e.Adopt(t.Context(), AdoptRequest{Branch: "elsewhere"})
	require.NoError(t, err)
	require.Empty(t, elsewhere.Branch.Worktree, "a branch not checked out has no worktree")
	require.Zero(t, elsewhere.Commits)

	_, err = e.Adopt(t.Context(), AdoptRequest{Branch: "master"})
	require.ErrorContains(t, err, "what branches start from")
	_, err = e.Adopt(t.Context(), AdoptRequest{Branch: "absent"})
	require.ErrorContains(t, err, "no local branch absent")
	run(t, f.clone, "switch", "-q", "--detach")
	_, err = e.Adopt(t.Context(), AdoptRequest{})
	require.ErrorContains(t, err, "not on a branch")
}

func TestPathAndResolve(t *testing.T) {
	f := setup(t)
	e := f.open(t)
	branch, err := e.Start(t.Context(), StartRequest{Name: "jq-update"})
	require.NoError(t, err)
	for _, selector := range []string{"jq-update", "dockhand/jq-update"} {
		path, err := e.Path(t.Context(), selector)
		require.NoError(t, err)
		require.Equal(t, branch.Worktree, path)
	}
	_, err = e.Path(t.Context(), "fd-update")
	require.ErrorIs(t, err, ErrNoBranch)

	// Opened inside the sparse worktree, the context is its branch.
	inside := f
	inside.options.Tree = filepath.Join(branch.Worktree, "_resources")
	here := inside.open(t)
	require.Equal(t, e.Repository, here.Repository, "a linked worktree is the same repository")
	path, err := here.Path(t.Context(), "")
	require.NoError(t, err)
	require.Equal(t, branch.Worktree, path)

	_, err = e.Path(t.Context(), "")
	require.ErrorContains(t, err, "master is not tracked")

	run(t, f.clone, "branch", "parked", "master")
	_, err = e.Adopt(t.Context(), AdoptRequest{Branch: "parked"})
	require.NoError(t, err)
	_, err = e.Path(t.Context(), "parked")
	require.ErrorContains(t, err, "not checked out anywhere")

	require.NoError(t, e.Repo.RemoveWorktree(context.Background(), branch.Worktree))
	_, err = e.Path(t.Context(), "jq-update")
	require.ErrorContains(t, err, "is gone")
}

func TestUpstreamRemoteIsFoundByURL(t *testing.T) {
	f := setup(t)
	e := f.open(t)
	remote, err := e.UpstreamRemote(t.Context())
	require.NoError(t, err)
	require.Nil(t, remote)
	run(t, f.clone, "remote", "add", "macports", "git@github.com:macports/macports-ports.git")
	remote, err = e.UpstreamRemote(t.Context())
	require.NoError(t, err)
	require.Equal(t, "macports", remote.Name)
	for url, want := range map[string]bool{
		"https://github.com/macports/macports-ports.git": true,
		"https://github.com/MacPorts/macports-ports/":    true,
		"ssh://git@github.com/macports/macports-ports":   true,
		"https://github.com/ada/macports-ports.git":      false,
		"https://example.org/macports/macports-ports":    false,
	} {
		require.Equal(t, want, namesRepository(url, UpstreamRepository), url)
	}
}

func TestScopeFollowsCIsRule(t *testing.T) {
	scope := ScopeOf([]string{
		"textproc/jq/Portfile",
		"textproc/jq/files/patch-a.diff",
		"devel/libharbor/files/extra/x.patch",
		"devel/libharbor/README",
		"_resources/port1.0/group/github-1.0.tcl",
		".github/workflows/main.yml",
		"README.md",
	})
	require.Equal(t, []string{"devel/libharbor", "textproc/jq"}, scope.Ports)
	require.True(t, scope.Resources)
	require.Empty(t, ScopeOf([]string{"devel/libharbor/README"}).Ports, "only a Portfile or files/ marks a port")
}
