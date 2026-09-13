package sqlite_test

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/herbygillot/dockhand/v2/internal/record"
	"github.com/herbygillot/dockhand/v2/internal/state"
	"github.com/herbygillot/dockhand/v2/internal/state/sqlite"
	"github.com/herbygillot/dockhand/v2/internal/workflow"
	"github.com/stretchr/testify/require"
)

func openStore(t *testing.T, path string) *sqlite.Store {
	t.Helper()
	s, err := sqlite.Open(t.Context(), path, sqlite.Options{BusyTimeout: time.Second})
	require.NoError(t, err)
	t.Cleanup(func() { s.Close() })
	return s
}
func repository(t *testing.T, s *sqlite.Store, name string) record.Repository {
	t.Helper()
	r, err := s.RegisterRepository(t.Context(), filepath.Join(t.TempDir(), name))
	require.NoError(t, err)
	return r
}
func source() record.Source {
	return record.Source{Tree: record.ObjectID(strings.Repeat("a", 40)), Commit: record.ObjectID(strings.Repeat("b", 40))}
}
func seed(t *testing.T, s *sqlite.Store, r record.Repository, id string) workflow.Receipt {
	t.Helper()
	now := time.Now().UTC().Truncate(time.Millisecond)
	require.NoError(t, s.Update(t.Context(), r.ID, func(ctx context.Context, tx state.Tx) error {
		if err := tx.PutChange(ctx, record.Change{ID: record.ChangeID(id), Branch: "shared-name", CurrentRevision: record.RevisionID(id), Disposition: record.ChangeOpen, CreatedAt: now}); err != nil {
			return err
		}
		return tx.PutRevision(ctx, record.Revision{ID: record.RevisionID(id), ChangeID: record.ChangeID(id), Source: source(), CreatedAt: now})
	}))
	e := workflow.Engine{State: s, Repository: r.ID}
	receipt, err := e.Submit(t.Context(), workflow.Request{ID: record.RequestID(id), Spec: record.JobSpec{Action: record.Verify, InputRevision: record.RevisionID(id), Targets: []record.Target{{Name: "fixture", Portfile: "Portfile"}}, Destination: record.VerificationComplete, Verification: record.VerificationRequired, Build: &record.BuildConfig{Provider: "test", Platform: record.Platform{OS: "darwin", Version: "25", Architecture: "arm64"}, EnvironmentDigest: "fixture", Tests: record.TestDeclared}}})
	require.NoError(t, err)
	return receipt
}
func TestRepositoryScopeAndImmutableReferences(t *testing.T) {
	s := openStore(t, filepath.Join(t.TempDir(), "state.db"))
	a, b := repository(t, s, "a"), repository(t, s, "b")
	first, second := seed(t, s, a, "a"), seed(t, s, b, "b")
	require.NoError(t, s.View(t.Context(), a.ID, func(ctx context.Context, r state.Reader) error {
		jobs, err := r.Jobs(ctx, state.Query{})
		require.NoError(t, err)
		require.Len(t, jobs, 1)
		require.Equal(t, first.JobID, jobs[0].ID)
		_, err = r.Job(ctx, second.JobID)
		require.ErrorIs(t, err, state.ErrNotFound)
		return nil
	}))
	for _, fn := range []func(context.Context, state.Tx) error{
		func(ctx context.Context, tx state.Tx) error {
			return tx.PutChange(ctx, record.Change{ID: "a", Disposition: record.ChangeOpen})
		},
		func(ctx context.Context, tx state.Tx) error {
			return tx.PutRevision(ctx, record.Revision{ID: "foreign", ChangeID: "a", Source: source()})
		},
		func(ctx context.Context, tx state.Tx) error {
			return tx.PutRequest(ctx, record.AcceptedRequest{ID: "a", Kind: record.JobRequest, Payload: []byte("intent"), AcceptedAt: time.Now()})
		},
	} {
		require.ErrorIs(t, s.Update(t.Context(), b.ID, fn), state.ErrConflict)
	}
	require.ErrorIs(t, s.Update(t.Context(), a.ID, func(ctx context.Context, tx state.Tx) error {
		revision, err := tx.Revision(ctx, "a")
		if err != nil {
			return err
		}
		revision.Source.Tree = record.ObjectID(strings.Repeat("c", 40))
		return tx.PutRevision(ctx, revision)
	}), state.ErrConflict)
	require.ErrorIs(t, s.Update(t.Context(), a.ID, func(ctx context.Context, tx state.Tx) error {
		job, err := tx.Job(ctx, first.JobID)
		if err != nil {
			return err
		}
		job.Spec.Build.EnvironmentDigest = "changed"
		return tx.PutJob(ctx, job)
	}), state.ErrConflict)
}
func TestRollbackSnapshotAndReadOnlyContract(t *testing.T) {
	s := openStore(t, filepath.Join(t.TempDir(), "state.db"))
	r := repository(t, s, "repo")
	seed(t, s, r, "change")
	sentinel := errors.New("stop")
	require.ErrorIs(t, s.Update(t.Context(), r.ID, func(ctx context.Context, tx state.Tx) error {
		v, err := tx.Change(ctx, "change")
		if err != nil {
			return err
		}
		v.Branch = "rolled-back"
		if err = tx.PutChange(ctx, v); err != nil {
			return err
		}
		return sentinel
	}), sentinel)
	ready, proceed := make(chan struct{}), make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- s.View(t.Context(), r.ID, func(ctx context.Context, reader state.Reader) error {
			first, err := reader.Change(ctx, "change")
			if err != nil {
				return err
			}
			close(ready)
			<-proceed
			second, err := reader.Change(ctx, "change")
			if err != nil {
				return err
			}
			if first.Branch != "shared-name" || first.Branch != second.Branch {
				return errors.New("snapshot changed")
			}
			return nil
		})
	}()
	<-ready
	require.NoError(t, s.Update(t.Context(), r.ID, func(ctx context.Context, tx state.Tx) error {
		v, err := tx.Change(ctx, "change")
		if err != nil {
			return err
		}
		v.Branch = "new"
		return tx.PutChange(ctx, v)
	}))
	close(proceed)
	require.NoError(t, <-done)
	var escaped state.Reader
	require.NoError(t, s.View(t.Context(), r.ID, func(ctx context.Context, reader state.Reader) error {
		escaped = reader
		v, err := reader.Change(ctx, "change")
		require.NoError(t, err)
		require.Equal(t, "new", v.Branch)
		require.ErrorIs(t, reader.(state.Writer).PutChange(ctx, v), state.ErrReadOnly)
		require.ErrorIs(t, s.Update(ctx, r.ID, func(context.Context, state.Tx) error { return nil }), state.ErrInvalid)
		return nil
	}))
	_, err := escaped.Change(t.Context(), "change")
	require.ErrorIs(t, err, state.ErrInvalid)
	ctx, cancel := context.WithCancel(t.Context())
	require.ErrorIs(t, s.Update(ctx, r.ID, func(ctx context.Context, tx state.Tx) error {
		v, err := tx.Change(ctx, "change")
		if err != nil {
			return err
		}
		v.Branch = "canceled"
		if err = tx.PutChange(ctx, v); err != nil {
			return err
		}
		cancel()
		return nil
	}), context.Canceled)
	require.NoError(t, s.View(t.Context(), r.ID, func(ctx context.Context, r state.Reader) error {
		v, err := r.Change(ctx, "change")
		require.Equal(t, "new", v.Branch)
		return err
	}))
}
func TestConcurrentInitializationRegistrationAndSchemaProtection(t *testing.T) {
	path := filepath.Join(t.TempDir(), "private", "state.db")
	common := filepath.Join(t.TempDir(), "common")
	var wg sync.WaitGroup
	errs := make([]error, 8)
	ids := make([]record.RepositoryID, 8)
	for i := range errs {
		wg.Go(func() {
			s, err := sqlite.Open(t.Context(), path, sqlite.Options{})
			if err != nil {
				errs[i] = err
				return
			}
			defer s.Close()
			r, err := s.RegisterRepository(t.Context(), common)
			ids[i], errs[i] = r.ID, err
		})
	}
	wg.Wait()
	for i, err := range errs {
		require.NoError(t, err)
		require.Equal(t, ids[0], ids[i])
	}
	info, err := os.Stat(path)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0600), info.Mode().Perm())
	missing := filepath.Join(t.TempDir(), "none", "state.db")
	_, err = sqlite.Open(t.Context(), missing, sqlite.Options{ReadOnly: true})
	require.ErrorIs(t, err, state.ErrNoDatabase)
	require.NoDirExists(t, filepath.Dir(missing))
	readOnly, err := sqlite.Open(t.Context(), path, sqlite.Options{ReadOnly: true})
	require.NoError(t, err)
	defer readOnly.Close()
	_, err = readOnly.RegisterRepository(t.Context(), common)
	require.ErrorIs(t, err, state.ErrReadOnly)
	require.ErrorIs(t, readOnly.Update(t.Context(), ids[0], func(context.Context, state.Tx) error { return nil }), state.ErrReadOnly)
	raw, err := sql.Open("sqlite", path)
	require.NoError(t, err)
	defer raw.Close()
	_, err = raw.Exec("PRAGMA user_version=3")
	require.NoError(t, err)
	_, err = sqlite.Open(t.Context(), path, sqlite.Options{})
	require.ErrorIs(t, err, state.ErrSchema)
	other := filepath.Join(t.TempDir(), "unrelated.db")
	db, err := sql.Open("sqlite", other)
	require.NoError(t, err)
	defer db.Close()
	_, err = db.Exec("CREATE TABLE important(value TEXT)")
	require.NoError(t, err)
	_, err = sqlite.Open(t.Context(), other, sqlite.Options{})
	require.ErrorIs(t, err, state.ErrSchema)
	var mode string
	require.NoError(t, db.QueryRow("PRAGMA journal_mode").Scan(&mode))
	require.Equal(t, "delete", mode)
	var count int
	require.NoError(t, db.QueryRow("SELECT count(*) FROM sqlite_schema WHERE type='table'").Scan(&count))
	require.Equal(t, 1, count)
}
func TestResourceIdentityIsGlobalAndSubmissionHistoryPersists(t *testing.T) {
	s := openStore(t, filepath.Join(t.TempDir(), "state.db"))
	a, b := repository(t, s, "a"), repository(t, s, "b")
	ja, jb := seed(t, s, a, "a"), seed(t, s, b, "b")
	insert := func(repo record.Repository, job record.JobID, aid string, handle string) error {
		return s.Update(t.Context(), repo.ID, func(ctx context.Context, tx state.Tx) error {
			j, err := tx.Job(ctx, job)
			if err != nil {
				return err
			}
			if err = tx.PutAttempt(ctx, record.Attempt{ID: record.AttemptID(aid), JobID: job, TargetID: "target", Spec: record.BuildSpec{RevisionID: j.Spec.InputRevision, Source: j.Spec.Source, Target: j.Spec.Targets[0], Config: *j.Spec.Build}, State: record.AttemptQueued, CreatedAt: j.AcceptedAt}); err != nil {
				return err
			}
			sub := record.Submission{ID: record.RequestID(aid), AttemptID: record.AttemptID(aid), Sequence: 1, Provider: "test", CreatedAt: j.AcceptedAt}
			if err = tx.PutSubmission(ctx, sub); err != nil {
				return err
			}
			sub.ClosedAt = &j.AcceptedAt
			if err = tx.PutSubmission(ctx, sub); err != nil {
				return err
			}
			next := sub
			next.ID += "_next"
			next.Sequence++
			next.ClosedAt = nil
			if err = tx.PutSubmission(ctx, next); err != nil {
				return err
			}
			return tx.PutResource(ctx, record.Resource{ID: record.ResourceID(aid), AttemptID: record.AttemptID(aid), SubmissionID: sub.ID, Handle: record.ResourceHandle{Provider: "test", ID: handle}, State: record.ResourceUncertain})
		})
	}
	require.NoError(t, insert(a, ja.JobID, "aa", "same"))
	require.ErrorIs(t, insert(b, jb.JobID, "bb", "same"), state.ErrConflict)
	require.NoError(t, s.View(t.Context(), a.ID, func(ctx context.Context, r state.Reader) error {
		ss, err := r.SubmissionsForAttempt(ctx, "aa")
		require.NoError(t, err)
		require.Len(t, ss, 2)
		require.NotNil(t, ss[0].ClosedAt)
		resources, err := r.ResourcesForAttempt(ctx, "aa")
		require.NoError(t, err)
		require.Equal(t, ss[0].ID, resources[0].SubmissionID)
		return nil
	}))
}

func TestBranchLookupIsRepositoryScopedAndExcludesClosedChanges(t *testing.T) {
	s := openStore(t, filepath.Join(t.TempDir(), "state.db"))
	a, b := repository(t, s, "a"), repository(t, s, "b")
	seed(t, s, a, "first")
	seed(t, s, b, "second")
	for _, test := range []struct {
		repo record.RepositoryID
		id   record.ChangeID
	}{{a.ID, "first"}, {b.ID, "second"}} {
		require.NoError(t, s.View(t.Context(), test.repo, func(ctx context.Context, r state.Reader) error {
			change, err := r.OpenChangeByBranch(ctx, "shared-name")
			require.NoError(t, err)
			require.Equal(t, test.id, change.ID)
			_, err = r.OpenChangeByBranch(ctx, "missing")
			require.ErrorIs(t, err, state.ErrNotFound)
			return nil
		}))
	}
	require.NoError(t, s.Update(t.Context(), a.ID, func(ctx context.Context, tx state.Tx) error {
		change, err := tx.OpenChangeByBranch(ctx, "shared-name")
		require.NoError(t, err)
		change.Disposition = record.ChangeClosed
		return tx.PutChange(ctx, change)
	}))
	require.NoError(t, s.View(t.Context(), a.ID, func(ctx context.Context, r state.Reader) error {
		_, err := r.OpenChangeByBranch(ctx, "shared-name")
		require.ErrorIs(t, err, state.ErrNotFound)
		return nil
	}))
}
