package workflow_test

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/state"
	"github.com/herbygillot/dockhand/internal/state/sqlite"
	"github.com/herbygillot/dockhand/internal/verify"
	"github.com/herbygillot/dockhand/internal/workflow"
	"github.com/stretchr/testify/require"
)

func completeVerification(t *testing.T, f *fixture, request workflow.Request, verdict record.Verdict) record.Attempt {
	t.Helper()
	f.provider.submit = func(_ context.Context, r verify.Request) (verify.Submission, error) { return admitted(r.ID), nil }
	f.provider.observe = terminal(f, verdict)
	receipt, err := f.engine.Submit(t.Context(), request)
	require.NoError(t, err)
	f.run(t, receipt.JobID)
	f.run(t, receipt.JobID)
	return f.attempt(t, receipt.JobID)
}
func reuseRequest(f *fixture, id string) workflow.Request {
	r := f.request(id)
	r.Spec.InputRevision = ""
	r.Spec.Source = f.source
	r.Spec.Build.VerifierDigest = "fixture:v1"
	return r
}
func TestCommittedWorkingTreeReusesOriginalEvidenceAfterRestart(t *testing.T) {
	t.Parallel()
	f, _ := bindingFixture(t)
	checkoutFixture(t, f)
	trackCandidate(t, f)
	file := filepath.Join(f.repo.Root, "devel/fixture/Portfile")
	require.NoError(t, os.WriteFile(file, []byte("version 2\n"), 0600))
	input := bindRequest(f, "working")
	input.Branch = ""
	input.Build.VerifierDigest = "fixture:v1"
	bound, err := f.engine.BindVerification(t.Context(), input)
	require.NoError(t, err)
	original := completeVerification(t, f, bound.Request, record.VerdictPassed)
	require.Empty(t, original.Spec.Source.Commit)
	for _, args := range [][]string{{"add", "devel/fixture/Portfile"}, {"-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "-c", "commit.gpgSign=false", "commit", "-qm", "verified edits"}} {
		cmd := exec.CommandContext(t.Context(), "git", args...)
		cmd.Dir = f.repo.Root
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "%s", out)
	}
	reopened, err := sqlite.Open(t.Context(), f.store.Path(), sqlite.Options{})
	require.NoError(t, err)
	defer reopened.Close()
	f.engine.State = reopened
	input.ID = "committed"
	input.Branch = "candidate"
	next, err := f.engine.BindVerification(t.Context(), input)
	require.NoError(t, err)
	require.Equal(t, original.Spec.Source.Tree, next.Request.Spec.Source.Tree)
	require.NotEmpty(t, next.Request.Spec.Source.Commit)
	receipt, err := f.engine.Submit(t.Context(), next.Request)
	require.NoError(t, err)
	calls := f.provider.count("submit")
	capabilities := f.provider.count("capabilities")
	f.engine.Provider = nil
	f.run(t, receipt.JobID)
	status := f.status(t, receipt.JobID)
	job := status.Jobs[0].Job
	require.Equal(t, record.JobCompleted, job.State)
	require.Equal(t, original.ID, job.ReusedAttempt)
	require.Nil(t, job.AdmittedAt)
	require.Empty(t, status.Jobs[0].Attempts)
	require.Equal(t, &original, status.Jobs[0].Reused)
	require.Equal(t, calls, f.provider.count("submit"))
	require.Equal(t, capabilities, f.provider.count("capabilities"))
	require.True(t, workflow.Reached(status, workflow.Admission))
	require.True(t, workflow.Reached(status, workflow.Completion))
	f.run(t, receipt.JobID)
	require.Equal(t, job, f.status(t, receipt.JobID).Jobs[0].Job)
	require.Equal(t, original, f.attempt(t, original.JobID))
	retry, err := f.engine.Submit(t.Context(), next.Request)
	require.NoError(t, err)
	require.Equal(t, receipt, retry)
}
func TestReuseMissesAndFreshRequestsProduceNewAttempts(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"tree", "variants", "environment", "tests", "fresh", "newer failure", "newer cancellation", "legacy"} {
		t.Run(mode, func(t *testing.T) {
			f := newFixture(t)
			old := reuseRequest(f, "original")
			if mode == "legacy" {
				old.Spec.Build.VerifierDigest = ""
			}
			original := completeVerification(t, f, old, record.VerdictPassed)
			wanted := reuseRequest(f, "new")
			switch mode {
			case "tree":
				wanted.Spec.Source.Commit = ""
				wanted.Spec.Source.Tree = record.ObjectID(strings.Repeat("b", 40))
			case "variants":
				wanted.Spec.Targets[0].Variants = map[string]bool{"debug": true}
			case "environment":
				wanted.Spec.Build.EnvironmentDigest = "changed"
			case "tests":
				wanted.Spec.Build.Tests = record.TestSkip
			case "fresh":
				wanted.Spec.FreshVerification = true
			case "newer failure", "newer cancellation":
				fresh := reuseRequest(f, "failed-recheck")
				fresh.Spec.FreshVerification = true
				verdict := record.VerdictFailed
				if mode == "newer cancellation" {
					verdict = record.VerdictCanceled
				}
				completeVerification(t, f, fresh, verdict)
			}
			receipt, err := f.engine.Submit(t.Context(), wanted)
			require.NoError(t, err)
			f.run(t, receipt.JobID)
			status := f.status(t, receipt.JobID)
			require.Empty(t, status.Jobs[0].Job.ReusedAttempt)
			require.Len(t, status.Jobs[0].Attempts, 1)
			require.NotEqual(t, original.ID, status.Jobs[0].Attempts[0].ID)
			require.NotEmpty(t, status.Jobs[0].Job.ReuseDetail)
		})
	}
}

type reuseFailureStore struct{ state.Store }
type reuseFailureTx struct{ state.Tx }

func (s reuseFailureStore) Update(ctx context.Context, repo record.RepositoryID, fn func(context.Context, state.Tx) error) error {
	return s.Store.Update(ctx, repo, func(ctx context.Context, tx state.Tx) error { return fn(ctx, reuseFailureTx{tx}) })
}
func (t reuseFailureTx) PutJob(ctx context.Context, job record.Job) error {
	if job.ReusedAttempt != "" {
		return errors.New("injected reuse write failure")
	}
	return t.Tx.PutJob(ctx, job)
}
func TestReuseIsAtomicUnderFailedWritesAndCompetingDrivers(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	original := completeVerification(t, f, reuseRequest(f, "original"), record.VerdictPassed)
	receipt, err := f.engine.Submit(t.Context(), reuseRequest(f, "new"))
	require.NoError(t, err)
	f.engine.State = reuseFailureStore{f.store}
	_, err = f.engine.Cycle(t.Context(), workflow.Scope{Jobs: []record.JobID{receipt.JobID}})
	require.ErrorContains(t, err, "injected reuse write failure")
	job := f.status(t, receipt.JobID).Jobs[0].Job
	require.Equal(t, record.JobQueued, job.State)
	require.Empty(t, job.ReusedAttempt)
	f.engine.State = f.store
	other, err := sqlite.Open(t.Context(), f.store.Path(), sqlite.Options{})
	require.NoError(t, err)
	defer other.Close()
	second := *f.engine
	second.State = other
	second.Owner = "second"
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for _, engine := range []*workflow.Engine{f.engine, &second} {
		wg.Go(func() {
			_, err := engine.Cycle(t.Context(), workflow.Scope{Jobs: []record.JobID{receipt.JobID}})
			errs <- err
		})
	}
	wg.Wait()
	require.NoError(t, <-errs)
	require.NoError(t, <-errs)
	require.Equal(t, original.ID, f.status(t, receipt.JobID).Jobs[0].Job.ReusedAttempt)
	require.Empty(t, f.status(t, receipt.JobID).Jobs[0].Attempts)
}

func TestCancellationBeforeReusePreservesOriginalResult(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	original := completeVerification(t, f, reuseRequest(f, "original"), record.VerdictPassed)
	receipt, err := f.engine.Submit(t.Context(), reuseRequest(f, "canceled"))
	require.NoError(t, err)
	f.cancel(t, receipt.JobID)
	f.engine.Provider = nil
	f.run(t, receipt.JobID)
	status := f.status(t, receipt.JobID).Jobs[0]
	require.Equal(t, record.JobCanceled, status.Job.State)
	require.Empty(t, status.Job.ReusedAttempt)
	require.Empty(t, status.Attempts)
	require.Equal(t, original, f.attempt(t, original.JobID))
}

func TestCancellationDuringCapabilitiesRechecksPlannedWork(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	id := f.submit(t, "build")
	paused := &capabilityBarrier{Provider: f.provider, started: make(chan struct{}), proceed: make(chan struct{})}
	first := *f.engine
	first.Provider = paused
	reply := startCycle(t.Context(), &first, id)
	receive(t, paused.started)
	f.cancel(t, id)
	f.run(t, id)
	close(paused.proceed)
	result := receive(t, reply)
	require.NoError(t, result.err)
	require.Empty(t, result.result.Problems)
	require.Equal(t, record.JobCanceled, f.status(t, id).Jobs[0].Job.State)
	require.Zero(t, f.provider.count("submit"))
}

func TestPreparedBumpReusesEvidenceForItsResultTree(t *testing.T) {
	t.Parallel()
	f, request := preparationFixture(t, true)
	request.Spec.Build.VerifierDigest = "fixture:v1"
	id := submitPreparation(t, f, request)
	candidate := candidateJob(t, f, id)
	originalRequest := reuseRequest(f, "candidate-verification")
	originalRequest.Spec.Source = candidate.Prepared.Source
	originalRequest.Spec.Targets = request.Spec.Targets
	originalRequest.Spec.Build = request.Spec.Build
	original := completeVerification(t, f, originalRequest, record.VerdictPassed)
	f.run(t, id)
	require.NotEmpty(t, f.status(t, id).Jobs[0].Job.ResultRevision)
	f.engine.Provider = nil
	f.run(t, id)
	status := f.status(t, id).Jobs[0]
	require.Equal(t, record.JobCompleted, status.Job.State)
	require.Equal(t, original.ID, status.Job.ReusedAttempt)
	require.Empty(t, status.Attempts)
	require.NotEqual(t, status.Job.Spec.Source.Tree, original.Spec.Source.Tree)
	require.Equal(t, status.Job.Prepared.Source.Tree, original.Spec.Source.Tree)
	commit, tree, err := f.repo.Branch(t.Context(), candidate.Prepared.Branch)
	require.NoError(t, err)
	require.Equal(t, string(candidate.Prepared.Source.Commit), commit)
	require.Equal(t, string(original.Spec.Source.Tree), tree)
}

func TestPreparedBumpSelectsExactConfigurationFromRecordedEvidence(t *testing.T) {
	t.Parallel()
	f, request := preparationFixture(t, true)
	config := *request.Spec.Build
	config.VerifierDigest = "fixture:v1"
	request.Spec.Build = nil
	request.Spec.BuildRequirements = &record.BuildRequirements{Provider: config.Provider, Platform: config.Platform, FromSource: config.FromSource, Tests: config.Tests}
	request.Spec.Preparation.VerificationProblem = "select a prepared image"
	id := prepareCombined(t, f, request)
	job := f.status(t, id).Jobs[0].Job
	verified := reuseRequest(f, "recorded-configuration")
	verified.Spec.Source, verified.Spec.Targets, verified.Spec.Build = job.Prepared.Source, job.Spec.Targets, &config
	original := completeVerification(t, f, verified, record.VerdictPassed)
	f.engine.Provider = nil
	f.run(t, id)
	status := f.status(t, id).Jobs[0]
	require.Equal(t, record.JobCompleted, status.Job.State)
	require.Nil(t, status.Job.Spec.Build)
	require.Equal(t, original.ID, status.Job.ReusedAttempt)
	require.Equal(t, config, status.Reused.Spec.Config)
	require.Empty(t, status.Attempts)
}

func TestPreparedBumpRecordedSelectionStopsAtNewerNegativeEvidence(t *testing.T) {
	t.Parallel()
	f, request := preparationFixture(t, true)
	config := *request.Spec.Build
	config.VerifierDigest = "fixture:v1"
	request.Spec.Build = nil
	request.Spec.BuildRequirements = &record.BuildRequirements{Provider: config.Provider, Platform: config.Platform, FromSource: config.FromSource, Tests: config.Tests}
	request.Spec.Preparation.VerificationProblem = "select a prepared image"
	id := prepareCombined(t, f, request)
	job := f.status(t, id).Jobs[0].Job
	verification := func(id string, verdict record.Verdict) record.Attempt {
		candidate := reuseRequest(f, id)
		candidate.Spec.Source, candidate.Spec.Targets, candidate.Spec.Build = job.Prepared.Source, job.Spec.Targets, &config
		candidate.Spec.FreshVerification = verdict != record.VerdictPassed
		return completeVerification(t, f, candidate, verdict)
	}
	verification("older-pass", record.VerdictPassed)
	f.advance(time.Second)
	negative := verification("newer-failure", record.VerdictFailed)
	f.engine.Provider = nil
	f.run(t, id)
	status := f.status(t, id).Jobs[0]
	require.Equal(t, record.JobNeedsAttention, status.Job.State)
	require.Empty(t, status.Job.ReusedAttempt)
	require.Empty(t, status.Attempts)
	require.Contains(t, status.Job.ReuseDetail, string(negative.ID))
	require.Contains(t, status.Job.Detail, "select a prepared image")
}
