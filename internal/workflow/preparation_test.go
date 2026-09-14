package workflow_test

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/prepare"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/state"
	"github.com/herbygillot/dockhand/internal/verify"
	"github.com/herbygillot/dockhand/internal/workflow"
	"github.com/stretchr/testify/require"
)

type prepareFunc func(context.Context, prepare.Request) (prepare.Result, error)

func (fn prepareFunc) Prepare(ctx context.Context, r prepare.Request) (prepare.Result, error) {
	return fn(ctx, r)
}

func preparationFixture(t *testing.T, verification bool) (*fixture, workflow.Request) {
	t.Helper()
	f, _ := bindingFixture(t)
	commit, tree, err := f.repo.Branch(t.Context(), "candidate")
	require.NoError(t, err)
	req := workflow.PreparationRequest{Action: record.BumpRevision, ID: "prepare", SourceBranch: "master", Source: record.Source{Commit: record.ObjectID(commit), Tree: record.ObjectID(tree), Base: record.ObjectID(commit)}, Selection: bindRequest(f, "").Selection,
		Destination: record.BranchReady, Verification: record.VerificationSkipped,
		Author: record.CommitIdentity{Name: "Accepted Author", Email: "accepted@example.invalid"}, Platform: buildPlatform, Reason: "Rebuild dependents"}
	if verification {
		req.Destination, req.Verification, req.Build = record.VerificationComplete, record.VerificationRequired, f.request("").Spec.Build
	}
	bound, err := f.engine.BindPreparation(t.Context(), req)
	require.NoError(t, err)
	f.engine.Preparer = prepareFunc(func(ctx context.Context, r prepare.Request) (prepare.Result, error) {
		// Immutable preparation must not hold the database writer.
		if err := f.store.Update(ctx, f.repository, func(context.Context, state.Tx) error { return nil }); err != nil {
			return prepare.Result{}, err
		}
		before, _, err := f.repo.File(ctx, string(r.Source.Tree), r.Selection.Selector)
		if err != nil {
			return prepare.Result{}, err
		}
		tree, err := f.repo.EditTree(ctx, string(r.Source.Tree), []git.FileEdit{{Path: r.Selection.Selector, Before: before, After: []byte("version 1\nrevision 1\n"), Mode: before.Mode}})
		return prepare.Result{Base: r.Source, Release: r.Release, Target: bound.Request.Spec.Targets[0], PreparedTree: record.ObjectID(tree), Commits: []prepare.CommitIntent{{Subject: "fixture: revbump", Body: r.Reason}}}, err
	})
	return f, bound.Request
}
func submitPreparation(t *testing.T, f *fixture, request workflow.Request) record.JobID {
	t.Helper()
	receipt, err := f.engine.Submit(t.Context(), request)
	require.NoError(t, err)
	return receipt.JobID
}
func candidateJob(t *testing.T, f *fixture, id record.JobID) record.Job {
	t.Helper()
	f.run(t, id)
	job := f.status(t, id).Jobs[0].Job
	require.Equal(t, record.PhasePreparation, job.Phase)
	require.NotNil(t, job.Prepared)
	require.False(t, job.Prepared.IntegrationStarted)
	require.Empty(t, job.ResultRevision)
	actual, err := f.repo.ReadRef(t.Context(), "refs/heads/"+job.Prepared.Branch)
	require.NoError(t, err)
	require.False(t, actual.Exists)
	return job
}

func TestRevisionPreparationCreatesSeparateContributionWithoutProvider(t *testing.T) {
	f, req := preparationFixture(t, false)
	require.Empty(t, f.run(t).Advanced)
	id := submitPreparation(t, f, req)
	candidate := candidateJob(t, f, id)
	f.engine.Preparer = nil // The checkpoint is sufficient after a restart.
	f.run(t, id)
	status := f.status(t, id)
	job := status.Jobs[0].Job
	require.Equal(t, record.JobCompleted, job.State)
	require.Equal(t, record.PhasePreparation, job.Phase)
	require.Equal(t, req.Spec.Source, job.Spec.Source)
	require.Equal(t, candidate.Prepared.Source, job.Prepared.Source)
	require.NotEmpty(t, job.ResultRevision)
	require.Nil(t, job.Claim)
	require.Empty(t, status.Jobs[0].Attempts)
	require.Zero(t, f.provider.count("capabilities"))
	commit, tree, err := f.repo.Branch(t.Context(), job.Prepared.Branch)
	require.NoError(t, err)
	require.Equal(t, string(job.Prepared.Source.Commit), commit)
	require.Equal(t, string(job.Prepared.Source.Tree), tree)
	sourceCommit, _, err := f.repo.Branch(t.Context(), "candidate")
	require.NoError(t, err)
	require.Equal(t, string(req.Spec.Source.Commit), sourceCommit)
	require.NoFileExists(t, filepath.Join(f.repo.CommonDir, "index"))
	out, err := exec.CommandContext(t.Context(), "git", "-C", f.repo.Root, "show", "-s", "--format=%P%n%an <%ae>%n%at%n%B", commit).CombinedOutput()
	require.NoError(t, err)
	require.Contains(t, string(out), sourceCommit+"\nAccepted Author <accepted@example.invalid>\n")
	require.Contains(t, string(out), "fixture: revbump\n\nRebuild dependents")
	require.Len(t, status.Changes, 1)
	require.Equal(t, req.Spec.Targets, status.Changes[0].Targets)
	require.Equal(t, job.ResultRevision, status.Changes[0].CurrentRevision)
	retry, err := f.engine.Submit(t.Context(), req)
	require.NoError(t, err)
	require.Equal(t, id, retry.JobID)
	require.Empty(t, f.run(t, id).Advanced)
}

func TestPreparedRevisionUsesExistingVerificationLifecycle(t *testing.T) {
	f, req := preparationFixture(t, true)
	id := submitPreparation(t, f, req)
	candidate := candidateJob(t, f, id)
	f.run(t, id)
	require.Equal(t, record.PhaseVerification, f.status(t, id).Jobs[0].Job.Phase)
	f.provider.submit = func(_ context.Context, request verify.Request) (verify.Submission, error) {
		require.Equal(t, candidate.Prepared.Source, request.Spec.Source)
		require.NotEmpty(t, request.Spec.RevisionID)
		return admitted(request.ID), nil
	}
	f.run(t, id)
	attempt := f.attempt(t, id)
	require.Equal(t, candidate.Prepared.Source, attempt.Spec.Source)
	require.Equal(t, f.status(t, id).Jobs[0].Job.ResultRevision, attempt.Spec.RevisionID)
	require.Equal(t, req.Spec.Source, f.status(t, id).Jobs[0].Job.Spec.Source)
	f.provider.observe = terminal(f, record.VerdictPassed)
	f.run(t, id)
	job := f.status(t, id).Jobs[0].Job
	require.Equal(t, record.JobCompleted, job.State)
	require.Equal(t, record.PhaseVerification, job.Phase)
	require.Equal(t, 1, f.provider.count("submit"))
	require.Equal(t, 1, f.provider.count("release"))
}

func TestMissingBuildConfigurationPreservesPreparedBranch(t *testing.T) {
	f, req := preparationFixture(t, true)
	req.Spec.Build = nil
	id := submitPreparation(t, f, req)
	candidateJob(t, f, id)
	f.run(t, id)
	f.run(t, id)
	job := f.status(t, id).Jobs[0].Job
	require.Equal(t, record.JobNeedsAttention, job.State)
	require.Contains(t, job.Detail, "no build configuration")
	actual, _, err := f.repo.Branch(t.Context(), job.Prepared.Branch)
	require.NoError(t, err)
	require.Equal(t, string(job.Prepared.Source.Commit), actual)
	require.Zero(t, f.provider.count("submit"))
}

func TestPreparationClaimFencesLateResultsAndCancellation(t *testing.T) {
	for _, cancelJob := range []bool{false, true} {
		t.Run(map[bool]string{false: "expired", true: "canceled"}[cancelJob], func(t *testing.T) {
			f, req := preparationFixture(t, false)
			id := submitPreparation(t, f, req)
			original := f.engine.Preparer
			started, release := make(chan struct{}), make(chan struct{})
			var once sync.Once
			unblock := func() { once.Do(func() { close(release) }) }
			defer unblock()
			var calls atomic.Int64
			f.engine.Preparer = prepareFunc(func(ctx context.Context, req prepare.Request) (prepare.Result, error) {
				if calls.Add(1) == 1 {
					close(started)
					<-release
				}
				return original.Prepare(ctx, req)
			})
			running := startCycle(t.Context(), f.engine, id)
			receive(t, started)
			require.Empty(t, f.run(t, id).Advanced, "another driver must respect the live claim")
			if cancelJob {
				f.cancel(t, id)
			} else {
				f.advance(2 * time.Minute)
			}
			f.run(t, id)
			winner := f.status(t, id).Jobs[0].Job
			if cancelJob {
				require.Equal(t, record.JobCanceled, winner.State)
			} else {
				require.NotNil(t, winner.Prepared)
			}
			unblock()
			reply := receive(t, running)
			require.NoError(t, reply.err)
			require.NotEmpty(t, reply.result.Problems)
			require.Equal(t, winner, f.status(t, id).Jobs[0].Job)
			if cancelJob {
				require.Nil(t, winner.Prepared)
			} else {
				require.Greater(t, winner.ClaimGeneration, uint64(1))
			}
		})
	}
}

type resultFailureStore struct {
	state.Store
	fail   *atomic.Bool
	before func(record.Job)
}
type resultFailureTx struct {
	state.Tx
	store *resultFailureStore
}

func (s *resultFailureStore) Update(ctx context.Context, repo record.RepositoryID, fn func(context.Context, state.Tx) error) error {
	return s.Store.Update(ctx, repo, func(ctx context.Context, tx state.Tx) error { return fn(ctx, &resultFailureTx{tx, s}) })
}
func (t *resultFailureTx) PutJob(ctx context.Context, job record.Job) error {
	if job.ResultRevision != "" {
		if t.store.before != nil {
			t.store.before(job)
		}
		if t.store.fail != nil && t.store.fail.Swap(false) {
			return errors.New("injected state write failure after Git integration")
		}
	}
	return t.Tx.PutJob(ctx, job)
}

func TestInterruptedIntegrationRecoversExactBranchWithoutRepeatingPreparation(t *testing.T) {
	for _, cancelJob := range []bool{false, true} {
		t.Run(map[bool]string{false: "resume", true: "cancel"}[cancelJob], func(t *testing.T) {
			f, req := preparationFixture(t, false)
			id := submitPreparation(t, f, req)
			candidate := candidateJob(t, f, id)
			var fail atomic.Bool
			fail.Store(true)
			f.engine.State = &resultFailureStore{Store: f.store, fail: &fail}
			_, err := f.engine.Cycle(t.Context(), workflow.Scope{Jobs: []record.JobID{id}})
			require.ErrorContains(t, err, "injected state write failure")
			pending := f.status(t, id).Jobs[0].Job
			require.True(t, pending.Prepared.IntegrationStarted)
			require.Empty(t, pending.ResultRevision)
			require.NotNil(t, pending.Claim)
			ref, err := f.repo.ReadRef(t.Context(), "refs/heads/"+candidate.Prepared.Branch)
			require.NoError(t, err)
			require.Equal(t, string(candidate.Prepared.Source.Commit), ref.Object)
			f.engine.State, f.engine.Preparer = f.store, nil
			if cancelJob {
				f.cancel(t, id)
			}
			f.advance(2 * time.Minute)
			f.run(t, id)
			recovered := f.status(t, id).Jobs[0].Job
			expected := record.JobCompleted
			if cancelJob {
				expected = record.JobCanceled
			}
			require.Equal(t, expected, recovered.State)
			require.NotEmpty(t, recovered.ResultRevision)
			require.Equal(t, candidate.Prepared.Source, recovered.Prepared.Source)
			require.Len(t, f.status(t, id).Changes, 1)
		})
	}
}

func TestIntegrationDoesNotOverwriteMovedBranchesOrRecreateMissingOnRecovery(t *testing.T) {
	for _, scenario := range []string{"source-moved", "destination-moved", "missing-after-interruption", "cancel-before-integration", "cancel-missing-after-interruption"} {
		t.Run(scenario, func(t *testing.T) {
			f, req := preparationFixture(t, false)
			id := submitPreparation(t, f, req)
			job := candidateJob(t, f, id)
			var preserved record.Source
			switch scenario {
			case "source-moved":
				preserved = commitPort(t, f, "candidate", "version 2\n")
			case "destination-moved":
				preserved = commitPort(t, f, job.Prepared.Branch, "version 3\n")
			case "missing-after-interruption", "cancel-missing-after-interruption":
				require.NoError(t, f.store.Update(t.Context(), f.repository, func(ctx context.Context, tx state.Tx) error {
					current, err := tx.Job(ctx, id)
					if err != nil {
						return err
					}
					current.Prepared.IntegrationStarted = true
					return tx.PutJob(ctx, current)
				}))
			}
			if scenario == "cancel-before-integration" || scenario == "cancel-missing-after-interruption" {
				f.cancel(t, id)
			}
			f.run(t, id)
			actual := f.status(t, id).Jobs[0].Job
			expected := record.JobNeedsAttention
			if actual.CancelRequestedAt != nil {
				expected = record.JobCanceled
			}
			if scenario == "source-moved" {
				expected = record.JobCompleted
			}
			require.Equal(t, expected, actual.State)
			if scenario == "source-moved" {
				require.NotEmpty(t, actual.ResultRevision)
			} else {
				require.Empty(t, actual.ResultRevision)
			}
			ref, err := f.repo.ReadRef(t.Context(), "refs/heads/"+job.Prepared.Branch)
			require.NoError(t, err)
			if scenario == "destination-moved" {
				require.Equal(t, string(preserved.Commit), ref.Object)
			} else if scenario == "source-moved" {
				require.Equal(t, string(job.Prepared.Source.Commit), ref.Object)
				parent, err := f.repo.SingleParent(t.Context(), ref.Object)
				require.NoError(t, err)
				require.Equal(t, string(req.Spec.Source.Commit), parent)
			} else {
				require.False(t, ref.Exists)
			}
			if scenario == "source-moved" {
				ref, err = f.repo.ReadRef(t.Context(), "refs/heads/candidate")
				require.NoError(t, err)
				require.Equal(t, string(preserved.Commit), ref.Object)
			}
		})
	}
}

func TestIntegrationWaiterRechecksStateUnderBranchLock(t *testing.T) {
	f, req := preparationFixture(t, false)
	id := submitPreparation(t, f, req)
	job := candidateJob(t, f, id)
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	defer unblock()
	f.engine.State = &resultFailureStore{Store: f.store, before: func(record.Job) { close(entered); <-release }}
	first := startCycle(t.Context(), f.engine, id)
	receive(t, entered)
	f.advance(2 * time.Minute)
	other := *f.engine
	other.State = f.store
	waiting := startCycle(t.Context(), &other, id)
	unblock()
	require.NoError(t, receive(t, first).err)
	require.NoError(t, receive(t, waiting).err)
	require.Equal(t, record.JobCompleted, f.status(t, id).Jobs[0].Job.State)
	ref := git.RefValue{Exists: true, Object: string(job.Prepared.Source.Commit)}
	require.NoError(t, f.repo.UpdateRefs(t.Context(), []git.RefChange{{Name: "refs/heads/" + job.Prepared.Branch, Expected: ref}}))
	require.Empty(t, f.run(t, id).Advanced)
	after, err := f.repo.ReadRef(t.Context(), "refs/heads/"+job.Prepared.Branch)
	require.NoError(t, err)
	require.False(t, after.Exists)
	require.NoFileExists(t, filepath.Join(f.repo.CommonDir, "index"))
	_, err = os.Stat(filepath.Join(f.repo.CommonDir, "dockhand", "branch-locks"))
	require.NoError(t, err)
}
