package git

import (
	"context"
	"path/filepath"
	"strings"
)

// CommonDirFile names a file in the COMMON git dir, not the worktree's
// own: a linked worktree has a private git dir while refs — the state
// ref, the branches and the notes among them — are shared. A lock placed
// per-worktree would let two worktrees hold different locks over the
// same ref, which is exactly the lost update the locks exist to prevent.
// Every lock over shared state resolves its path here, so there is one
// rule for where "here" is.
//
// It is exported because the ledger flock is no longer this package's to
// take. R23 puts every durable write behind one lock, statestore.Lock,
// held by the store around the state ref AND the note export together;
// the shipped LockNotes and LockSubmit were two locks over a note that
// is no longer the authority, and they retired with it. What the store
// needs from git is the one thing only git can answer — where
// $GIT_COMMON_DIR is for this checkout — and that is this verb. It
// returns a path and takes nothing: acquiring is lockfile's.
func (r *Repo) CommonDirFile(ctx context.Context, name string) (string, error) {
	dir, err := r.git(ctx, "rev-parse", "--git-common-dir")
	if err != nil {
		return "", err
	}
	if !filepath.IsAbs(dir) {
		dir = filepath.Join(r.Root, dir)
	}
	return filepath.Join(strings.TrimSpace(dir), name), nil
}
