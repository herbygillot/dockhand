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
	require.ErrorContains(t, err, "is gone, and so is its Git branch, so nothing of it is left: dockhand archive poppler-25.09 sets its record aside")
	_, err = e.ArchiveBranch(t.Context(), ArchiveRequest{Branch: branch})
	require.NoError(t, err, "as the refusal says")
}
