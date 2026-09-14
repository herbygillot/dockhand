package workflow_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/herbygillot/dockhand/v2/internal/prepare"
	"github.com/herbygillot/dockhand/v2/internal/record"
	"github.com/herbygillot/dockhand/v2/internal/state"
	"github.com/herbygillot/dockhand/v2/internal/state/sqlite"
	"github.com/herbygillot/dockhand/v2/internal/verify"
	"github.com/herbygillot/dockhand/v2/internal/workflow"
	"github.com/stretchr/testify/require"
)

type resolveFunc func(context.Context, prepare.Request) (record.Release, error)

func (f resolveFunc) ResolveRelease(ctx context.Context, r prepare.Request) (record.Release, error) {
	return f(ctx, r)
}

func resolvedFixture(f *fixture) record.Release {
	return record.Release{Requested: "2.0", Version: "2.0", Forge: "github", Instance: "https://github.com", Repository: "owner/project", Tag: "v2.0", Commit: strings.Repeat("a", 40), ObservedAt: f.now()}
}

func TestVersionBumpCheckpointsReleaseBeforePreparationAndResumesVerification(t *testing.T) {
	for _, automatic := range []bool{false, true} {
		for _, verification := range []bool{false, true} {
			t.Run(fmt.Sprintf("automatic=%t/verification=%t", automatic, verification), func(t *testing.T) {
				f, req := preparationFixture(t, verification)
				req.Spec.Action = record.Bump
				req.Spec.Version = "2.0"
				release := resolvedFixture(f)
				if automatic {
					req.Spec.Version = ""
					release.Requested = ""
					release.CurrentVersion = "1.0"
				}
				var resolves, prepares int
				f.engine.Releases = resolveFunc(func(ctx context.Context, r prepare.Request) (record.Release, error) {
					resolves++
					require.Equal(t, req.Spec.Version, r.Version)
					require.Nil(t, r.Release)
					// Release lookup must leave the shared database writer available.
					err := f.store.Update(ctx, f.repository, func(context.Context, state.Tx) error { return nil })
					return release, err
				})
				original := f.engine.Preparer
				f.engine.Preparer = prepareFunc(func(ctx context.Context, r prepare.Request) (prepare.Result, error) {
					prepares++
					require.Equal(t, &release, r.Release)
					return original.Prepare(ctx, r)
				})
				id := submitPreparation(t, f, req)
				f.run(t, id)
				job := f.status(t, id).Jobs[0].Job
				require.Equal(t, &release, job.ResolvedRelease)
				require.Nil(t, job.Prepared)
				require.Zero(t, prepares)
				reopened, err := sqlite.Open(t.Context(), f.store.Path(), sqlite.Options{})
				require.NoError(t, err)
				defer reopened.Close()
				fresh := *f.engine
				fresh.State = reopened
				fresh.Releases = nil
				f.engine = &fresh
				candidate := candidateJob(t, f, id)
				require.True(t, strings.HasPrefix(candidate.Prepared.Branch, "dockhand/bump/"))
				require.Equal(t, 1, resolves)
				require.Equal(t, 1, prepares)
				f.run(t, id)
				if verification {
					f.provider.submit = func(_ context.Context, r verify.Request) (verify.Submission, error) {
						require.Equal(t, candidate.Prepared.Source, r.Spec.Source)
						return admitted(r.ID), nil
					}
					f.run(t, id)
					f.provider.observe = terminal(f, record.VerdictPassed)
					f.run(t, id)
				}
				job = f.status(t, id).Jobs[0].Job
				require.Equal(t, record.JobCompleted, job.State)
				require.NotEmpty(t, job.ResultRevision)
				require.Equal(t, &release, job.ResolvedRelease)
				err = f.store.Update(t.Context(), f.repository, func(ctx context.Context, tx state.Tx) error {
					current, err := tx.Job(ctx, id)
					if err != nil {
						return err
					}
					current.ResolvedRelease.Commit = strings.Repeat("b", 40)
					return tx.PutJob(ctx, current)
				})
				require.ErrorIs(t, err, state.ErrConflict)
			})
		}
	}
}

type releaseFailureStore struct{ state.Store }
type releaseFailureTx struct{ state.Tx }

func (s releaseFailureStore) Update(ctx context.Context, repo record.RepositoryID, fn func(context.Context, state.Tx) error) error {
	return s.Store.Update(ctx, repo, func(ctx context.Context, tx state.Tx) error { return fn(ctx, releaseFailureTx{tx}) })
}

func (t releaseFailureTx) PutJob(ctx context.Context, job record.Job) error {
	if job.ResolvedRelease != nil {
		return errors.New("injected release checkpoint failure")
	}
	return t.Tx.PutJob(ctx, job)
}

func TestFailedReleaseCheckpointCannotStartPreparation(t *testing.T) {
	f, req := preparationFixture(t, false)
	req.Spec.Action = record.Bump
	req.Spec.Version = "2.0"
	f.engine.Releases = resolveFunc(func(context.Context, prepare.Request) (record.Release, error) { return resolvedFixture(f), nil })
	var prepares int
	f.engine.Preparer = prepareFunc(func(context.Context, prepare.Request) (prepare.Result, error) {
		prepares++
		return prepare.Result{}, nil
	})
	id := submitPreparation(t, f, req)
	f.engine.State = releaseFailureStore{f.store}
	_, err := f.engine.Cycle(t.Context(), workflow.Scope{Jobs: []record.JobID{id}})
	require.ErrorContains(t, err, "injected release checkpoint failure")
	job := f.status(t, id).Jobs[0].Job
	require.Nil(t, job.ResolvedRelease)
	require.Nil(t, job.Prepared)
	require.Zero(t, prepares)
}

func TestReleaseResolutionFencesExpiredAndCanceledClaims(t *testing.T) {
	for _, canceled := range []bool{false, true} {
		t.Run(map[bool]string{false: "expired", true: "canceled"}[canceled], func(t *testing.T) {
			f, req := preparationFixture(t, false)
			req.Spec.Action = record.Bump
			req.Spec.Version = "2.0"
			started, unblock := make(chan struct{}), make(chan struct{})
			var once sync.Once
			releaseFirst := func() { once.Do(func() { close(unblock) }) }
			defer releaseFirst()
			var calls atomic.Int64
			f.engine.Releases = resolveFunc(func(ctx context.Context, r prepare.Request) (record.Release, error) {
				release := resolvedFixture(f)
				if calls.Add(1) == 1 {
					close(started)
					select {
					case <-unblock:
					case <-ctx.Done():
						return record.Release{}, ctx.Err()
					}
				} else {
					release.Commit = strings.Repeat("b", 40)
				}
				return release, nil
			})
			id := submitPreparation(t, f, req)
			running := startCycle(t.Context(), f.engine, id)
			receive(t, started)
			require.Empty(t, f.run(t, id).Advanced)
			if canceled {
				f.cancel(t, id)
			} else {
				f.advance(2 * time.Minute)
			}
			f.run(t, id)
			winner := f.status(t, id).Jobs[0].Job
			releaseFirst()
			reply := receive(t, running)
			require.NoError(t, reply.err)
			require.NotEmpty(t, reply.result.Problems)
			require.Equal(t, winner, f.status(t, id).Jobs[0].Job)
			if canceled {
				require.Equal(t, record.JobCanceled, winner.State)
				require.Nil(t, winner.ResolvedRelease)
			} else {
				require.Equal(t, strings.Repeat("b", 40), winner.ResolvedRelease.Commit)
			}
		})
	}
}

func TestAlreadyCurrentBumpCompletesWithoutPreparationOrVerification(t *testing.T) {
	for _, verification := range []bool{false, true} {
		t.Run(fmt.Sprint(verification), func(t *testing.T) {
			f, req := preparationFixture(t, verification)
			req.Spec.Action = record.Bump
			release := resolvedFixture(f)
			release.Requested = ""
			release.CurrentVersion = "2.0"
			release.NoUpdate = true
			resolves := 0
			f.engine.Releases = resolveFunc(func(context.Context, prepare.Request) (record.Release, error) { resolves++; return release, nil })
			f.engine.Preparer = prepareFunc(func(context.Context, prepare.Request) (prepare.Result, error) {
				t.Error("current port must not be prepared")
				return prepare.Result{}, errors.New("unexpected preparation")
			})
			id := submitPreparation(t, f, req)
			f.run(t, id)
			status := f.status(t, id)
			job := status.Jobs[0].Job
			require.Equal(t, record.JobCompleted, job.State)
			require.True(t, job.ResolvedRelease.NoUpdate)
			require.Contains(t, job.Detail, "Already current")
			require.Nil(t, job.Prepared)
			require.Empty(t, job.ResultRevision)
			require.Nil(t, job.AdmittedAt)
			require.Empty(t, status.Jobs[0].Attempts)
			require.Empty(t, status.Changes)
			require.Zero(t, f.provider.count("capabilities"))
			require.Zero(t, f.provider.count("submit"))
			require.True(t, workflow.Reached(status, workflow.Admission))
			require.True(t, workflow.Reached(status, workflow.Completion))
			reopened, err := sqlite.Open(t.Context(), f.store.Path(), sqlite.Options{})
			require.NoError(t, err)
			defer reopened.Close()
			fresh := *f.engine
			fresh.State = reopened
			fresh.Releases = nil
			f.engine = &fresh
			require.Empty(t, f.run(t, id).Advanced)
			require.Equal(t, 1, resolves)
			require.Equal(t, job, f.status(t, id).Jobs[0].Job)
		})
	}
}

func TestFailedAutomaticObservationIsNotSuccessfulNoOp(t *testing.T) {
	f, req := preparationFixture(t, false)
	req.Spec.Action = record.Bump
	f.engine.Releases = resolveFunc(func(context.Context, prepare.Request) (record.Release, error) {
		return record.Release{}, errors.New("incomplete upstream observation")
	})
	id := submitPreparation(t, f, req)
	f.run(t, id)
	job := f.status(t, id).Jobs[0].Job
	require.Equal(t, record.JobNeedsAttention, job.State)
	require.Nil(t, job.ResolvedRelease)
	require.Nil(t, job.Prepared)
	require.Contains(t, job.Detail, "incomplete upstream observation")
}

func TestNoUpdateCheckpointFailureDoesNotReportSuccess(t *testing.T) {
	f, req := preparationFixture(t, false)
	req.Spec.Action = record.Bump
	release := resolvedFixture(f)
	release.Requested = ""
	release.CurrentVersion = "2.0"
	release.NoUpdate = true
	f.engine.Releases = resolveFunc(func(context.Context, prepare.Request) (record.Release, error) { return release, nil })
	id := submitPreparation(t, f, req)
	f.engine.State = releaseFailureStore{f.store}
	_, err := f.engine.Cycle(t.Context(), workflow.Scope{Jobs: []record.JobID{id}})
	require.Error(t, err)
	job := f.status(t, id).Jobs[0].Job
	require.Equal(t, record.JobActive, job.State)
	require.Nil(t, job.ResolvedRelease)
	require.Nil(t, job.FinishedAt)
}
