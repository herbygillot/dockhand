package ghactions

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/buildenv"
	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/testsupport"
)

// said is a Build that keeps what's said of it.
type said struct {
	buildenv.Build
	lines []string
}

func (s *said) Progress(message string) { s.lines = append(s.lines, message) }
func (s *said) Waiting(message string)  { s.Progress(message) }
func (s *said) Holding(string) string   { return "" }

// The check's branch goes from your fork once the check is done with it,
// only while it holds the commit checked: one that moved since is left,
// and said, and neither fails the check
// (the test plan's step 2, item 20).
func TestTheChecksBranchGoesOnlyWhileItHoldsTheCommit(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	clone := filepath.Join(root, "clone")
	testsupport.Git(t, root, "init", "-q", clone)
	testsupport.Git(t, clone, "commit", "-q", "--allow-empty", "-m", "checked")
	checked := testsupport.Git(t, clone, "rev-parse", "HEAD")
	testsupport.Git(t, clone, "commit", "-q", "--allow-empty", "-m", "later")
	later := testsupport.Git(t, clone, "rev-parse", "HEAD")
	fork := filepath.Join(root, "fork.git")
	testsupport.Git(t, root, "init", "-q", "--bare", fork)
	repo, err := git.Open(t.Context(), clone, git.Program{Env: os.Environ()})
	require.NoError(t, err)
	p := &Provider{Repo: repo}
	target := buildenv.Fork{Repository: "ada/macports-ports", PushURL: fork}
	branch := BranchPrefix + checked[:12]

	testsupport.Git(t, clone, "push", "-q", fork, later+":refs/heads/"+branch)
	build := &said{}
	p.removeBranch(t.Context(), build, target, branch, checked)
	require.Equal(t, []string{"left " + branch + " on ada/macports-ports, since it has moved since the check"}, build.lines)
	require.Equal(t, later, testsupport.Git(t, fork, "rev-parse", branch))

	testsupport.Git(t, clone, "push", "-q", "-f", fork, checked+":refs/heads/"+branch)
	build = &said{}
	p.removeBranch(t.Context(), build, target, branch, checked)
	require.Equal(t, []string{"removed " + branch + " from ada/macports-ports"}, build.lines)
	require.Empty(t, testsupport.Git(t, fork, "branch", "--list", branch))

	build = &said{}
	p.removeBranch(t.Context(), build, target, branch, checked)
	require.Equal(t, []string{"removed " + branch + " from ada/macports-ports"}, build.lines, "one already gone is gone either way, and the check goes on")
}

// pushingAPI is a fork whose workflow runs and that lists no runs.
type pushingAPI struct{ API }

func (pushingAPI) WorkflowState(context.Context, string) (string, error) { return "active", nil }
func (pushingAPI) Runs(context.Context, string, string, string) ([]Run, error) {
	return nil, nil
}

// noting is a Build that keeps the notes its launches saved, the first and
// the one the start handed back.
type noting struct {
	said
	notes []string
}

func (n *noting) Launch(note string, start func(context.Context) (string, error)) error {
	n.notes = append(n.notes, note)
	after, err := start(context.Background())
	n.notes = append(n.notes, after)
	return err
}

// The check's branch is its own, the commit's and the check's ID's: one
// already at the commit that no attempt of the check noted pushing is
// refused by name, and a push the remote rejects is written down as
// having started nothing, which a stop then needs no run for (the
// lifecycle design's "GitHub").
func TestTheChecksOwnBranchAndAPushThatStartedNothing(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	clone := filepath.Join(root, "clone")
	testsupport.Git(t, root, "init", "-q", clone)
	testsupport.Git(t, clone, "commit", "-q", "--allow-empty", "-m", "checked")
	commit := testsupport.Git(t, clone, "rev-parse", "HEAD")
	repo, err := git.Open(t.Context(), clone, git.Program{Env: os.Environ()})
	require.NoError(t, err)
	run := model.Run{ID: "run_abcdefghijklmnop", Number: 4}
	branch := buildenv.CheckBranch(commit, run.ID)
	require.Equal(t, "dockhand-check/"+commit[:12]+"-abcdefghijklmnop", branch)
	job := buildenv.Job{Run: run, Commit: commit, Directory: t.TempDir(), Execution: model.GuestExecution{Attempt: 1}}

	fork := filepath.Join(root, "fork.git")
	testsupport.Git(t, root, "init", "-q", "--bare", fork)
	testsupport.Git(t, clone, "push", "-q", fork, commit+":refs/heads/"+branch)
	p := &Provider{Repo: repo, API: pushingAPI{}, Fork: func(context.Context) (buildenv.Fork, error) {
		return buildenv.Fork{Repository: "ada/macports-ports", PushURL: fork}, nil
	}}
	err = p.Execute(t.Context(), job, &noting{})
	require.ErrorContains(t, err, branch+" on ada/macports-ports is already at commit "+commit[:12]+", and no attempt of check-4 noted pushing it")

	// A remote that refuses the push: the note it saved first says only
	// that something may have started, and the one it hands back that
	// nothing did.
	refusing := filepath.Join(root, "refusing")
	testsupport.Git(t, root, "init", "-q", "-b", branch, refusing)
	p.Fork = func(context.Context) (buildenv.Fork, error) {
		return buildenv.Fork{Repository: "ada/macports-ports", PushURL: refusing}, nil
	}
	build := &noting{}
	require.Error(t, p.Execute(t.Context(), job, build))
	require.Len(t, build.notes, 2)
	require.NotContains(t, build.notes[0], "nothing")
	require.Contains(t, build.notes[0], `"action":"push"`)
	require.Contains(t, build.notes[1], `"nothing":"git push reported the branch rejected: `)
	require.Contains(t, build.notes[1], "[remote rejected]")
}
