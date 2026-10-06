package engine

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/store"
	"github.com/herbygillot/dockhand/internal/testsupport"
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
	testsupport.Git(t, f.upstream, "init", "-q")
	write(t, f.upstream, map[string]string{
		"_resources/port1.0/group/github-1.0.tcl": "# group\n",
		"textproc/jq/Portfile":                    "name jq\nversion 1.7.1\n",
		"devel/libharbor/Portfile":                "name libharbor\n",
	})
	testsupport.Git(t, f.upstream, "add", "-A")
	testsupport.Git(t, f.upstream, "commit", "-q", "-m", "init")
	testsupport.Git(t, root, "clone", "-q", f.upstream, f.clone)
	write(t, f.upstream, map[string]string{"devel/libharbor/Portfile": "name libharbor\nversion 2\n"})
	testsupport.Git(t, f.upstream, "commit", "-q", "-am", "libharbor: update to 2")
	// The worktrees are the fixture's own, never the person's
	// ~/Source/macports-branches, which the default is.
	f.options = Options{Tree: f.clone, Database: filepath.Join(root, "home", ".dockhand", "dockhand.db"), Upstream: f.upstream, Now: func() time.Time { return at },
		Worktrees: filepath.Join(root, "src", "macports-branches")}
	return f
}

func (f fixture) open(t *testing.T) *Engine {
	t.Helper()
	e, err := Open(t.Context(), f.options)
	require.NoError(t, err)
	t.Cleanup(func() { e.Close() })
	e.HTTPS = testsupport.HTTPSAnswers{} // no test asks the network
	return e
}

func (f fixture) upstreamMaster(t *testing.T) model.ObjectID {
	return model.ObjectID(testsupport.Git(t, f.upstream, "rev-parse", "master"))
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
	t.Parallel()
	root := t.TempDir()
	testsupport.Git(t, root, "init", "-q")
	write(t, root, map[string]string{"README.md": "dockhand\n", "internal/cli/main.go": "package cli\n"})
	testsupport.Git(t, root, "add", "-A")
	testsupport.Git(t, root, "commit", "-q", "-m", "init")
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
	t.Parallel()
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
	require.Equal(t, string(branch.Base), testsupport.Git(t, branch.Worktree, "rev-parse", "HEAD"))
	require.Len(t, files(t, f.clone), 3, "the person's checkout is untouched")
	require.Equal(t, "master", testsupport.Git(t, f.clone, "branch", "--show-current"))

	_, err = e.Start(t.Context(), StartRequest{Name: "dockhand/jq-update"})
	require.ErrorContains(t, err, "already a tracked branch")
	_, err = e.Start(t.Context(), StartRequest{Name: "bad name"})
	require.ErrorContains(t, err, "not a usable branch name")

	testsupport.Git(t, f.clone, "branch", "dockhand/mine")
	_, err = e.Start(t.Context(), StartRequest{Name: "mine"})
	require.ErrorIs(t, err, git.ErrBranchExists)
	require.ErrorContains(t, err, "dockhand adopt dockhand/mine")

	// A branch named for a port has its directory checked out, so its
	// files are there to edit (the Vx port's field testing, 2026-10-04).
	named, err := e.Start(t.Context(), StartRequest{Name: "jq"})
	require.NoError(t, err)
	require.Contains(t, files(t, named.Worktree), "textproc/jq/Portfile")

	require.NoError(t, os.MkdirAll(filepath.Join(e.Worktrees(), "occupied"), 0o755))
	_, err = e.Start(t.Context(), StartRequest{Name: "occupied"})
	require.ErrorContains(t, err, "already exists")
	require.Empty(t, testsupport.Git(t, f.clone, "branch", "--list", "dockhand/occupied"), "a refusal creates no branch")

	require.NoError(t, e.Store.View(t.Context(), e.Repository, func(r store.Reader) error {
		events, err := r.Events(0, 0)
		require.NoError(t, err)
		require.Equal(t, "branch.start", events[len(events)-1].Kind)
		return nil
	}))
}

func TestAFailedStartLeavesNothingBehind(t *testing.T) {
	t.Parallel()
	f := setup(t)
	blocker := filepath.Join(t.TempDir(), "not-a-directory")
	require.NoError(t, os.WriteFile(blocker, []byte("x"), 0o644))
	f.options.Worktrees = filepath.Join(blocker, "branches")
	e := f.open(t)
	_, err := e.Start(t.Context(), StartRequest{Name: "jq-update"})
	require.Error(t, err)
	require.Empty(t, testsupport.Git(t, f.clone, "branch", "--list", "dockhand/jq-update"), "the branch it created is deleted again")

	f.options.Worktrees, f.options.Upstream = "", filepath.Join(t.TempDir(), "no-such-upstream")
	e = f.open(t)
	_, err = e.Start(t.Context(), StartRequest{Name: "jq-update"})
	require.ErrorContains(t, err, "fetching master")
	require.Empty(t, testsupport.Git(t, f.clone, "branch", "--list", "dockhand/jq-update"))
}

func TestStartHereUsesThePersonsCheckoutOnlyWhenNothingWouldBeDisplaced(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e := f.open(t)
	write(t, f.clone, map[string]string{"textproc/jq/Portfile": "name jq\nversion 1.8.1\n"})
	_, err := e.Start(t.Context(), StartRequest{Name: "jq-here", Here: true})
	require.ErrorContains(t, err, "uncommitted changes to textproc/jq/Portfile")
	require.Empty(t, testsupport.Git(t, f.clone, "branch", "--list", "dockhand/jq-here"))

	testsupport.Git(t, f.clone, "checkout", "-q", "--", ".")
	write(t, f.clone, map[string]string{"notes.txt": "untracked work is not displaced\n"})
	branch, err := e.Start(t.Context(), StartRequest{Name: "jq-here", Here: true})
	require.NoError(t, err)
	require.False(t, branch.Managed)
	require.Equal(t, f.clone, branch.Worktree)
	require.Equal(t, "dockhand/jq-here", testsupport.Git(t, f.clone, "branch", "--show-current"))
	require.Equal(t, f.upstreamMaster(t), branch.Base)
}

func TestAdoptTracksABranchAsItStands(t *testing.T) {
	t.Parallel()
	f := setup(t)
	testsupport.Git(t, f.clone, "switch", "-q", "-c", "update-jq")
	write(t, f.clone, map[string]string{"textproc/jq/Portfile": "name jq\nversion 1.8.1\n"})
	testsupport.Git(t, f.clone, "commit", "-q", "-am", "Update jq")
	write(t, f.clone, map[string]string{"textproc/jq/files/patch-fix.diff": "--- a\n", "_resources/port1.0/group/github-1.0.tcl": "# group, fixed\n"})
	testsupport.Git(t, f.clone, "add", "-A")
	testsupport.Git(t, f.clone, "commit", "-q", "-m", "oops")
	head := testsupport.Git(t, f.clone, "rev-parse", "HEAD")

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
	require.Equal(t, testsupport.Git(t, f.clone, "merge-base", "HEAD", string(f.upstreamMaster(t))), string(adoption.Branch.Base))
	require.Equal(t, head, testsupport.Git(t, f.clone, "rev-parse", "HEAD"), "adopting moves nothing")

	again, err := e.Adopt(t.Context(), AdoptRequest{Branch: "update-jq"})
	require.NoError(t, err)
	require.True(t, again.Already)
	require.Equal(t, adoption.Branch.ID, again.Branch.ID)
	require.Equal(t, 2, again.Commits, "a tracked branch is counted as it stands")
	require.Equal(t, []string{"jq"}, again.Scope.PortNames())

	// A branch not checked out anywhere is checked out as start checks one
	// out, not left for git switch to switch the person's own checkout
	// (the flatbuffers, nuspell, zola, and alertmanager run's finding 3).
	testsupport.Git(t, f.clone, "branch", "elsewhere", "master")
	elsewhere, err := e.Adopt(t.Context(), AdoptRequest{Branch: "elsewhere"})
	require.NoError(t, err)
	require.Equal(t, e.worktreeDirectory("elsewhere"), elsewhere.Branch.Worktree)
	require.Equal(t, elsewhere.Branch.Worktree, elsewhere.Placed)
	require.True(t, elsewhere.Branch.Managed, "dockhand's, as start's are")
	require.DirExists(t, filepath.Join(elsewhere.Branch.Worktree, "_resources"))
	require.Zero(t, elsewhere.Commits)

	_, err = e.Adopt(t.Context(), AdoptRequest{Branch: "master"})
	require.ErrorContains(t, err, "what branches start from")
	_, err = e.Adopt(t.Context(), AdoptRequest{Branch: "absent"})
	require.ErrorContains(t, err, "no local branch absent")
	testsupport.Git(t, f.clone, "switch", "-q", "--detach")
	_, err = e.Adopt(t.Context(), AdoptRequest{})
	require.ErrorContains(t, err, "not on a branch")
}

func TestPathAndResolve(t *testing.T) {
	t.Parallel()
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
	require.ErrorContains(t, err, "this is your checkout's master, not one of dockhand's branches")

	// Where dockhand can't check an adopted branch out, a worktree the
	// person adds for it later is found, and recorded as theirs.
	testsupport.Git(t, f.clone, "branch", "parked", "master")
	require.NoError(t, os.MkdirAll(e.worktreeDirectory("parked"), 0o755))
	parked, err := e.Adopt(t.Context(), AdoptRequest{Branch: "parked"})
	require.NoError(t, err)
	require.Contains(t, parked.Unplaced, "already exists")
	require.Empty(t, parked.Branch.Worktree)
	// Git reports resolved paths; on macOS the temporary directory is
	// reached through /var, a link to /private/var.
	temporary, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	theirs := filepath.Join(temporary, "parked")
	testsupport.Git(t, f.clone, "worktree", "add", "-q", theirs, "parked")
	path, err = e.Path(t.Context(), "parked")
	require.NoError(t, err)
	require.Equal(t, theirs, path)
	recorded, err := e.Resolve(t.Context(), "parked")
	require.NoError(t, err)
	require.Equal(t, theirs, recorded.Worktree)
	require.False(t, recorded.Managed, "the person's")

	// A removed worktree is checked out again; with its Git branch gone
	// too, there is nothing to check out.
	require.NoError(t, e.Repo.RemoveWorktree(context.Background(), branch.Worktree))
	path, err = e.Path(t.Context(), "jq-update")
	require.NoError(t, err)
	require.DirExists(t, path)
	require.NoError(t, e.Repo.RemoveWorktree(context.Background(), branch.Worktree))
	testsupport.Git(t, f.clone, "branch", "-D", branch.Name)
	_, err = e.Path(t.Context(), "jq-update")
	require.ErrorContains(t, err, "is gone, and so is its Git branch")
}

func TestUpstreamRemoteIsFoundByURL(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e := f.open(t)
	remote, err := e.UpstreamRemote(t.Context())
	require.NoError(t, err)
	require.Nil(t, remote)
	testsupport.Git(t, f.clone, "remote", "add", "macports", "git@github.com:macports/macports-ports.git")
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
		testsupport.Git(t, f.clone, "remote", "set-url", "macports", url)
		remote, err = e.UpstreamRemote(t.Context())
		require.NoError(t, err)
		require.Equal(t, want, remote != nil, url)
	}
}

// An update's file is in its port's directory, or else in its own.
func TestAnUpdatedFileIsInItsPortsDirectory(t *testing.T) {
	t.Parallel()
	for file, directory := range map[string]string{
		"textproc/jq/Portfile":                    "textproc/jq",
		"textproc/jq/files/patch-a.diff":          "textproc/jq",
		"_resources/port1.0/group/golang-1.0.tcl": "_resources/port1.0/group",
		"README.md": ".",
	} {
		require.Equal(t, directory, portDirectory(file), file)
	}
}

// A command pointed at a checkout without naming it itself, as
// MACPORTS_TREE points one, works in the branch worktree it is run in,
// whose branch is the one checked out there; anywhere else, in the
// checkout named. The name may reach the checkout through a symlink, as
// ~/Source/ports reaches ~/Source/macports-ports.
func TestHereIsTheWorktreeACommandRunsIn(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e := f.open(t)
	branch, err := e.Start(t.Context(), StartRequest{Name: "jq-update"})
	require.NoError(t, err)
	worktree, err := filepath.EvalSymlinks(branch.Worktree)
	require.NoError(t, err)
	tree := e.Clone()
	link := filepath.Join(t.TempDir(), "ports")
	require.NoError(t, os.Symlink(tree, link))
	inside := filepath.Join(branch.Worktree, "inside")
	require.NoError(t, os.Mkdir(inside, 0o755))

	for _, named := range []string{tree, link} {
		require.Equal(t, worktree, Here(t.Context(), named, branch.Worktree, ""), "the worktree run in")
		require.Equal(t, worktree, Here(t.Context(), named, inside, ""), "a directory inside it")
		require.Equal(t, named, Here(t.Context(), named, t.TempDir(), ""), "not in a checkout")
		require.Equal(t, named, Here(t.Context(), named, f.upstream, ""), "in another repository")
	}
	require.Equal(t, "/nowhere", Here(t.Context(), "/nowhere", branch.Worktree, ""), "a tree that isn't a checkout is left to fail as it would")
}
