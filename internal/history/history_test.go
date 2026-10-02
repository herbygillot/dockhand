package history_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/history"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/store"
	"github.com/herbygillot/dockhand/internal/store/sqlite"
	"github.com/herbygillot/dockhand/internal/testsupport"
)

var at = time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)

// world is a repository with a branch two commits above master, a store
// that tracks it, and the transitions over both.
type world struct {
	root        string
	transitions *history.Transitions
	branch      model.Branch
	base, head  string
}

func newWorld(t *testing.T) world {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	root = filepath.Join(root, "ports")
	require.NoError(t, os.MkdirAll(filepath.Join(root, "textproc", "jq"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "textproc", "jq", "Portfile"), []byte("name jq\n"), 0o644))
	testsupport.Git(t, root, "init", "-q", "-b", "master")
	testsupport.Git(t, root, "add", "-A")
	testsupport.Git(t, root, "commit", "-q", "-m", "init")
	base := testsupport.Git(t, root, "rev-parse", "HEAD")
	testsupport.Git(t, root, "switch", "-q", "-c", "dockhand/jq")
	testsupport.Git(t, root, "commit", "-q", "--allow-empty", "-m", "jq: one")
	head := testsupport.Git(t, root, "rev-parse", "HEAD")
	repo, err := git.Open(t.Context(), root, "")
	require.NoError(t, err)
	s, err := sqlite.Open(t.Context(), filepath.Join(filepath.Dir(root), "dockhand.db"), sqlite.Options{})
	require.NoError(t, err)
	t.Cleanup(func() { s.Close() })
	repository, err := s.Register(t.Context(), repo.CommonDir)
	require.NoError(t, err)
	branch := model.Branch{ID: "br_1", Repository: repository, Name: "dockhand/jq", Base: model.ObjectID(base), Worktree: root, Managed: true, State: model.BranchOpen, CreatedAt: at, Origin: model.OriginPerson}
	require.NoError(t, s.Update(t.Context(), repository, func(tx store.Tx) error { return tx.AddBranch(branch) }))
	transitions := &history.Transitions{Repo: repo, Store: s, Repository: repository, Now: func() time.Time { return at },
		Worktree: func(context.Context, model.Branch) (*git.Repository, error) { return repo, nil }}
	return world{root: root, transitions: transitions, branch: branch, base: base, head: head}
}

// prepared records a tidy's checkpoint as prepared, from head to a new
// commit on base, and makes that commit, as a tidy that stopped before
// moving the branch would have.
func (w world) prepared(t *testing.T) model.Checkpoint {
	t.Helper()
	after := testsupport.Git(t, w.root, "commit-tree", w.base+"^{tree}", "-p", w.base, "-m", "jq: tidied")
	checkpoint := model.Checkpoint{Kind: model.CheckpointTidy, Branch: w.branch.ID, Before: model.ObjectID(w.head), After: model.ObjectID(after),
		BaseBefore: w.branch.Base, BaseAfter: w.branch.Base, At: at}
	require.NoError(t, w.transitions.Prepare(t.Context(), &checkpoint))
	return checkpoint
}

func (w world) settled(t *testing.T) {
	t.Helper()
	require.NoError(t, w.transitions.With(t.Context(), w.branch, func(context.Context) error { return nil }))
}

func (w world) state(t *testing.T, number int) model.CheckpointState {
	t.Helper()
	checkpoint, found := w.transitions.Read(t.Context(), number)
	require.True(t, found)
	return checkpoint.State
}

// A change counts as made when the branch holds what it wrote, even with
// a person's commits on top since: its new head is in the branch's
// history, which only the change could have put there.
func TestAChangeWithCommitsOnTopIsMade(t *testing.T) {
	w := newWorld(t)
	checkpoint := w.prepared(t)
	testsupport.Git(t, w.root, "update-ref", "refs/heads/dockhand/jq", string(checkpoint.After), w.head)
	testsupport.Git(t, w.root, "reset", "-q", "--hard")
	testsupport.Git(t, w.root, "commit", "-q", "--allow-empty", "-m", "jq: more, by hand")
	w.settled(t)
	require.Equal(t, model.CheckpointApplied, w.state(t, checkpoint.Number))
}

// A change whose branch is somewhere its new head never was wasn't made,
// whatever else happened to the branch, and is abandoned with the refs
// it made.
func TestAChangeTheBranchNeverHeldIsAbandoned(t *testing.T) {
	w := newWorld(t)
	checkpoint := w.prepared(t)
	testsupport.Git(t, w.root, "update-ref", checkpoint.Ref(), w.head)
	testsupport.Git(t, w.root, "reset", "-q", "--hard", w.base)
	w.settled(t)
	require.Equal(t, model.CheckpointAbandoned, w.state(t, checkpoint.Number))
	require.Empty(t, testsupport.Git(t, w.root, "for-each-ref", checkpoint.Ref()), "its ref is gone")
}
