package workflow_test

import (
	"context"
	"errors"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/publish"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/herbygillot/dockhand/internal/forge"
	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/macports/patchcheck"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/state"
	"github.com/herbygillot/dockhand/internal/verify"
	"github.com/herbygillot/dockhand/internal/workflow"
	"github.com/herbygillot/dockhand/internal/workflow/preparation"
	"github.com/stretchr/testify/require"
)

type prepareFunc func(context.Context, preparation.Request) (preparation.Result, error)

func (fn prepareFunc) Prepare(ctx context.Context, r preparation.Request) (preparation.Result, error) {
	return fn(ctx, r)
}

func preparationFixture(t *testing.T, verification bool) (*fixture, workflow.Request) {
	t.Helper()
	f, _ := bindingFixture(t)
	commit, tree, err := f.repo.Branch(t.Context(), "candidate")
	require.NoError(t, err)
	req := workflow.PreparationRequest{Action: record.BumpRevision, ID: "prepare", SourceBranch: "master",
		Resolution:  workflow.Resolution{Kind: workflow.Fresh, Source: record.Source{Commit: record.ObjectID(commit), Tree: record.ObjectID(tree), Base: record.ObjectID(commit)}, Selection: bindRequest(f, "").Selection, Subject: "Rebuild dependents"},
		Destination: record.BranchReady, Verification: record.VerificationSkipped,
		Author: record.CommitIdentity{Name: "Accepted Author", Email: "accepted@example.invalid"}, Platform: buildPlatform}
	if verification {
		req.Destination, req.Verification, req.Build = record.VerificationComplete, record.VerificationRequired, f.request("").Spec.Build
	}
	bound, err := f.engine.BindPreparation(t.Context(), req)
	require.NoError(t, err)
	f.engine.Preparer = prepareFunc(func(ctx context.Context, r preparation.Request) (preparation.Result, error) {
		// Immutable preparation must not hold the database writer.
		if err := f.store.Update(ctx, f.repository, func(context.Context, state.Tx) error { return nil }); err != nil {
			return preparation.Result{}, err
		}
		before, _, err := f.repo.File(ctx, string(r.Source.Tree), r.Selection.Selector)
		if err != nil {
			return preparation.Result{}, err
		}
		tree, err := f.repo.EditTree(ctx, string(r.Source.Tree), []git.FileEdit{{Path: r.Selection.Selector, Before: before, After: []byte("version 1\nrevision 1\n"), Mode: before.Mode}})
		return preparation.Result{Base: r.Source, Release: r.Release, Target: bound.Request.Spec.Targets[0], PreparedTree: record.ObjectID(tree), Commits: []preparation.CommitIntent{{Subject: "fixture: " + r.Subject, References: r.References}}}, err
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
	t.Parallel()
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
	require.Equal(t, job.Prepared.Source.Commit, status.Changes[0].GeneratedCommit)
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
	require.Contains(t, string(out), "\n\n"+preparation.GeneratedBy())
	require.Contains(t, string(out), sourceCommit+"\nAccepted Author <accepted@example.invalid>\n")
	require.Contains(t, string(out), "fixture: Rebuild dependents\n\n"+preparation.GeneratedBy())
	require.Len(t, status.Changes, 1)
	require.Equal(t, req.Spec.Targets, status.Changes[0].Targets)
	require.Equal(t, job.ResultRevision, status.Changes[0].CurrentRevision)
	retry, err := f.engine.Submit(t.Context(), req)
	require.NoError(t, err)
	require.Equal(t, id, retry.JobID)
	require.Empty(t, f.run(t, id).Advanced)
}

func TestPreparedRevisionUsesExistingVerificationLifecycle(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
			f.engine.Preparer = prepareFunc(func(ctx context.Context, req preparation.Request) (preparation.Result, error) {
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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

func TestRateLimitedPreparationRetainsAcceptedWork(t *testing.T) {
	t.Parallel()
	f, request := preparationFixture(t, false)
	original := f.engine.Preparer
	deadline := f.now().Add(time.Minute)
	f.engine.Preparer = prepareFunc(func(context.Context, preparation.Request) (preparation.Result, error) {
		return preparation.Result{}, &forge.RateLimitError{RetryAt: deadline, Err: errors.New("rate limited")}
	})
	id := submitPreparation(t, f, request)
	f.run(t, id)
	job := f.status(t, id).Jobs[0].Job
	require.Equal(t, record.JobActive, job.State)
	require.Equal(t, deadline, *job.RetryAt)
	require.EqualValues(t, 1, job.ConsecutiveFailures)
	f.engine.Preparer = original
	f.advance(time.Minute)
	f.run(t, id)
	require.NotNil(t, f.status(t, id).Jobs[0].Job.Prepared)
	require.Zero(t, f.status(t, id).Jobs[0].Job.ConsecutiveFailures)
}

func TestCurrentChecksumsDoNotCreateBranchOrPublish(t *testing.T) {
	t.Parallel()
	f, hosting, request := combinedFixture(t, record.RefreshChecksums)
	f.engine.Preparer = prepareFunc(func(_ context.Context, input preparation.Request) (preparation.Result, error) {
		return preparation.Result{Base: input.Source, Target: request.Spec.Targets[0], PreparedTree: input.Source.Tree}, nil
	})
	id := submitPreparation(t, f, request)
	f.run(t, id)
	status := f.status(t, id)
	require.Equal(t, record.JobCompleted, status.Jobs[0].Job.State)
	require.Contains(t, status.Jobs[0].Job.Detail, "already current")
	require.Nil(t, status.Jobs[0].Job.Prepared)
	require.Len(t, status.Changes, 1)
	require.Equal(t, record.ChangeClosed, status.Changes[0].Disposition)
	require.Empty(t, status.Jobs[0].Attempts)
	require.Empty(t, status.Jobs[0].Publications)
	require.Zero(t, hosting.writes)
	require.Zero(t, f.provider.count("submit"))
}

func TestRejectedPatchCreatesTheBranchButNotVerification(t *testing.T) {
	t.Parallel()
	f, req := preparationFixture(t, true)
	original := f.engine.Preparer
	f.engine.Preparer = prepareFunc(func(ctx context.Context, r preparation.Request) (preparation.Result, error) {
		result, err := original.Prepare(ctx, r)
		result.Patches = []patchcheck.Result{{Name: "patch-keep.diff", Checked: true, Applies: true, Detail: "applies"}, {Name: "patch-daemon.diff", Checked: true, Applies: false, Detail: "4 out of 5 hunks failed"}}
		return result, err
	})
	id := submitPreparation(t, f, req)
	var job record.Job
	for i := 0; i < 6; i++ {
		f.run(t, id)
		job = f.status(t, id).Jobs[0].Job
		if job.State != record.JobActive && job.State != record.JobQueued {
			break
		}
	}
	require.Equal(t, record.JobNeedsAttention, job.State)
	require.Equal(t, record.PhasePreparation, job.Phase)
	require.NotNil(t, job.Prepared)
	require.Equal(t, []string{"patch-daemon.diff: 4 out of 5 hunks failed"}, job.Prepared.PatchProblems)
	require.Contains(t, job.Detail, "Prepared branch "+job.Prepared.Branch+"; verification not started because a patch no longer applies")
	require.Contains(t, job.Detail, "patch-daemon.diff: 4 out of 5 hunks failed")
	require.NotEmpty(t, job.ChangeID, "the branch and change exist for a correction")
}

// An update onto an adopted contribution runs through the same durable
// stages as a fresh preparation: the person's choices reach the job, the
// editor's patch findings reach the branch integration gate, and the
// candidate replaces the contribution's branch head as one commit on its
// base with the message it had.
func TestPreparationOntoAContributionKeepsChoicesAndPatchFindings(t *testing.T) {
	t.Parallel()
	f, _ := publicationFixtureWithTracking(t, true)
	previous, err := f.repo.ReadRef(t.Context(), "refs/heads/candidate")
	require.NoError(t, err)
	resolution, err := f.engine.Resolve(t.Context(), workflow.ResolutionRequest{Action: record.BumpRevision, Selection: macports.Selection{Selector: "fixture"}, Platform: buildPlatform})
	require.NoError(t, err)
	require.Equal(t, workflow.Onto, resolution.Kind)
	req := workflow.PreparationRequest{Action: record.BumpRevision, ID: "onto", Resolution: resolution, AllSubports: true, SourceBranch: "master",
		Destination: record.VerificationComplete, Verification: record.VerificationRequired, Build: f.request("").Spec.Build,
		Author: record.CommitIdentity{Name: "Accepted Author", Email: "accepted@example.invalid"}, Platform: buildPlatform}
	bound, err := f.engine.BindPreparation(t.Context(), req)
	require.NoError(t, err)
	spec := bound.Request.Spec
	require.True(t, spec.AllSubports, "the choice reaches the job")
	require.Equal(t, record.ChangeID("change"), spec.ChangeID)
	require.Equal(t, f.source, spec.Source, "the source is the contribution's current revision")
	require.Equal(t, "update to 2", spec.Subject, "the contribution's own subject")
	require.NotNil(t, spec.Preparation.Correction)
	require.Equal(t, record.CorrectionSpec{ChangeID: "change", RevisionID: "publication_revision", Branch: "candidate", PreviousHead: f.source.Commit, RemoteHead: f.source.Commit}, *spec.Preparation.Correction, "no captured candidate: the job prepares it")
	f.engine.Preparer = prepareFunc(func(ctx context.Context, r preparation.Request) (preparation.Result, error) {
		require.Equal(t, f.source, r.Source)
		before, _, err := f.repo.File(ctx, string(r.Source.Tree), r.Selection.Selector)
		if err != nil {
			return preparation.Result{}, err
		}
		tree, err := f.repo.EditTree(ctx, string(r.Source.Tree), []git.FileEdit{{Path: r.Selection.Selector, Before: before, After: []byte("version 2\nrevision 1\n"), Mode: before.Mode}})
		return preparation.Result{Base: r.Source, Target: spec.Targets[0], PreparedTree: record.ObjectID(tree),
			Commits: []preparation.CommitIntent{{Subject: "fixture: " + r.Subject}},
			Patches: []patchcheck.Result{{Name: "patch-daemon.diff", Checked: true, Applies: false, Detail: "4 out of 5 hunks failed"}}}, err
	})
	id := submitPreparation(t, f, bound.Request)
	var job record.Job
	for i := 0; i < 6; i++ {
		f.run(t, id)
		job = f.status(t, id).Jobs[0].Job
		if job.State != record.JobActive && job.State != record.JobQueued {
			break
		}
	}
	require.Equal(t, record.JobNeedsAttention, job.State)
	require.Equal(t, []string{"patch-daemon.diff: 4 out of 5 hunks failed"}, job.Prepared.PatchProblems, "the findings reach the gate")
	require.Contains(t, job.Detail, "verification not started because a patch no longer applies")
	require.Equal(t, "candidate", job.Prepared.Branch, "the candidate lands on the contribution's branch")
	head, err := f.repo.ReadRef(t.Context(), "refs/heads/candidate")
	require.NoError(t, err)
	require.NotEqual(t, previous.Object, head.Object)
	require.Equal(t, string(job.Prepared.Source.Commit), head.Object)
	parent, err := f.repo.SingleParent(t.Context(), head.Object)
	require.NoError(t, err)
	require.Equal(t, string(f.source.Base), parent, "one commit on the contribution's base")
	message, err := f.repo.CommitMessage(t.Context(), head.Object)
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(message, "fixture: update to 2"), message)
	change, err := f.engine.SelectContribution(t.Context(), workflow.ContributionSelector{ChangeID: "change"})
	require.NoError(t, err)
	require.Equal(t, job.ResultRevision, change.CurrentRevision, "the contribution's current revision is the amendment")
}

// An update prepared onto a contribution keeps what the contribution
// already settled: its release scope, rebound onto the candidate before the
// branch moves, its pull request's destination, and its commit message with
// the body and trailers its author wrote.
func TestPreparationOntoAContributionKeepsItsScopeDestinationAndMessage(t *testing.T) {
	t.Parallel()
	scoped := func(t *testing.T) (*fixture, *publicationForge, *record.ReleaseScope) {
		t.Helper()
		f, forge := publicationFixtureWithTracking(t, true)
		fixture := record.Target{Name: "fixture", Portfile: "devel/fixture/Portfile"}
		scope := &record.ReleaseScope{Input: record.ReleaseInput{Portfile: fixture.Portfile, Before: "1", After: "2"}, Affected: []record.ReleaseMember{{Target: fixture, After: record.ReleaseState{Version: "1"}}}}
		fork := filepath.Join(t.TempDir(), "fork.git")
		out, err := exec.CommandContext(t.Context(), "git", "init", "--bare", "-q", fork).CombinedOutput()
		require.NoError(t, err, "%s", out)
		forge.forkRemote, forge.headName = fork, "stranger/ports"
		require.NoError(t, f.store.Update(t.Context(), f.repository, func(ctx context.Context, tx state.Tx) error {
			change, err := tx.Change(ctx, "change")
			if err != nil {
				return err
			}
			if err := tx.PutRevision(ctx, record.Revision{ID: "scoped", ChangeID: change.ID, Previous: change.CurrentRevision, Source: f.source, Scope: scope, CreatedAt: f.now()}); err != nil {
				return err
			}
			if err := tx.PutPullRequest(ctx, record.PullRequest{ID: "pr", ChangeID: change.ID, Ref: record.PullRequestRef{Forge: "fixture", Repository: "author/ports", Number: 7, URL: "https://example.invalid/pull/7"}, HeadRepository: "stranger/ports", HeadBranch: "candidate", BaseBranch: "main", State: record.PullRequestOpen, RemoteHead: f.source.Commit, ObservedAt: f.now()}); err != nil {
				return err
			}
			change.CurrentRevision, change.PullRequestID = "scoped", "pr"
			return tx.PutChange(ctx, change)
		}))
		return f, forge, scope
	}
	bind := func(t *testing.T, f *fixture, id string) workflow.BoundPreparation {
		t.Helper()
		resolution, err := f.engine.Resolve(t.Context(), workflow.ResolutionRequest{Action: record.BumpRevision, Selection: macports.Selection{Selector: "fixture"}, Subject: "rebuild for the new runtime", References: []record.Reference{{Relation: record.ReferenceCloses, URL: "https://trac.macports.org/ticket/74379"}}, Platform: buildPlatform})
		require.NoError(t, err)
		require.Equal(t, workflow.Onto, resolution.Kind)
		bound, err := f.engine.BindPreparation(t.Context(), workflow.PreparationRequest{Action: record.BumpRevision, ID: record.RequestID(id), Resolution: resolution, SourceBranch: "master",
			Destination: record.Published, Verification: record.VerificationRequired, Build: f.request("").Spec.Build, Publication: publish.Options{},
			Author: record.CommitIdentity{Name: "Accepted Author", Email: "accepted@example.invalid"}, Platform: buildPlatform})
		require.NoError(t, err)
		return bound
	}
	edit := func(f *fixture, scope *record.ReleaseScope) {
		f.engine.Preparer = prepareFunc(func(ctx context.Context, r preparation.Request) (preparation.Result, error) {
			before, _, err := f.repo.File(ctx, string(r.Source.Tree), r.Selection.Selector)
			if err != nil {
				return preparation.Result{}, err
			}
			tree, err := f.repo.EditTree(ctx, string(r.Source.Tree), []git.FileEdit{{Path: r.Selection.Selector, Before: before, After: []byte("version 2\nrevision 1\n"), Mode: before.Mode}})
			return preparation.Result{Base: r.Source, Target: record.Target{Name: "fixture", Portfile: "devel/fixture/Portfile"}, PreparedTree: record.ObjectID(tree), Scope: scope,
				Commits: []preparation.CommitIntent{{Subject: "fixture: " + r.Subject, References: r.References}}}, err
		})
	}
	prepare := func(t *testing.T, f *fixture, id record.JobID) record.Job {
		t.Helper()
		var job record.Job
		for range 6 {
			f.run(t, id)
			job = f.status(t, id).Jobs[0].Job
			if job.Prepared != nil && job.Prepared.IntegrationStarted || job.State.Terminal() {
				break
			}
		}
		return job
	}

	t.Run("kept", func(t *testing.T) {
		t.Parallel()
		f, forge, scope := scoped(t)
		bound := bind(t, f, "onto-kept")
		spec := bound.Request.Spec
		require.Equal(t, scope, spec.Preparation.Correction.Scope, "the contribution's scope travels with the correction")
		require.Equal(t, "stranger/ports", spec.PublishTo.HeadRepository, "the pull request's head, whoever owns it")
		require.Equal(t, forge.forkRemote, spec.PublishTo.PushURL)
		require.Equal(t, "author/ports", spec.PublishTo.Repository)
		edit(f, nil)
		job := prepare(t, f, submitPreparation(t, f, bound.Request))
		require.NotEmpty(t, job.ResultRevision, "%s", job.Detail)
		head, err := f.repo.ReadRef(t.Context(), "refs/heads/candidate")
		require.NoError(t, err)
		message, err := f.repo.CommitMessage(t.Context(), head.Object)
		require.NoError(t, err)
		require.Equal(t, "fixture: rebuild for the new runtime\n\nContribution details\n\nCloses: https://trac.macports.org/ticket/74379", strings.TrimRight(message, "\n"), "the author's body stays; the new subject and reference are laid over it")
		revisions := f.status(t, job.ID).Revisions
		var current record.Revision
		for _, revision := range revisions {
			if revision.ID == job.ResultRevision {
				current = revision
			}
		}
		require.NotNil(t, current.Scope, "an edit with no scope of its own carries the contribution's")
		require.True(t, scope.SameMembership(current.Scope))
		require.Equal(t, "1", current.Scope.Affected[0].After.Version, "rebound onto the candidate")
	})
}

// An update onto a contribution whose target is a stub's carrier, a
// py313-foo adopted by hand or chosen by an earlier bump, edits the stub's
// shared release as a fresh bump of the stub would: the stub is recorded
// on the intent and the siblings are authorized to move.
func TestPreparationOntoAStubCarrierEditsTheSharedRelease(t *testing.T) {
	t.Parallel()
	family := func(stub bool) func(macports.Context) map[string]macports.PortInfo {
		return func(macports.Context) map[string]macports.PortInfo {
			owner := map[string]string{}
			if stub {
				owner["dockhand.metadata_only"] = "1"
			}
			return map[string]macports.PortInfo{
				"fixture":       {Name: "fixture", Version: "1", Options: owner},
				"py312-fixture": {Name: "py312-fixture", Version: "1", Options: map[string]string{}},
				"py313-fixture": {Name: "py313-fixture", Version: "1", Options: map[string]string{}},
			}
		}
	}
	for _, stub := range []bool{true, false} {
		t.Run(map[bool]string{true: "stub", false: "ordinary"}[stub], func(t *testing.T) {
			t.Parallel()
			f, _ := publicationFixtureWithTracking(t, true)
			f.engine.Ports.(*boundPorts).snapshot = family(stub)
			carrier := record.Target{Name: "py313-fixture", Portfile: "devel/fixture/Portfile", Subport: "py313-fixture"}
			// A contribution tracked on the carrier, as adopting a hand-made
			// py313-fixture branch records it.
			require.NoError(t, f.store.Update(t.Context(), f.repository, func(ctx context.Context, tx state.Tx) error {
				change := record.Change{ID: "carrier", InitiatingTarget: carrier.Name, Targets: []record.Target{carrier}, Branch: "carrier-branch", Disposition: record.ChangeOpen, CreatedAt: f.now()}
				if err := tx.PutChange(ctx, change); err != nil {
					return err
				}
				if err := tx.PutRevision(ctx, record.Revision{ID: "carrier_revision", ChangeID: change.ID, Source: f.source, CreatedAt: f.now()}); err != nil {
					return err
				}
				change.CurrentRevision = "carrier_revision"
				return tx.PutChange(ctx, change)
			}))
			resolution, err := f.engine.Resolve(t.Context(), workflow.ResolutionRequest{Action: record.BumpRevision, Selection: macports.Selection{Selector: "py313-fixture"}, Subject: "rebuild", Platform: buildPlatform})
			require.NoError(t, err)
			require.Equal(t, workflow.Onto, resolution.Kind)
			require.Equal(t, "py313-fixture", resolution.Selection.Subport)
			bound, err := f.engine.BindPreparation(t.Context(), workflow.PreparationRequest{Action: record.BumpRevision, ID: "onto-carrier", Resolution: resolution, SourceBranch: "master",
				Destination: record.BranchReady, Verification: record.VerificationSkipped, Author: record.CommitIdentity{Name: "A", Email: "a@example.invalid"}, Platform: buildPlatform})
			require.NoError(t, err)
			spec := bound.Request.Spec
			require.Equal(t, carrier, spec.Targets[0], "the carrier stays the target")
			if stub {
				require.Equal(t, "fixture", spec.Preparation.EditIntent.Stub, "the stub is recorded, so the editor keeps its name, its livecheck, and its family")
				require.False(t, spec.Preparation.EditIntent.SharedRelease, "the shared-release authorization is a bump's alone; a revision bump records the stub without it")
			} else {
				require.Empty(t, spec.Preparation.EditIntent.Stub)
				require.False(t, spec.Preparation.EditIntent.SharedRelease, "a named subport of an ordinary Portfile moves its siblings only when asked")
			}
		})
	}
}
