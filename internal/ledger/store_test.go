package ledger_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/herbygillot/dockhand/v2/internal/git"
	"github.com/herbygillot/dockhand/v2/internal/ledger"
	"github.com/herbygillot/dockhand/v2/internal/record"
	"github.com/stretchr/testify/require"
)

func TestSnapshotsAndAtomicRefAdoption(t *testing.T) {
	for _, format := range []string{"sha1", "sha256"} {
		t.Run(format, func(t *testing.T) {
			f := newFixture(t, format)
			_, err := f.store.Read(t.Context())
			require.ErrorIs(t, err, ledger.ErrNoState, "initial read: %v", err)
			require.NoError(t, f.store.Update(t.Context(), addChange("before")))
			before := f.snapshot(t)
			source := f.source(t, "version 1.0\n")
			entered, resume := make(chan struct{}), make(chan struct{})
			var once sync.Once
			unblock := func() { once.Do(func() { close(resume) }) }
			defer unblock()
			done := make(chan error, 1)
			go func() {
				done <- f.store.Update(t.Context(), func(ctx context.Context, tx *ledger.Transaction) error {
					if tx.Version != before.Version {
						return fmt.Errorf("wrong starting version: %s", tx.Version)
					}
					delete(tx.State.Changes, "before")
					tx.State.Changes["after"] = record.Change{ID: "after"}
					tx.State.Revisions["revision"] = record.Revision{ID: "revision", Source: source}
					tx.Refs = []git.RefChange{{Name: "refs/heads/prepared", Desired: git.RefValue{Exists: true, Object: string(source.Commit)}}}
					close(entered)
					select {
					case <-resume:
						return nil
					case <-ctx.Done():
						return ctx.Err()
					}
				})
			}()
			receive(t, entered)
			readCtx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
			during, err := f.store.Read(readCtx)
			cancel()
			require.NoError(t, err)
			require.Equal(t, before, during, "reader observed uncommitted state")
			require.False(t, f.ref(t, "refs/heads/prepared").Exists, "branch moved before commit")

			unblock()
			require.NoError(t, receive(t, done))
			after := f.snapshot(t)
			require.NotEqual(t, before.Version, after.Version, "bad committed snapshot: %+v", after)
			require.Len(t, after.State.Changes, 1, "bad committed snapshot: %+v", after)
			require.Equal(t, record.ChangeID("after"), after.State.Changes["after"].ID, "bad committed snapshot: %+v", after)

			parent, err := f.repo.Resolve(t.Context(), string(after.Version)+"^")
			require.NoError(t, err, "history parent: %s, %v", parent, err)
			require.Equal(t, string(before.Version), parent, "history parent: %s, %v", parent, err)

			for _, id := range []record.ObjectID{source.Commit, source.Tree} {
				got := f.ref(t, ledger.PinsPrefix+string(id))
				require.Equal(t, string(id), got.Object, "missing source pin: %+v", got)
			}
			require.Equal(t, string(source.Commit), f.ref(t, "refs/heads/prepared").Object, "branch not adopted with state")

			delete(after.State.Changes, "after")
			reopened, err := ledger.New(f.repo, f.options)
			require.NoError(t, err)
			persisted, err := reopened.Read(t.Context())
			require.NoError(t, err)
			require.Equal(t, record.ChangeID("after"), persisted.State.Changes["after"].ID, "mutating a read changed durable state")
			require.NoError(t, reopened.Update(t.Context(), func(context.Context, *ledger.Transaction) error { return nil }))
			require.Equal(t, after.Version, f.snapshot(t).Version, "no-op appended history")
		})
	}
}

func TestRejectedTransactionsLeaveStateAndRefsUnchanged(t *testing.T) {
	f := newFixture(t, "sha1")
	require.NoError(t, f.store.Update(t.Context(), addChange("original")))
	before := f.snapshot(t)
	source := f.source(t, "version 2.0\n")
	sentinel := errors.New("callback rejected")
	tests := []struct {
		name string
		edit func(context.Context, *ledger.Transaction) error
		want error
	}{
		{"callback error", func(context.Context, *ledger.Transaction) error { return sentinel }, sentinel},
		{"invalid state", func(_ context.Context, tx *ledger.Transaction) error {
			tx.State.Changes["wrong"] = record.Change{ID: "other"}
			return nil
		}, ledger.ErrInvalidState},
		{"nested transaction", func(ctx context.Context, _ *ledger.Transaction) error {
			return f.store.Update(ctx, addChange("nested"))
		}, ledger.ErrNestedTransaction},
		{"ref conflict", func(_ context.Context, tx *ledger.Transaction) error {
			tx.Refs = []git.RefChange{
				{Name: "refs/heads/new", Desired: git.RefValue{Exists: true, Object: string(source.Commit)}},
				{Name: "refs/heads/absent", Expected: git.RefValue{Exists: true, Object: string(source.Commit)}, Desired: git.RefValue{Exists: true, Object: string(source.Commit)}},
			}
			return nil
		}, ledger.ErrConflict},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			err := f.store.Update(t.Context(), func(ctx context.Context, tx *ledger.Transaction) error {
				calls++
				tx.State.Revisions["new"] = record.Revision{ID: "new", Source: source}
				return test.edit(ctx, tx)
			})
			require.ErrorIs(t, err, test.want, "Update: %v, callback calls: %d", err, calls)
			require.Equal(t, 1, calls, "Update: %v, callback calls: %d", err, calls)
			require.Equal(t, before, f.snapshot(t), "rejected transaction changed state")

			for _, name := range []string{"refs/heads/new", ledger.PinsPrefix + string(source.Commit), ledger.PinsPrefix + string(source.Tree)} {
				require.False(t, f.ref(t, name).Exists, "rejected transaction published %s", name)
			}
		})
	}
	for _, name := range []string{ledger.StateRef, ledger.NotesRef, ledger.PinsPrefix + string(source.Commit)} {
		err := f.store.Update(t.Context(), func(_ context.Context, tx *ledger.Transaction) error {
			tx.Refs = []git.RefChange{{Name: name, Desired: git.RefValue{Exists: true, Object: string(source.Commit)}}}
			return nil
		})
		require.Error(t, err, "allowed callback to override managed ref %s", name)
	}
	require.Equal(t, before, f.snapshot(t), "managed-ref rejection changed state")
}

func TestTransactionCancellationAndPanicReleaseWriter(t *testing.T) {
	f := newFixture(t, "sha1")
	require.NoError(t, f.store.Update(t.Context(), addChange("original")))
	before := f.snapshot(t)
	ctx, cancel := context.WithCancel(t.Context())
	err := f.store.Update(ctx, func(_ context.Context, tx *ledger.Transaction) error {
		tx.State.Changes["canceled"] = record.Change{ID: "canceled"}
		cancel()
		return nil
	})
	require.ErrorIs(t, err, context.Canceled, "canceled Update: %v", err)

	called := false
	err = f.store.Update(ctx, func(context.Context, *ledger.Transaction) error { called = true; return nil })
	require.ErrorIs(t, err, context.Canceled, "pre-canceled Update: %v, callback %v", err, called)
	require.False(t, called, "pre-canceled Update: %v, callback %v", err, called)
	require.Equal(t, before, f.snapshot(t), "cancellation committed state")

	require.PanicsWithValue(t, "test panic", func() {
		_ = f.store.Update(t.Context(), func(_ context.Context, tx *ledger.Transaction) error {
			tx.State.Changes["panicked"] = record.Change{ID: "panicked"}
			panic("test panic")
		})
	})
	require.Equal(t, before, f.snapshot(t), "panic committed state")
	require.NoError(t, f.store.Update(t.Context(), addChange("recovered")))

	options := f.options
	options.OperationTimeout = time.Second
	bounded, err := ledger.New(f.repo, options)
	require.NoError(t, err)
	before = f.snapshot(t)
	called = false
	err = bounded.Update(t.Context(), func(ctx context.Context, tx *ledger.Transaction) error {
		called = true
		tx.State.Changes["expired"] = record.Change{ID: "expired"}
		<-ctx.Done()
		return nil
	})
	require.ErrorIs(t, err, context.DeadlineExceeded, "expired Update: %v, callback %v", err, called)
	require.True(t, called, "expired Update: %v, callback %v", err, called)
	require.Equal(t, before, f.snapshot(t), "expired transaction committed")
}

func TestWriterLockIsBoundedAndDoesNotBlockReadsOrInitialization(t *testing.T) {
	f := newFixture(t, "sha1")
	require.NoError(t, f.store.Update(t.Context(), addChange("original")))
	before := f.snapshot(t)
	holder, err := os.OpenFile(f.options.Lockfile, os.O_RDWR, 0)
	require.NoError(t, err)
	defer holder.Close()
	require.NoError(t, syscall.Flock(int(holder.Fd()), syscall.LOCK_EX|syscall.LOCK_NB))
	options := f.options
	options.LockTimeout = 75 * time.Millisecond
	initialized := make(chan *ledger.Store, 1)
	initErrors := make(chan error, 1)
	go func() { store, err := ledger.New(f.repo, options); initErrors <- err; initialized <- store }()
	require.NoError(t, receive(t, initErrors))
	other := receive(t, initialized)
	readCtx, cancel := context.WithTimeout(t.Context(), time.Second)
	snapshot, err := other.Read(readCtx)
	cancel()
	require.NoError(t, err)
	require.Equal(t, before, snapshot, "reader lost snapshot while writer locked")

	called := false
	err = other.Update(t.Context(), func(context.Context, *ledger.Transaction) error { called = true; return nil })
	require.ErrorIs(t, err, ledger.ErrLockTimeout, "locked Update: %v, callback %v", err, called)
	require.ErrorIs(t, err, context.DeadlineExceeded, "locked Update: %v, callback %v", err, called)
	require.False(t, called, "locked Update: %v, callback %v", err, called)
	require.NoError(t, holder.Close())
	require.NoError(t, other.Update(t.Context(), addChange("unlocked")))
}

func TestConcurrentStoresPreserveEveryAcceptedUpdate(t *testing.T) {
	f := newFixture(t, "sha1")
	start, done := make(chan struct{}), make(chan error, 6)
	for i := range 6 {
		store, err := ledger.New(f.repo, f.options)
		require.NoError(t, err)
		go func() {
			<-start
			done <- store.Update(t.Context(), addChange(record.ChangeID(fmt.Sprintf("writer-%d", i))))
		}()
	}
	close(start)
	for range 6 {
		require.NoError(t, receive(t, done))
	}
	got := len(f.snapshot(t).State.Changes)
	require.Equal(t, 6, got, "lost concurrent updates: %d changes", got)
}

func TestSourcePinsSurviveCollectionAndRepairWithoutNewState(t *testing.T) {
	f := newFixture(t, "sha1")
	source := f.source(t, "version 3.0\n")
	require.NoError(t, f.store.Update(t.Context(), func(_ context.Context, tx *ledger.Transaction) error {
		tx.State.Revisions["revision"] = record.Revision{ID: "revision", Source: source}
		return nil
	}))
	before := f.snapshot(t)
	name := ledger.PinsPrefix + string(source.Commit)
	require.NoError(t, f.repo.UpdateRefs(t.Context(), []git.RefChange{{Name: name, Expected: f.ref(t, name)}}))
	require.NoError(t, f.store.Update(t.Context(), func(context.Context, *ledger.Transaction) error { return nil }))
	require.Equal(t, before.Version, f.snapshot(t).Version, "pin repair changed state history or failed to restore pin")
	require.Equal(t, string(source.Commit), f.ref(t, name).Object, "pin repair changed state history or failed to restore pin")
	require.NoError(t, f.store.Update(t.Context(), func(_ context.Context, tx *ledger.Transaction) error {
		delete(tx.State.Revisions, "revision")
		return nil
	}))
	runGit(t, f.repo.Root, "gc", "--prune=now")
	for _, id := range []record.ObjectID{source.Commit, source.Tree} {
		_, err := f.repo.ObjectType(t.Context(), string(id))
		require.NoError(t, err, "historical source was collected: %v", err)
	}
}

func TestSourceValidationDoesNotPublishInvalidObjects(t *testing.T) {
	f := newFixture(t, "sha1")
	a, b := f.source(t, "version 1\n"), f.source(t, "version 2\n")
	require.NoError(t, f.store.Update(t.Context(), addChange("initial")))
	before := f.snapshot(t)
	tests := []struct {
		name   string
		source record.Source
	}{
		{"abbreviated ID", record.Source{Tree: "1234567"}},
		{"wrong type", record.Source{Tree: a.Commit}},
		{"missing object", record.Source{Tree: record.ObjectID(strings.Repeat("a", 40))}},
		{"tree mismatch", record.Source{Commit: a.Commit, Tree: b.Tree}},
		{"contradictory types", record.Source{Commit: a.Commit, Tree: a.Commit}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := f.store.Update(t.Context(), func(_ context.Context, tx *ledger.Transaction) error {
				tx.State.Revisions["bad"] = record.Revision{ID: "bad", Source: test.source}
				return nil
			})
			require.Error(t, err, "accepted invalid source")
			require.Equal(t, before, f.snapshot(t), "invalid source committed")
		})
	}
	name := ledger.PinsPrefix + string(a.Commit)
	require.NoError(t, f.repo.UpdateRefs(t.Context(), []git.RefChange{{Name: name, Desired: git.RefValue{Exists: true, Object: string(b.Commit)}}}))
	err := f.store.Update(t.Context(), func(_ context.Context, tx *ledger.Transaction) error {
		tx.State.Revisions["bad-pin"] = record.Revision{ID: "bad-pin", Source: a}
		return nil
	})
	require.ErrorIs(t, err, ledger.ErrInvalidState, "corrupt pin handling: %v", err)
	require.Equal(t, string(b.Commit), f.ref(t, name).Object, "corrupt pin handling: %v", err)
	require.Equal(t, before, f.snapshot(t), "corrupt pin allowed state commit")
}

func TestReadDistinguishesAbsentStateFromCorruption(t *testing.T) {
	f := newFixture(t, "sha1")
	valid, err := ledger.Encode(ledger.NewState())
	require.NoError(t, err)
	blob, err := f.repo.WriteBlob(t.Context(), valid)
	require.NoError(t, err)
	for _, test := range []struct {
		name    string
		mode    uint32
		content []byte
	}{
		{"wrong-name.json", 0o100644, valid},
		{"state.json", 0o100755, valid},
		{"state.json", 0o100644, []byte(`{"schema":999}`)},
	} {
		data, err := f.repo.WriteBlob(t.Context(), test.content)
		require.NoError(t, err)
		tree, err := f.repo.WriteTree(t.Context(), []git.TreeEntry{{Name: test.name, Mode: test.mode, Type: "blob", Object: data}})
		require.NoError(t, err)
		commit := f.commit(t, tree)
		require.NoError(t, f.repo.UpdateRefs(t.Context(), []git.RefChange{{Name: ledger.StateRef, Expected: f.ref(t, ledger.StateRef), Desired: git.RefValue{Exists: true, Object: commit}}}))
		_, err = f.store.Read(t.Context())
		require.Error(t, err, "corrupt state read: %v", err)
		require.NotErrorIs(t, err, ledger.ErrNoState, "corrupt state read: %v", err)
	}
	require.NoError(t, f.repo.UpdateRefs(t.Context(), []git.RefChange{{Name: ledger.StateRef, Expected: f.ref(t, ledger.StateRef), Desired: git.RefValue{Exists: true, Object: blob}}}))
	_, err = f.store.Read(t.Context())
	require.ErrorIs(t, err, ledger.ErrInvalidState, "non-commit state: %v", err)

	called := false
	err = f.store.Update(t.Context(), func(context.Context, *ledger.Transaction) error { called = true; return nil })
	require.Error(t, err, "Update overwrote corrupt state: %v, callback %v", err, called)
	require.False(t, called, "Update overwrote corrupt state: %v, callback %v", err, called)
}

func TestUncertainCommitMustBeReadBackBeforeRetry(t *testing.T) {
	f := newFixture(t, "sha1")
	realGit, err := exec.LookPath("git")
	require.NoError(t, err)
	wrapper := filepath.Join(t.TempDir(), "git-lost-ack")
	quotedGit := "'" + strings.ReplaceAll(realGit, "'", "'\"'\"'") + "'"
	script := "#!/bin/sh\nfor arg do\n if [ \"$arg\" = update-ref ]; then\n  " + quotedGit + " \"$@\" || exit $?\n  kill -KILL $$\n fi\ndone\nexec " + quotedGit + " \"$@\"\n"
	require.NoError(t, os.WriteFile(wrapper, []byte(script), 0o700))
	repo := *f.repo
	repo.Executable = wrapper
	uncertain, err := ledger.New(&repo, f.options)
	require.NoError(t, err)
	calls := 0
	err = uncertain.Update(t.Context(), func(ctx context.Context, tx *ledger.Transaction) error {
		calls++
		return addChange("accepted")(ctx, tx)
	})
	require.ErrorIs(t, err, ledger.ErrCommitUncertain, "lost commit acknowledgement: %v, calls %d", err, calls)
	require.Equal(t, 1, calls, "lost commit acknowledgement: %v, calls %d", err, calls)
	require.Equal(t, record.ChangeID("accepted"), f.snapshot(t).State.Changes["accepted"].ID, "fixture did not commit before losing acknowledgement")
	require.NoError(t, f.store.Update(t.Context(), addChange("later")))
	require.Len(t, f.snapshot(t).State.Changes, 2, "recovery lost committed state or writer lock remained held")
}

func TestNewCreatesOnlyConfiguredLockAndPreservesExistingFile(t *testing.T) {
	f := newFixture(t, "sha1")
	file, err := os.Stat(f.options.Lockfile)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), file.Mode().Perm(), "lock permissions: %v", file.Mode())

	dir, err := os.Stat(filepath.Dir(f.options.Lockfile))
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o700), dir.Mode().Perm(), "lock directory permissions: %v", dir.Mode())
	require.NoError(t, os.WriteFile(f.options.Lockfile, []byte("preserve"), 0o600))
	_, err = ledger.New(f.repo, f.options)
	require.NoError(t, err)
	content, err := os.ReadFile(f.options.Lockfile)
	require.NoError(t, err)
	require.Equal(t, "preserve", string(content), "initialization truncated lock or created state")
	require.False(t, f.ref(t, ledger.StateRef).Exists, "initialization truncated lock or created state")

	for _, options := range []ledger.Options{{}, {Lockfile: "relative"}, {Lockfile: f.options.Lockfile, LockTimeout: -1}, {Lockfile: f.options.Lockfile, OperationTimeout: -1}} {
		_, err := ledger.New(f.repo, options)
		require.Error(t, err, "accepted options %+v", options)
	}
	_, err = ledger.New(nil, f.options)
	require.Error(t, err, "accepted unopened repository")
}
