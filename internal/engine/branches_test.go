package engine

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/testsupport"
)

// Your checkout's master is never one of dockhand's branches: a command
// run there names how to pick one, not adopt (the rc5 full stage, A4).
func TestMasterIsYourCheckoutsNotABranch(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e := f.open(t)
	_, err := e.Current(t.Context())
	require.ErrorIs(t, err, ErrNoBranch)
	require.ErrorIs(t, err, ErrYourCheckout)
	require.ErrorContains(t, err, "this is your checkout's master, not one of dockhand's branches; name one with -b <branch>")
	require.NotContains(t, err.Error(), "adopt")
}

// A branch whose worktree and Git branch are both gone has nothing left to
// check out, and the refusal says how to set its record aside (the rc6
// full stage, A10).
func TestAGoneBranchSaysHowToSetItAside(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e := f.open(t)
	branch, err := e.Start(t.Context(), StartRequest{Name: "poppler-25.09"})
	require.NoError(t, err)
	require.NoError(t, os.RemoveAll(branch.Worktree))
	testsupport.Git(t, f.clone, "worktree", "prune")
	testsupport.Git(t, f.clone, "branch", "-D", branch.Name)
	_, err = e.Path(t.Context(), "poppler-25.09")
	require.ErrorContains(t, err, "is gone, and so is its Git branch, so nothing of it is left: dockhand archive poppler-25.09 sets its record aside, and the name stays taken; another name starts afresh")
	_, err = e.ArchiveBranch(t.Context(), ArchiveRequest{Branch: branch})
	require.NoError(t, err, "as the refusal says")

	// Archived, it says the name is still held, where archive's advice read
	// as freeing it (the rc7 full stage).
	_, err = e.Path(t.Context(), "poppler-25.09")
	require.ErrorContains(t, err, "its record is archived, and keeps the name: start the work under another name (reusing an archived branch's name comes after the release)")
	require.NotContains(t, err.Error(), "dockhand archive")
}

// A record set aside takes one name, however it's set aside: <name>-archived,
// or -2 where a Git branch has that, which archive's rename and a Git
// rename's each chose their own way (the architecture review of 5897a82,
// R3-13).
func TestARecordSetAsideTakesTheFirstFreeName(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e := f.open(t)
	branch, err := e.Start(t.Context(), StartRequest{Name: "notes"})
	require.NoError(t, err)
	archived, err := e.Archive(t.Context(), branch, false)
	require.NoError(t, err)
	testsupport.Git(t, f.clone, "branch", "dockhand/notes-archived", "master")
	renamed, err := e.RenameArchived(t.Context(), archived)
	require.NoError(t, err)
	require.Equal(t, "notes-archived-2", renamed.ShortName())
	require.Contains(t, testsupport.Git(t, f.clone, "branch", "--list", "dockhand/notes-archived-2"), "dockhand/notes-archived-2")
	found, err := e.Resolve(t.Context(), "notes-archived-2")
	require.NoError(t, err)
	require.Equal(t, archived.ID, found.ID)
}
