package ghactions

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/buildenv"
	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/testsupport"
)

// said is a Build that keeps what's said of it.
type said struct {
	buildenv.Build
	lines []string
}

func (s *said) Progress(message string) { s.lines = append(s.lines, message) }

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
	repo, err := git.Open(t.Context(), clone, "")
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
