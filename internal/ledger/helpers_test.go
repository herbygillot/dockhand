package ledger_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/herbygillot/dockhand/v2/internal/git"
	"github.com/herbygillot/dockhand/v2/internal/ledger"
	"github.com/herbygillot/dockhand/v2/internal/record"
	"github.com/stretchr/testify/require"
)

type fixture struct {
	repo    *git.Repository
	store   *ledger.Store
	options ledger.Options
}

func newFixture(t *testing.T, format string) *fixture {
	t.Helper()
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	root := t.TempDir()
	runGit(t, root, "init", "--quiet", "--object-format="+format)
	repo, err := git.Open(t.Context(), root, "git")
	require.NoError(t, err)
	options := ledger.Options{Lockfile: filepath.Join(root, "locks", "ledger.lock"), LockTimeout: 5 * time.Second}
	store, err := ledger.New(repo, options)
	require.NoError(t, err)
	return &fixture{repo, store, options}
}

func runGit(t *testing.T, root string, args ...string) []byte {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "git", args...)
	cmd.Dir = root
	cmd.Env = isolatedGitEnv()
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "git %v: %v\n%s", args, err, out)

	return out
}

func (f *fixture) snapshot(t *testing.T) ledger.Snapshot {
	t.Helper()
	snapshot, err := f.store.Read(t.Context())
	require.NoError(t, err)
	return snapshot
}

func addChange(id record.ChangeID) func(context.Context, *ledger.Transaction) error {
	return func(_ context.Context, tx *ledger.Transaction) error {
		tx.State.Changes[id] = record.Change{ID: id, Disposition: record.ChangeOpen}
		return nil
	}
}

func (f *fixture) source(t *testing.T, content string) record.Source {
	t.Helper()
	blob, err := f.repo.WriteBlob(t.Context(), []byte(content))
	require.NoError(t, err)
	tree, err := f.repo.WriteTree(t.Context(), []git.TreeEntry{{Name: "Portfile", Mode: 0o100644, Type: "blob", Object: blob}})
	require.NoError(t, err)
	commit := f.commit(t, tree)
	return record.Source{Tree: record.ObjectID(tree), Commit: record.ObjectID(commit), Base: record.ObjectID(commit)}
}

func (f *fixture) commit(t *testing.T, tree string) string {
	t.Helper()
	identity := git.Signature{Name: "Ledger test", Email: "ledger@example.invalid", When: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	commit, err := f.repo.WriteCommit(t.Context(), git.Commit{Tree: tree, Message: "fixture\n", Author: identity, Committer: identity})
	require.NoError(t, err)
	return commit
}

func (f *fixture) ref(t *testing.T, name string) git.RefValue {
	t.Helper()
	ref, err := f.repo.ReadRef(t.Context(), name)
	require.NoError(t, err)
	return ref
}

func receive[T any](t *testing.T, channel <-chan T) T {
	t.Helper()
	select {
	case value := <-channel:
		return value
	case <-time.After(10 * time.Second):
		require.FailNow(t, "timed out waiting for coordinated operation")
	}
	var zero T
	return zero
}

func isolatedGitEnv() []string {
	var env []string
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(entry, "GIT_") {
			env = append(env, entry)
		}
	}
	return append(env, "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_NOSYSTEM=1")
}
