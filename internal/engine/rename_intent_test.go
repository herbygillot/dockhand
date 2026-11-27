package engine

import (
	"context"
	"testing"
	"time"

	"github.com/herbygillot/dockhand/internal/editprep"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/refusal"
	"github.com/herbygillot/dockhand/internal/store"
	"github.com/herbygillot/dockhand/internal/testsupport"
	"github.com/stretchr/testify/require"
)

// intentTo records a rename's intent on a branch, as a process cut short
// after saving it leaves it.
func intentTo(t *testing.T, e *Engine, branch model.Branch, to string) model.Branch {
	t.Helper()
	branch.RenameTo = to
	require.NoError(t, e.Store.Update(t.Context(), e.Repository, func(tx store.Tx) error { return tx.SetRenameTo(branch.ID, to) }))
	return branch
}

func reread(t *testing.T, e *Engine, id model.BranchID) model.Branch {
	t.Helper()
	var branch model.Branch
	require.NoError(t, e.Store.View(t.Context(), e.Repository, func(r store.Reader) error {
		var err error
		branch, err = r.Branch(id)
		return err
	}))
	return branch
}

// A version rename records its intent, renames Git's branch, then saves
// the name with the intent emptied (#194, L1).
func TestARenameRecordsItsIntentFirst(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e, _ := f.withPreparer(t)
	branch, err := e.Start(t.Context(), StartRequest{Name: "jq-1.7.1"})
	require.NoError(t, err)
	update, err := e.Update(t.Context(), UpdateRequest{Branch: branch, Action: model.EditUpdate, Port: "jq"})
	require.NoError(t, err)
	require.Equal(t, "dockhand/jq-1.8.1", update.Branch.Name)
	saved := reread(t, e, branch.ID)
	require.Equal(t, "dockhand/jq-1.8.1", saved.Name)
	require.Empty(t, saved.RenameTo, "the intent is emptied with the save")
}

// A name a rename is moving a branch to is taken: refused to another
// branch's rename (#194, L2).
func TestANameBeingMovedToIsTaken(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e := f.open(t)
	moving, err := e.Start(t.Context(), StartRequest{Name: "jq-1.7.1"})
	require.NoError(t, err)
	intentTo(t, e, moving, "dockhand/jq-1.8.1")
	other, err := e.Start(t.Context(), StartRequest{Name: "jq-other"})
	require.NoError(t, err)
	err = e.lockedBranch(t.Context(), other.ID, other.Name, func(ctx context.Context, current model.Branch, held *heldLock) error {
		return e.renameWithIntent(ctx, held, &current, "dockhand/jq-1.8.1", "renamed")
	})
	require.ErrorContains(t, err, "jq-1.8.1 is jq-1.7.1's, or the name its rename is moving it to")
	require.Equal(t, "dockhand/jq-other", reread(t, e, other.ID).Name)
}

// Either name finds a branch whose rename is under way, and status says
// the rename and finishes none (#194, L3, L6).
func TestEitherNameFindsABranchBeingRenamed(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e := f.open(t)
	branch, err := e.Start(t.Context(), StartRequest{Name: "jq-1.7.1"})
	require.NoError(t, err)
	intentTo(t, e, branch, "dockhand/jq-1.8.1")
	for _, name := range []string{"jq-1.7.1", "jq-1.8.1", "dockhand/jq-1.8.1"} {
		found, err := e.Resolve(t.Context(), name)
		require.NoError(t, err, name)
		require.Equal(t, branch.ID, found.ID, name)
	}
}

// A rename cut short is finished by the next command that takes the
// branch's lock: Git renamed and the name not saved, saved; Git not yet
// renamed, renamed and saved; Git with both names, stopped and said, with
// nothing guessed (#194, L4).
func TestARenameCutShortIsFinishedAtTheLock(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name    string
		arrange func(t *testing.T, e *Engine, branch model.Branch)
		want    string
	}{
		{"Git renamed, the name not saved", func(t *testing.T, e *Engine, branch model.Branch) {
			require.NoError(t, e.Repo.RenameBranch(t.Context(), branch.Name, "dockhand/jq-1.8.1"))
		}, "dockhand/jq-1.8.1"},
		{"Git not yet renamed", func(*testing.T, *Engine, model.Branch) {}, "dockhand/jq-1.8.1"},
		{"Git with both names", func(t *testing.T, e *Engine, branch model.Branch) {
			testsupport.Git(t, e.Repo.Root, "branch", "dockhand/jq-1.8.1", branch.Name)
		}, ""},
	} {
		f := setup(t)
		e := f.open(t)
		branch, err := e.Start(t.Context(), StartRequest{Name: "jq-1.7.1"})
		require.NoError(t, err)
		branch = intentTo(t, e, branch, "dockhand/jq-1.8.1")
		test.arrange(t, e, branch)
		err = gateErr(e.UnfinishedGate(t.Context(), branch, true))
		saved := reread(t, e, branch.ID)
		if test.want == "" {
			require.Error(t, err, test.name)
			require.Contains(t, refusal.WordsOf(err), "nothing was guessed", test.name)
			require.Equal(t, "dockhand/jq-1.8.1", saved.RenameTo, "the intent stays")
			continue
		}
		require.NoError(t, err, test.name)
		require.Equal(t, test.want, saved.Name, test.name)
		require.Empty(t, saved.RenameTo, test.name)
		_, _, err = e.Repo.Branch(t.Context(), test.want)
		require.NoError(t, err, test.name)
	}
}

// Where another process holds the new name's lock, the rename is left as
// it is and said under way (#194, L4).
func TestARenameUnderWayElsewhereIsLeft(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e := f.open(t)
	branch, err := e.Start(t.Context(), StartRequest{Name: "jq-1.7.1"})
	require.NoError(t, err)
	branch = intentTo(t, e, branch, "dockhand/jq-1.8.1")
	release, err := e.Repo.TryBranchLock("dockhand/jq-1.8.1")
	require.NoError(t, err)
	defer func() { _ = release() }()
	err = gateErr(e.UnfinishedGate(t.Context(), branch, true))
	require.Error(t, err)
	require.Contains(t, refusal.WordsOf(err), "is under way in another process")
	require.Equal(t, "dockhand/jq-1.8.1", reread(t, e, branch.ID).RenameTo)
}

// renamedElsewhere renames a branch as another command would, Git and the
// record, leaving the caller's copy stale.
func renamedElsewhere(t *testing.T, e *Engine, branch model.Branch, to string) {
	t.Helper()
	require.NoError(t, e.Repo.RenameBranch(t.Context(), branch.Name, to))
	branch.Name = to
	require.NoError(t, e.Store.Update(t.Context(), e.Repository, func(tx store.Tx) error { return tx.UpdateBranch(branch) }))
}

// blockedUntil reports whether run is still waiting after a moment, then
// lets go of the lock and waits for it.
func blockedUntil(t *testing.T, release func() error, run func() error) bool {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- run() }()
	blocked := true
	select {
	case <-done:
		blocked = false
	case <-time.After(300 * time.Millisecond):
	}
	require.NoError(t, release())
	if blocked {
		require.NoError(t, <-done)
	}
	return blocked
}

// Settling takes the lock of the name the branch's ID carries under it,
// not a name the caller read before: renamed meanwhile, it waits on the
// new name's lock another process holds (Codex's read of 2a, P2).
func TestSettlingTakesCurrentBranchNameLockAfterRename(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e := f.open(t)
	stale, err := e.Start(t.Context(), StartRequest{Name: "jq-1.7.1"})
	require.NoError(t, err)
	renamedElsewhere(t, e, stale, "dockhand/jq-1.8.1")
	release, err := e.Repo.TryBranchLock("dockhand/jq-1.8.1")
	require.NoError(t, err)
	require.True(t, blockedUntil(t, release, func() error {
		_, err := e.settled(t.Context(), &stale, SettlerFor(SettlerOther))
		return err
	}), "it waits for the lock of the name the branch carries now")
	require.Equal(t, "dockhand/jq-1.8.1", stale.Name, "the caller's copy is the branch as read under the lock")
}

// renamingPreparer renames the branch, as another command would, while
// the update prepares, before its step 3 takes the lock, and holds the
// new name's lock until the test lets it go.
type renamingPreparer struct {
	*fakePreparer
	rename func()
}

func (p renamingPreparer) Prepare(ctx context.Context, r editprep.Request) (editprep.Result, error) {
	result, err := p.fakePreparer.Prepare(ctx, r)
	p.rename()
	return result, err
}

// An update's steps 3 to 6 take the lock of the name the branch carries
// once it's prepared (Codex's read of 2a, P2).
func TestAnUpdateTakesCurrentBranchNameLockAfterRename(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e, p := f.withPreparer(t)
	branch, err := e.Start(t.Context(), StartRequest{Name: "jq-work"})
	require.NoError(t, err)
	locked := make(chan func() error, 1)
	e.Preparer = renamingPreparer{p, func() {
		renamedElsewhere(t, e, branch, "dockhand/jq-moved")
		release, err := e.Repo.TryBranchLock("dockhand/jq-moved")
		require.NoError(t, err)
		locked <- release
	}}
	done := make(chan error, 1)
	go func() {
		_, err := e.Update(t.Context(), UpdateRequest{Branch: branch, Action: model.EditUpdate, Port: "jq"})
		done <- err
	}()
	release := <-locked
	select {
	case err := <-done:
		t.Fatalf("the update went on under the old name's lock: %v", err)
	case <-time.After(300 * time.Millisecond):
	}
	require.NoError(t, release())
	require.NoError(t, <-done)
}

// A rename is made only under the old name's lock: renameWithIntent
// refuses without it, and the version rename a settled update finishes
// takes it (Codex's read of 2c, P2-3).
func TestSettledVersionRenameRequiresTheOldBranchLock(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e, _ := f.withPreparer(t)
	branch, err := e.Start(t.Context(), StartRequest{Name: "jq-1.7.1"})
	require.NoError(t, err)
	err = e.renameWithIntent(t.Context(), nil, &branch, "dockhand/jq-elsewhere", "renamed")
	require.ErrorContains(t, err, "without its lock")
	require.Equal(t, "dockhand/jq-1.7.1", reread(t, e, branch.ID).Name)

	kept := e.Store
	e.Store = &uncertainStore{Store: kept, when: func(tx *watchedTx) bool { return tx.written }, landed: false}
	_, err = e.Update(t.Context(), UpdateRequest{Branch: branch, Action: model.EditUpdate, Port: "jq"})
	require.Error(t, err, "the record isn't complete; step 6 didn't run")
	e.Store = kept
	_, err = e.Update(t.Context(), UpdateRequest{Branch: reread(t, e, branch.ID), Action: model.EditUpdate, Port: "jq"})
	require.NoError(t, err)
	require.Equal(t, "dockhand/jq-1.8.1", reread(t, e, branch.ID).Name, "settled, the update finishes its rename under the lock")
}

// A rename is refused where only another process holds the old name's
// lock: holding isn't shown by the lock being busy, but by the value
// lockedBranch hands its caller, which nothing else makes (Codex's
// confirmation of #194, P3).
func TestRenameWithIntentRefusesWhenOnlyAnotherProcessHoldsOldLock(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e := f.open(t)
	branch, err := e.Start(t.Context(), StartRequest{Name: "jq-1.7.1"})
	require.NoError(t, err)
	release, err := e.Repo.TryBranchLock(branch.Name)
	require.NoError(t, err, "another process holds the old name's lock")
	defer func() { _ = release() }()
	err = e.renameWithIntent(t.Context(), nil, &branch, "dockhand/jq-1.8.1", "renamed")
	require.ErrorContains(t, err, "without its lock")
	err = e.renameWithIntent(t.Context(), &heldLock{name: branch.Name, released: true}, &branch, "dockhand/jq-1.8.1", "renamed")
	require.ErrorContains(t, err, "without its lock", "a lock spent once its holder returned")
	require.Equal(t, "dockhand/jq-1.7.1", reread(t, e, branch.ID).Name)
	require.Empty(t, reread(t, e, branch.ID).RenameTo, "no intent was recorded")
}
