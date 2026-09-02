package git

import (
	"context"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/lockfile"
)

// The submit lock lives where the notes lock lives, for the same
// reason: linked worktrees share the notes ref, so a claim over a run
// recorded there must be one claim however many worktrees make it.
func TestSubmitLockIsSharedAcrossLinkedWorktrees(t *testing.T) {
	r := newRepo(t)
	ctx := context.Background()

	wt := filepath.Join(t.TempDir(), "linked")
	out, err := exec.Command("git", "-C", r.Root, "worktree", "add", "--quiet", wt).CombinedOutput()
	require.NoError(t, err, "%s", out)
	linked, err := Open(ctx, wt)
	require.NoError(t, err)

	p1, err := r.commonDirFile(ctx, "dockhand-submit.lock")
	require.NoError(t, err)
	p2, err := linked.commonDirFile(ctx, "dockhand-submit.lock")
	require.NoError(t, err)
	r1, _ := filepath.EvalSymlinks(p1)
	r2, _ := filepath.EvalSymlinks(p2)
	assert.Equal(t, r1, r2, "one repository, one submit lock, however many worktrees")

	// And held from one worktree, it refuses the other — the claim is
	// real, not two files that happen to share a name.
	unlock, err := r.LockSubmit(ctx, 0)
	require.NoError(t, err)
	t.Cleanup(unlock)
	_, err = linked.LockSubmit(ctx, 0)
	require.ErrorIs(t, err, lockfile.ErrHeld)
}

// The two locks are distinct: a claimant holding the submit lock still
// takes the notes lock inside its submit, and a note writer never
// waits on a claimant's guest boot.
func TestSubmitLockAndNotesLockAreIndependent(t *testing.T) {
	r := newRepo(t)
	ctx := context.Background()

	unlockSubmit, err := r.LockSubmit(ctx, 0)
	require.NoError(t, err)
	t.Cleanup(unlockSubmit)

	unlockNotes, err := r.LockNotes(ctx)
	require.NoError(t, err)
	unlockNotes()
}
