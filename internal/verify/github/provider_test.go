package github

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	gh "github.com/google/go-github/v91/github"
	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/state/sqlite"
	"github.com/herbygillot/dockhand/internal/verify"
	"github.com/herbygillot/dockhand/internal/workflow"
	"github.com/stretchr/testify/require"
)

const testWorkflow = `name: lint & build changed ports
on:
  push:
    branches-ignore: [master]
jobs:
  build:
    name: ${{ matrix.os }}
    runs-on: ${{ matrix.os }}
    strategy:
      matrix:
        os: [macos-14, macos-15]
`

type fakeActions struct {
	flow     *gh.Workflow
	runs     []*gh.WorkflowRun
	run      *gh.WorkflowRun
	jobs     []*gh.WorkflowJob
	err      error
	runsErr  error
	canceled int
	logCalls int
}

func (a *fakeActions) Workflow(context.Context, string) (*gh.Workflow, error) { return a.flow, a.err }
func (a *fakeActions) Runs(context.Context, int64, string, string) ([]*gh.WorkflowRun, error) {
	if a.runsErr != nil {
		return nil, a.runsErr
	}
	return a.runs, a.err
}
func (a *fakeActions) Run(_ context.Context, _ int64, attempt int) (*gh.WorkflowRun, error) {
	if attempt != 0 && attempt != a.run.GetRunAttempt() {
		return nil, errors.New("wrong run attempt requested")
	}
	return a.run, a.err
}
func (a *fakeActions) Jobs(context.Context, int64, int) ([]*gh.WorkflowJob, error) {
	return a.jobs, a.err
}
func (a *fakeActions) Cancel(context.Context, int64) error { a.canceled++; return a.err }
func (a *fakeActions) JobLog(context.Context, int64) (io.ReadCloser, error) {
	a.logCalls++
	return io.NopCloser(strings.NewReader("build log\n")), a.err
}

type atCapacity struct{ verify.Provider }

func (atCapacity) Capabilities(context.Context) (verify.Capabilities, error) {
	return verify.Capabilities{Name: ProviderName}, nil
}
func (atCapacity) Submit(context.Context, verify.Request) (verify.Submission, error) {
	return verify.Submission{State: verify.AtCapacity}, nil
}

type fixture struct {
	provider *Provider
	api      *fakeActions
	request  verify.Request
	remote   string
	engine   *workflow.Engine
	job      record.JobID
}

func setup(t *testing.T) *fixture {
	t.Helper()
	root := t.TempDir()
	command := func(args ...string) string {
		t.Helper()
		cmd := exec.CommandContext(t.Context(), "git", args...)
		cmd.Dir = root
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, string(out))
		return strings.TrimSpace(string(out))
	}
	command("init", "-q", "-b", "master")
	command("config", "user.name", "Fixture")
	command("config", "user.email", "fixture@example.invalid")
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".github/workflows"), 0700))
	require.NoError(t, os.WriteFile(filepath.Join(root, WorkflowPath), []byte(testWorkflow), 0600))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "devel/fixture"), 0700))
	require.NoError(t, os.WriteFile(filepath.Join(root, "devel/fixture/Portfile"), []byte("version 1\n"), 0600))
	command("add", ".")
	command("commit", "-qm", "base")
	base := command("rev-parse", "HEAD")
	remote := filepath.Join(t.TempDir(), "remote.git")
	command("init", "--bare", "-q", remote)
	command("push", "-q", remote, "master")
	command("checkout", "-qb", "candidate")
	require.NoError(t, os.WriteFile(filepath.Join(root, "devel/fixture/Portfile"), []byte("version 2\n"), 0600))
	command("commit", "-qam", "fixture: update to 2")
	commit := command("rev-parse", "HEAD")
	tree := command("rev-parse", "HEAD^{tree}")
	repo, err := git.Open(t.Context(), root, "")
	require.NoError(t, err)
	store, err := sqlite.Open(t.Context(), filepath.Join(t.TempDir(), "state.db"), sqlite.Options{})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	repository, err := store.RegisterRepository(t.Context(), repo.CommonDir)
	require.NoError(t, err)
	api := &fakeActions{flow: &gh.Workflow{ID: gh.Ptr(int64(7)), Path: gh.Ptr(WorkflowPath), State: gh.Ptr("active")}}
	p := &Provider{State: store, Repository: repository.ID, Repo: repo, Directory: filepath.Join(t.TempDir(), "coordination"), Actions: func(context.Context, string) (Actions, error) { return api, nil }}
	config, err := BuildConfig(record.Platform{OS: "darwin", Version: "25", Architecture: "arm64"}, Config{WorkflowID: 7, Destination: record.PublicationDestination{Forge: ProviderName, Repository: "macports/macports-ports", HeadRepository: "contributor/macports-ports", BaseBranch: "master", PushURL: remote, BaseURL: remote, LockDirectory: filepath.Join(t.TempDir(), "push-locks")}}, false)
	require.NoError(t, err)
	e := &workflow.Engine{State: store, Repository: repository.ID, Repo: repo, Provider: atCapacity{}, RetryDelay: time.Millisecond, ObserveInterval: time.Millisecond}
	receipt, err := e.Submit(t.Context(), workflow.Request{ID: "fixture", Spec: record.JobSpec{Action: record.Verify, SourceBranch: "candidate", Source: record.Source{Commit: record.ObjectID(commit), Tree: record.ObjectID(tree), Base: record.ObjectID(base)}, Targets: []record.Target{{Name: "fixture", Portfile: "devel/fixture/Portfile"}}, Destination: record.VerificationComplete, Verification: record.VerificationRequired, Build: &config}})
	require.NoError(t, err)
	_, err = e.Cycle(t.Context(), workflow.Scope{Jobs: []record.JobID{receipt.JobID}})
	require.NoError(t, err)
	status, err := e.Status(t.Context(), workflow.Scope{Jobs: []record.JobID{receipt.JobID}})
	require.NoError(t, err)
	require.Len(t, status.Jobs[0].Attempts, 1)
	attempt := status.Jobs[0].Attempts[0]
	e.Provider = nil
	e.Providers = map[string]verify.Provider{ProviderName: p}
	return &fixture{p, api, verify.Request{ID: attempt.SubmissionID, AttemptID: attempt.ID, Spec: attempt.Spec}, remote, e, receipt.JobID}
}

func (f *fixture) ready() {
	f.api.run = &gh.WorkflowRun{ID: gh.Ptr(int64(10)), RunAttempt: gh.Ptr(1), WorkflowID: gh.Ptr(int64(7)), HeadSHA: gh.Ptr(string(f.request.Spec.Source.Commit)), HeadBranch: gh.Ptr("candidate"), Path: gh.Ptr(WorkflowPath), Event: gh.Ptr("push"), Status: gh.Ptr("completed"), Conclusion: gh.Ptr("success"), HTMLURL: gh.Ptr("https://github.com/contributor/macports-ports/actions/runs/10"), Repository: &gh.Repository{FullName: gh.Ptr("contributor/macports-ports")}, HeadRepository: &gh.Repository{FullName: gh.Ptr("contributor/macports-ports")}}
	f.api.runs = []*gh.WorkflowRun{f.api.run}
	for i, name := range []string{"macos-14", "macos-15"} {
		f.api.jobs = append(f.api.jobs, &gh.WorkflowJob{ID: gh.Ptr(int64(i + 100)), RunID: gh.Ptr(int64(10)), RunAttempt: gh.Ptr(int64(1)), HeadSHA: gh.Ptr(string(f.request.Spec.Source.Commit)), Name: gh.Ptr(name), Status: gh.Ptr("completed"), Conclusion: gh.Ptr("success"), HTMLURL: gh.Ptr("https://github.com/contributor/macports-ports/actions/runs/10/job")})
	}
}

func TestPushRecoveryAndDriverCompletion(t *testing.T) {
	f := setup(t)
	submission, err := f.provider.Submit(t.Context(), f.request)
	require.NoError(t, err)
	require.Equal(t, verify.SubmissionUncertain, submission.State, submission.Detail)
	head, err := f.provider.Repo.RemoteHead(t.Context(), f.remote, "candidate")
	require.NoError(t, err)
	require.Equal(t, string(f.request.Spec.Source.Commit), head.Object)
	row, err := f.provider.read(t.Context(), f.request.ID)
	require.NoError(t, err)
	require.Equal(t, record.ExecutionReserved, row.State)
	f.ready()
	// A newly constructed provider must recover solely from persisted intent.
	restarted := *f.provider
	found, err := restarted.Reconcile(t.Context(), f.request.ID, verify.ReconcileOptions{})
	require.NoError(t, err)
	require.Equal(t, verify.RunFound, found.State)
	repeated, err := restarted.Submit(t.Context(), f.request)
	require.NoError(t, err)
	require.Equal(t, found.Submission, repeated)
	observation, err := restarted.Observe(t.Context(), found.Submission.Run)
	require.NoError(t, err)
	require.Equal(t, record.VerdictPassed, observation.Verdict)
	require.Empty(t, observation.Steps)
	require.Nil(t, observation.Environment)
	require.Contains(t, observation.TestOmission, "permits port test failures")
	require.Len(t, observation.Workflow.Jobs, 2)
	require.Eventually(t, func() bool {
		_, err := f.engine.Cycle(t.Context(), workflow.Scope{Jobs: []record.JobID{f.job}})
		require.NoError(t, err)
		status, err := f.engine.Status(t.Context(), workflow.Scope{Jobs: []record.JobID{f.job}})
		require.NoError(t, err)
		return status.Jobs[0].Job.State == record.JobCompleted
	}, 5*time.Second, 10*time.Millisecond)
	status, err := f.engine.Status(t.Context(), workflow.Scope{Jobs: []record.JobID{f.job}})
	require.NoError(t, err)
	require.Equal(t, int64(10), status.Jobs[0].Attempts[0].Evidence.Workflow.RunID)
}

func TestConcurrentSubmitAndClosedRequest(t *testing.T) {
	f := setup(t)
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for range 2 {
		wg.Go(func() { _, err := f.provider.Submit(t.Context(), f.request); errs <- err })
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	closed, err := f.provider.Reconcile(t.Context(), "never-submitted", verify.ReconcileOptions{})
	require.NoError(t, err)
	require.Equal(t, verify.RequestClosed, closed.State)
	request := f.request
	request.ID = "never-submitted"
	result, err := f.provider.Submit(t.Context(), request)
	require.NoError(t, err)
	require.Equal(t, verify.Unsupported, result.State)
	changed := f.request
	changed.Spec.Target.Name = "other"
	_, err = f.provider.Submit(t.Context(), changed)
	require.Error(t, err)
}

func TestNegativeEvidenceAndPinnedRunAttempt(t *testing.T) {
	f := setup(t)
	f.ready()
	submitted, err := f.provider.Submit(t.Context(), f.request)
	require.NoError(t, err)
	for _, tc := range []struct {
		conclusion string
		verdict    record.Verdict
	}{{"failure", record.VerdictFailed}, {"cancelled", record.VerdictCanceled}, {"timed_out", record.VerdictErrored}, {"skipped", record.VerdictBlocked}} {
		f.api.run.Conclusion = gh.Ptr(tc.conclusion)
		observed, err := f.provider.Observe(t.Context(), submitted.Run)
		require.NoError(t, err)
		require.Equal(t, tc.verdict, observed.Verdict)
	}
	f.api.run.Conclusion = gh.Ptr("success")
	f.api.jobs = f.api.jobs[:1]
	observed, err := f.provider.Observe(t.Context(), submitted.Run)
	require.NoError(t, err)
	require.Equal(t, record.VerdictBlocked, observed.Verdict)
	f.api.run.RunAttempt = gh.Ptr(2)
	_, err = f.provider.Observe(t.Context(), submitted.Run)
	require.ErrorContains(t, err, "wrong run attempt")
	require.NoError(t, f.provider.Cancel(t.Context(), submitted.Run))
	require.Zero(t, f.api.canceled)
}

func TestSubmissionRefusalsDoNotPush(t *testing.T) {
	for _, kind := range []string{"disabled", "wrong tree", "variants", "moved branch"} {
		t.Run(kind, func(t *testing.T) {
			f := setup(t)
			switch kind {
			case "disabled":
				f.api.flow.State = gh.Ptr("disabled_manually")
			case "wrong tree":
				f.request.Spec.Source.Tree = f.request.Spec.Source.Commit
			case "variants":
				f.request.Spec.Target.Variants = map[string]bool{"debug": true}
			case "moved branch":
				require.NoError(t, f.provider.Repo.UpdateRefs(t.Context(), []git.RefChange{{Name: "refs/heads/candidate", Desired: git.RefValue{Exists: true, Object: string(f.request.Spec.Source.Base)}, Expected: git.RefValue{Exists: true, Object: string(f.request.Spec.Source.Commit)}}}))
			}
			result, err := f.provider.Submit(t.Context(), f.request)
			require.True(t, err != nil || result.State == verify.Unsupported)
			head, err := f.provider.Repo.RemoteHead(t.Context(), f.remote, "candidate")
			require.NoError(t, err)
			require.False(t, head.Exists)
		})
	}
}

func TestCompletedLogsArePinnedAndCached(t *testing.T) {
	f := setup(t)
	f.ready()
	submission, err := f.provider.Submit(t.Context(), f.request)
	require.NoError(t, err)
	result, err := f.provider.ReadLog(t.Context(), submission.Run, 0, 4096)
	require.NoError(t, err)
	require.True(t, result.Complete)
	require.Contains(t, string(result.Data), "build log")
	require.Equal(t, 2, f.api.logCalls)
	again, err := f.provider.ReadLog(t.Context(), submission.Run, 0, 4096)
	require.NoError(t, err)
	require.Equal(t, result, again)
	require.Equal(t, 2, f.api.logCalls)
}

func TestWorkflowMatrixRejectsUnsupportedTriggers(t *testing.T) {
	matrix, err := workflowMatrix([]byte(testWorkflow))
	require.NoError(t, err)
	require.Equal(t, []string{"macos-14", "macos-15"}, matrix)
	for _, raw := range []string{strings.Replace(testWorkflow, "push:", "pull_request:", 1), strings.Replace(testWorkflow, "[master]", "[master, candidate]", 1), strings.Replace(testWorkflow, "macos-15", "macos-14", 1)} {
		_, err := workflowMatrix([]byte(raw))
		require.Error(t, err)
	}
}

func TestCancellationFencesUncertainPush(t *testing.T) {
	for _, pushed := range []bool{false, true} {
		t.Run(fmt.Sprint("pushed=", pushed), func(t *testing.T) {
			f := setup(t)
			if !pushed {
				f.api.runsErr = errors.New("Actions unavailable before push")
			}
			// Go through the driver so cancellation intent must cross the provider boundary.
			_, err := f.engine.Cycle(t.Context(), workflow.Scope{Jobs: []record.JobID{f.job}})
			require.NoError(t, err)
			row, err := f.provider.read(t.Context(), f.request.ID)
			require.NoError(t, err)
			require.Equal(t, record.ExecutionReserved, row.State)
			head, err := f.provider.Repo.RemoteHead(t.Context(), f.remote, "candidate")
			require.NoError(t, err)
			require.Equal(t, pushed, head.Exists)
			require.NoError(t, f.engine.Control(t.Context(), record.ControlRequest{ID: "cancel-fixture", Kind: record.Cancel, Jobs: []record.JobID{f.job}}))
			// Closure must work even if GitHub is unavailable.
			f.api.err = errors.New("offline")
			require.Eventually(t, func() bool {
				_, err := f.engine.Cycle(t.Context(), workflow.Scope{Jobs: []record.JobID{f.job}})
				require.NoError(t, err)
				status, err := f.engine.Status(t.Context(), workflow.Scope{Jobs: []record.JobID{f.job}})
				require.NoError(t, err)
				return status.Jobs[0].Job.State == record.JobCanceled
			}, 5*time.Second, 10*time.Millisecond)
			restarted := *f.provider
			row, err = restarted.read(t.Context(), f.request.ID)
			require.NoError(t, err)
			require.Equal(t, record.ExecutionClosed, row.State)
			require.False(t, row.Occupied)
			f.api.err, f.api.runsErr = nil, nil
			stale, err := restarted.Submit(t.Context(), f.request)
			require.NoError(t, err)
			require.Equal(t, verify.Unsupported, stale.State)
			after, err := f.provider.Repo.RemoteHead(t.Context(), f.remote, "candidate")
			require.NoError(t, err)
			require.Equal(t, head, after)
		})
	}
}
