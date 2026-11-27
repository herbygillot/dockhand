package engine

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/forge/forgetest"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/store"
	"github.com/herbygillot/dockhand/internal/testsupport"
)

// An earlier submission whose fork branch can't be read says why, in
// Git's words: the push a failed submit owed was settled over an SSH
// remote whose key GitHub refused, and the refusal said only "the fork's
// branch couldn't be read; … dockhand submit tries again", which no try
// cures (the v0.9.0-rc2 full stage's D-C6, #365). A cause the person has
// to put right names no retry; one another try may cure still does.
func TestAnUnreadForkBranchSaysWhy(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name, stderr, says string
		retries            bool
	}{
		{"a refused key", "git@github.com: Permission denied (publickey).", "is your key loaded?", false},
		{"a host key ssh doesn't know", "Host key verification failed.", "known_hosts", false},
		{"a connection that timed out", "ssh: connect to host github.com port 22: Operation timed out", "Operation timed out", true},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			f := setup(t)
			e, _ := f.withPreparer(t)
			f.withFork(t, e)
			branch := committedUpdate(t, e)
			commit := testsupport.Git(t, f.clone, "rev-parse", branch.Name)
			// The fork's remote over SSH, through an ssh that answers as
			// GitHub's server did, and exits as ssh does.
			ssh := filepath.Join(t.TempDir(), "ssh")
			require.NoError(t, os.WriteFile(ssh, []byte("#!/bin/sh\necho '"+c.stderr+"' >&2\nexit 255\n"), 0o755))
			testsupport.Git(t, f.clone, "remote", "set-url", "fork", "git@github.com:"+forgetest.ForkRepository+".git")
			testsupport.Git(t, f.clone, "config", "core.sshCommand", ssh)

			recorded := &model.Publication{ID: "pub_owed", Commit: model.ObjectID(commit), At: time.Now().UTC(), Head: forgetest.ForkRepository + ":" + branch.Name,
				Base: e.PullRequestRepository() + ":" + UpstreamBranch, Owes: []model.PublicationStep{model.StepPush, model.StepOpen}}
			require.NoError(t, e.Store.Update(t.Context(), e.Repository, func(tx store.Tx) error { return tx.SetPublication(branch.ID, "", recorded) }))
			branch, err := e.Branch(t.Context(), branch.ID)
			require.NoError(t, err)
			worktree, err := e.worktree(t.Context(), branch)
			require.NoError(t, err)
			_, _, _, err = e.settlePublication(t.Context(), worktree, branch, false)
			require.ErrorContains(t, err, "the fork's branch couldn't be read (")
			require.ErrorContains(t, err, c.says, "Git's reason is said")
			if c.retries {
				require.ErrorContains(t, err, "nothing was done; dockhand submit reads it again")
			} else {
				require.NotContains(t, err.Error(), "reads it again", "no retry is named for what a retry won't cure")
				require.ErrorContains(t, err, "put that right")
			}
			kept, err := e.Branch(t.Context(), branch.ID)
			require.NoError(t, err)
			require.True(t, kept.Publication.Same(*recorded), "nothing was settled")
		})
	}
}
