package github

import (
	"context"
	"errors"
	"fmt"
	"github.com/herbygillot/dockhand/internal/macports"
	"io"
	"net/http"
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
	"github.com/herbygillot/dockhand/internal/state"
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
	logCalls int
	jobLog   func(context.Context, int64) (io.ReadCloser, error)
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
func (a *fakeActions) JobLog(ctx context.Context, id int64) (io.ReadCloser, error) {
	a.logCalls++
	if a.jobLog != nil {
		return a.jobLog(ctx, id)
	}
	return io.NopCloser(strings.NewReader("build log\n")), a.err
}

type atCapacity struct{ verify.Provider }

func (atCapacity) Capabilities(context.Context) (verify.Capabilities, error) {
	return verify.Capabilities{Name: verify.ProviderGitHub}, nil
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
	require.NoError(t, os.WriteFile(filepath.Join(root, macports.PortsWorkflowPath), []byte(testWorkflow), 0600))
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
	api := &fakeActions{flow: &gh.Workflow{ID: gh.Ptr(int64(7)), Path: gh.Ptr(macports.PortsWorkflowPath), State: gh.Ptr("active")}}
	p := &Provider{State: store, Repository: repository.ID, Repo: repo, Directory: filepath.Join(t.TempDir(), "coordination"), backend: func(context.Context, string) (actionsAPI, error) { return api, nil }}
	config, err := buildConfig(record.Platform{OS: "darwin", Version: "25", Architecture: "arm64"}, Config{WorkflowID: 7, Destination: record.PublicationDestination{Forge: verify.ProviderGitHub, Repository: "macports/macports-ports", HeadRepository: "contributor/macports-ports", BaseBranch: "master", PushURL: remote, BaseURL: remote, LockDirectory: filepath.Join(t.TempDir(), "push-locks")}}, false)
	require.NoError(t, err)
	e := &workflow.Engine{State: store, Repository: repository.ID, Repo: repo, Provider: atCapacity{}, WaitInterval: time.Millisecond, RetryDelay: time.Millisecond, ObserveInterval: time.Millisecond}
	receipt, err := e.Submit(t.Context(), workflow.Request{ID: "fixture", Spec: record.JobSpec{Action: record.Verify, SourceBranch: "candidate", Source: record.Source{Commit: record.ObjectID(commit), Tree: record.ObjectID(tree), Base: record.ObjectID(base)}, Targets: []record.Target{{Name: "fixture", Portfile: "devel/fixture/Portfile"}}, Destination: record.VerificationComplete, Verification: record.VerificationRequired, Build: &config}})
	require.NoError(t, err)
	_, err = e.Cycle(t.Context(), workflow.Scope{Jobs: []record.JobID{receipt.JobID}})
	require.NoError(t, err)
	status, err := e.Status(t.Context(), workflow.Scope{Jobs: []record.JobID{receipt.JobID}})
	require.NoError(t, err)
	require.Len(t, status.Jobs[0].Attempts, 1)
	attempt := status.Jobs[0].Attempts[0]
	e.Provider = nil
	e.Providers = map[string]verify.Provider{verify.ProviderGitHub: p}
	return &fixture{p, api, verify.Request{ID: attempt.SubmissionID, AttemptID: attempt.ID, Spec: attempt.Spec}, remote, e, receipt.JobID}
}

func (f *fixture) ready() {
	f.api.run = &gh.WorkflowRun{ID: gh.Ptr(int64(10)), RunAttempt: gh.Ptr(1), WorkflowID: gh.Ptr(int64(7)), HeadSHA: gh.Ptr(string(f.request.Spec.Source.Commit)), HeadBranch: gh.Ptr("candidate"), Path: gh.Ptr(macports.PortsWorkflowPath), Event: gh.Ptr("push"), Status: gh.Ptr("completed"), Conclusion: gh.Ptr("success"), HTMLURL: gh.Ptr("https://github.com/contributor/macports-ports/actions/runs/10"), Repository: &gh.Repository{FullName: gh.Ptr("contributor/macports-ports")}, HeadRepository: &gh.Repository{FullName: gh.Ptr("contributor/macports-ports")}}
	f.api.runs = []*gh.WorkflowRun{f.api.run}
	for i, name := range []string{"macos-14", "macos-15"} {
		f.api.jobs = append(f.api.jobs, &gh.WorkflowJob{ID: gh.Ptr(int64(i + 100)), RunID: gh.Ptr(int64(10)), RunAttempt: gh.Ptr(int64(1)), HeadSHA: gh.Ptr(string(f.request.Spec.Source.Commit)), Name: gh.Ptr(name), Status: gh.Ptr("completed"), Conclusion: gh.Ptr("success"), HTMLURL: gh.Ptr("https://github.com/contributor/macports-ports/actions/runs/10/job")})
	}
}

func TestPushRecoveryAndDriverCompletion(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
}

func TestSubmissionRefusalsDoNotPush(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
	f := setup(t)
	f.ready()
	submission, err := f.provider.Submit(t.Context(), f.request)
	require.NoError(t, err)
	result, err := f.provider.ReadLog(t.Context(), submission.Run, 0, 4096)
	require.NoError(t, err)
	require.True(t, result.Complete)
	require.Contains(t, string(result.Data), "build log")
	require.Equal(t, 2, f.api.logCalls)
	restarted := *f.provider
	credentialReads := 0
	restarted.backend = func(context.Context, string) (actionsAPI, error) {
		credentialReads++
		return nil, errors.New("credentials unavailable")
	}
	again, err := restarted.ReadLog(t.Context(), submission.Run, 0, 4096)
	require.NoError(t, err)
	require.Equal(t, result, again)
	chunk, err := restarted.ReadLog(t.Context(), submission.Run, 7, 13)
	require.NoError(t, err)
	require.Equal(t, result.Data[7:20], chunk.Data)
	require.EqualValues(t, 20, chunk.Next)
	require.False(t, chunk.Complete)
	require.Zero(t, credentialReads)
	require.Equal(t, 2, f.api.logCalls)
}

func TestWorkflowMatrixRejectsUnsupportedTriggers(t *testing.T) {
	t.Parallel()
	matrix, err := workflowMatrix([]byte(testWorkflow))
	require.NoError(t, err)
	require.Equal(t, []string{"macos-14", "macos-15"}, matrix)
	for _, raw := range []string{strings.Replace(testWorkflow, "push:", "pull_request:", 1), strings.Replace(testWorkflow, "[master]", "[master, candidate]", 1), strings.Replace(testWorkflow, "macos-15", "macos-14", 1)} {
		_, err := workflowMatrix([]byte(raw))
		require.Error(t, err)
	}
}

func TestCancellationFencesUncertainPush(t *testing.T) {
	t.Parallel()
	for _, pushed := range []bool{false, true} {
		t.Run(fmt.Sprint("pushed=", pushed), func(t *testing.T) {
			f := setup(t)
			if !pushed {
				f.api.runsErr = errors.New("Actions unavailable before push")
			}
			// Go through the driver so cancellation intent must cross the provider boundary.
			require.Eventually(t, func() bool {
				_, err := f.engine.Cycle(t.Context(), workflow.Scope{Jobs: []record.JobID{f.job}})
				require.NoError(t, err)
				status, err := f.engine.Status(t.Context(), workflow.Scope{Jobs: []record.JobID{f.job}})
				require.NoError(t, err)
				return status.Jobs[0].Attempts[0].State == record.AttemptUncertain
			}, 5*time.Second, 10*time.Millisecond)
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

func TestCancellationDetachesOnlyItsOwnTracking(t *testing.T) {
	t.Parallel()
	f := setup(t)
	f.ready()
	f.api.run.Status, f.api.run.Conclusion = gh.Ptr("in_progress"), nil
	first, err := f.provider.Submit(t.Context(), f.request)
	require.NoError(t, err)
	otherRequest := f.request
	otherRequest.ID = "another-observer"
	other, err := f.provider.Submit(t.Context(), otherRequest)
	require.NoError(t, err)
	require.Equal(t, first.Run.RunID, other.Run.RunID)
	// Cancellation must not need GitHub access or affect another observer's run.
	f.api.err = errors.New("GitHub is offline")
	invalid := first.Run
	invalid.RunID = "wrong"
	require.Error(t, f.provider.Cancel(t.Context(), invalid))
	require.NoError(t, f.provider.Cancel(t.Context(), first.Run))
	restarted := *f.provider
	require.NoError(t, restarted.Cancel(t.Context(), first.Run))
	observed, err := restarted.Observe(t.Context(), first.Run)
	require.NoError(t, err)
	require.Equal(t, record.AttemptCanceled, observed.State)
	require.Equal(t, record.VerdictCanceled, observed.Verdict)
	require.Contains(t, observed.Detail, "remote run was not canceled")
	require.Nil(t, observed.Workflow, "local cancellation must not fabricate a remote conclusion")
	_, err = verify.Judge(observed)
	require.NoError(t, err)
	logs, err := restarted.ReadLog(t.Context(), first.Run, 0, 4096)
	require.NoError(t, err)
	require.True(t, logs.Complete)
	stale, err := restarted.Submit(t.Context(), f.request)
	require.NoError(t, err)
	require.Equal(t, verify.Unsupported, stale.State)
	// A driver that lost the cancellation response can recover the same handle.
	recovered, err := restarted.Reconcile(t.Context(), f.request.ID, verify.ReconcileOptions{CancelRequested: true})
	require.NoError(t, err)
	require.Equal(t, verify.RunFound, recovered.State)
	require.Equal(t, first.Run, recovered.Submission.Run)
	f.api.err = nil
	f.api.run.Status, f.api.run.Conclusion = gh.Ptr("completed"), gh.Ptr("success")
	observed, err = restarted.Observe(t.Context(), other.Run)
	require.NoError(t, err)
	require.Equal(t, record.VerdictPassed, observed.Verdict)
}

func TestDriverCancellationDetachesGitHubRun(t *testing.T) {
	t.Parallel()
	f := setup(t)
	f.ready()
	f.api.run.Status, f.api.run.Conclusion = gh.Ptr("in_progress"), nil
	require.Eventually(t, func() bool {
		_, err := f.engine.Cycle(t.Context(), workflow.Scope{Jobs: []record.JobID{f.job}})
		require.NoError(t, err)
		status, err := f.engine.Status(t.Context(), workflow.Scope{Jobs: []record.JobID{f.job}})
		require.NoError(t, err)
		return status.Jobs[0].Attempts[0].State == record.AttemptRunning
	}, 5*time.Second, 10*time.Millisecond)
	require.NoError(t, f.engine.Control(t.Context(), record.ControlRequest{ID: "cancel-admitted", Kind: record.Cancel, Jobs: []record.JobID{f.job}}))
	f.api.err = errors.New("offline")
	require.Eventually(t, func() bool {
		_, err := f.engine.Cycle(t.Context(), workflow.Scope{Jobs: []record.JobID{f.job}})
		require.NoError(t, err)
		status, err := f.engine.Status(t.Context(), workflow.Scope{Jobs: []record.JobID{f.job}})
		require.NoError(t, err)
		return status.Jobs[0].Job.State == record.JobCanceled
	}, 5*time.Second, 10*time.Millisecond)
	require.Equal(t, "in_progress", f.api.run.GetStatus())
}

func TestSourceRequiresWorkflowDetectableChanges(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"deleted patch", "renamed patch", "added patch", "modified patch", "modified Portfile with deleted patch", "outside deletion"} {
		t.Run(kind, func(t *testing.T) {
			f := setup(t)
			repo := f.provider.Repo
			trees, err := repo.CommitTrees(t.Context(), []string{string(f.request.Spec.Source.Base)})
			require.NoError(t, err)
			baseTree, err := repo.EditTree(t.Context(), trees[string(f.request.Spec.Source.Base)], []git.FileEdit{
				{Path: "devel/fixture/files/old.patch", After: []byte("patch bytes"), Mode: 0o100644},
				{Path: "other/port/Portfile", After: []byte("unrelated"), Mode: 0o100644},
			})
			require.NoError(t, err)
			signature := git.Signature{Name: "Fixture", Email: "fixture@example.invalid", When: time.Now()}
			base, err := repo.WriteCommit(t.Context(), git.Commit{Tree: baseTree, Message: "base", Author: signature, Committer: signature})
			require.NoError(t, err)
			old, _, err := repo.File(t.Context(), baseTree, "devel/fixture/files/old.patch")
			require.NoError(t, err)
			edits := []git.FileEdit{{Path: "devel/fixture/files/old.patch", Before: old, Delete: true}}
			switch kind {
			case "renamed patch":
				edits = append(edits, git.FileEdit{Path: "devel/fixture/files/new.patch", After: []byte("patch bytes"), Mode: 0o100644})
			case "added patch":
				edits = []git.FileEdit{{Path: "devel/fixture/files/new.patch", After: []byte("new bytes"), Mode: 0o100644}}
			case "modified patch":
				edits = []git.FileEdit{{Path: "devel/fixture/files/old.patch", Before: old, After: []byte("updated bytes"), Mode: 0o100644}}
			case "modified Portfile with deleted patch", "outside deletion":
				port, _, err := repo.File(t.Context(), baseTree, "devel/fixture/Portfile")
				require.NoError(t, err)
				edits = append(edits, git.FileEdit{Path: "devel/fixture/Portfile", Before: port, After: []byte("version 2\n"), Mode: 0o100644})
				if kind == "outside deletion" {
					outside, _, err := repo.File(t.Context(), baseTree, "other/port/Portfile")
					require.NoError(t, err)
					edits = append(edits, git.FileEdit{Path: "other/port/Portfile", Before: outside, Delete: true})
				}
			}
			tree, err := repo.EditTree(t.Context(), baseTree, edits)
			require.NoError(t, err)
			commit, err := repo.WriteCommit(t.Context(), git.Commit{Tree: tree, Parents: []string{base}, Message: "fixture change", Author: signature, Committer: signature})
			require.NoError(t, err)
			f.request.Spec.Source = record.Source{Base: record.ObjectID(base), Commit: record.ObjectID(commit), Tree: record.ObjectID(tree)}
			_, err = f.provider.source(t.Context(), f.request)
			if kind == "deleted patch" || kind == "renamed patch" || kind == "outside deletion" {
				require.Error(t, err)
				refused, err := f.provider.Submit(t.Context(), f.request)
				require.NoError(t, err)
				require.Equal(t, verify.Unsupported, refused.State)
				head, err := repo.RemoteHead(t.Context(), f.remote, "candidate")
				require.NoError(t, err)
				require.False(t, head.Exists)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestIndependentJobsShareRunAndCancelSeparately(t *testing.T) {
	t.Parallel()
	f := setup(t)
	f.ready()
	f.api.run.Status, f.api.run.Conclusion = gh.Ptr("in_progress"), nil
	status, err := f.engine.Status(t.Context(), workflow.Scope{Jobs: []record.JobID{f.job}})
	require.NoError(t, err)
	spec := status.Jobs[0].Job.Spec
	spec.FreshVerification = true
	second, err := f.engine.Submit(t.Context(), workflow.Request{ID: "independent-observer", Spec: spec})
	require.NoError(t, err)
	scope := workflow.Scope{Jobs: []record.JobID{f.job, second.JobID}}
	require.Eventually(t, func() bool {
		_, err := f.engine.Cycle(t.Context(), scope)
		require.NoError(t, err)
		status, err := f.engine.Status(t.Context(), scope)
		require.NoError(t, err)
		for _, job := range status.Jobs {
			if len(job.Attempts) != 1 || job.Attempts[0].State != record.AttemptRunning {
				return false
			}
			require.Equal(t, "10:1", job.Attempts[0].Run.RunID)
		}
		return true
	}, 5*time.Second, 10*time.Millisecond)
	require.NoError(t, f.engine.Control(t.Context(), record.ControlRequest{ID: "cancel-second", Kind: record.Cancel, Jobs: []record.JobID{second.JobID}}))
	require.Eventually(t, func() bool {
		_, err := f.engine.Cycle(t.Context(), scope)
		require.NoError(t, err)
		status, err := f.engine.Status(t.Context(), scope)
		require.NoError(t, err)
		canceled := false
		for _, job := range status.Jobs {
			if job.Job.ID == second.JobID {
				canceled = job.Job.State == record.JobCanceled
			} else {
				require.Equal(t, record.JobActive, job.Job.State)
			}
		}
		return canceled
	}, 5*time.Second, 10*time.Millisecond)
	f.api.run.Status, f.api.run.Conclusion = gh.Ptr("completed"), gh.Ptr("success")
	require.Eventually(t, func() bool {
		_, err := f.engine.Cycle(t.Context(), scope)
		require.NoError(t, err)
		status, err := f.engine.Status(t.Context(), workflow.Scope{Jobs: []record.JobID{f.job}})
		require.NoError(t, err)
		return status.Jobs[0].Job.State == record.JobCompleted
	}, 5*time.Second, 10*time.Millisecond)
}

type lostRejectionReply struct{ verify.Provider }

func (p lostRejectionReply) Submit(ctx context.Context, request verify.Request) (verify.Submission, error) {
	result, err := p.Provider.Submit(ctx, request)
	if err == nil && result.State == verify.Unsupported {
		return verify.Submission{}, context.DeadlineExceeded
	}
	return result, err
}

func TestPermanentAdmissionFailureSurvivesLostReply(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"merged", "missing base", "missing local branch", "unrelated base", "authentication", "missing workflow"} {
		t.Run(kind, func(t *testing.T) {
			f := setup(t)
			switch kind {
			case "merged":
				require.NoError(t, f.provider.Repo.Push(t.Context(), git.Push{Remote: f.remote, Branch: "master", Commit: string(f.request.Spec.Source.Commit), ExpectedRemote: git.RefValue{Exists: true, Object: string(f.request.Spec.Source.Base)}}))
			case "missing local branch":
				require.NoError(t, f.provider.Repo.UpdateRefs(t.Context(), []git.RefChange{{Name: "refs/heads/candidate", Expected: git.RefValue{Exists: true, Object: string(f.request.Spec.Source.Commit)}}}))
			case "missing base":
				out, err := exec.CommandContext(t.Context(), "git", "--git-dir", f.remote, "update-ref", "-d", "refs/heads/master").CombinedOutput()
				require.NoError(t, err, string(out))
			case "unrelated base":
				sig := git.Signature{Name: "Fixture", Email: "fixture@example.invalid", When: time.Now()}
				unrelated, err := f.provider.Repo.WriteCommit(t.Context(), git.Commit{Tree: string(f.request.Spec.Source.Tree), Message: "unrelated", Author: sig, Committer: sig})
				require.NoError(t, err)
				require.NoError(t, f.provider.Repo.Push(t.Context(), git.Push{Remote: f.remote, Branch: "master", Commit: unrelated, ExpectedRemote: git.RefValue{Exists: true, Object: string(f.request.Spec.Source.Base)}}))
			case "authentication":
				f.api.err = &gh.ErrorResponse{Response: &http.Response{StatusCode: 401}, Message: "credential rejected"}
			case "missing workflow":
				f.api.err = &gh.ErrorResponse{Response: &http.Response{StatusCode: 404}, Message: "workflow missing"}
			}
			f.engine.Providers[verify.ProviderGitHub] = lostRejectionReply{f.provider}
			require.Eventually(t, func() bool {
				_, err := f.engine.Cycle(t.Context(), workflow.Scope{Jobs: []record.JobID{f.job}})
				require.NoError(t, err)
				status, err := f.engine.Status(t.Context(), workflow.Scope{Jobs: []record.JobID{f.job}})
				require.NoError(t, err)
				return status.Jobs[0].Job.State == record.JobNeedsAttention
			}, 5*time.Second, 10*time.Millisecond)
			restarted := *f.provider
			recovered, err := restarted.Reconcile(t.Context(), f.request.ID, verify.ReconcileOptions{})
			require.NoError(t, err)
			require.Equal(t, verify.Unsupported, recovered.Submission.State)
			require.NotEmpty(t, recovered.Submission.Detail)
			repeat, err := restarted.Submit(t.Context(), f.request)
			require.NoError(t, err)
			require.Equal(t, recovered.Submission, repeat)
			require.NoError(t, f.engine.State.View(t.Context(), f.engine.Repository, func(ctx context.Context, r state.Reader) error {
				submissions, err := r.SubmissionsForAttempt(ctx, f.request.AttemptID)
				require.NoError(t, err)
				require.Len(t, submissions, 1)
				return nil
			}))
			head, err := f.provider.Repo.RemoteHead(t.Context(), f.remote, "candidate")
			require.NoError(t, err)
			require.False(t, head.Exists)
		})
	}
}

func TestTemporaryPreflightFailureCanRecover(t *testing.T) {
	t.Parallel()
	for _, code := range []int{403, 429, 503} {
		t.Run(fmt.Sprint(code), func(t *testing.T) {
			f := setup(t)
			f.api.err = &gh.ErrorResponse{Response: &http.Response{StatusCode: code}, Message: "temporarily unavailable"}
			require.Eventually(t, func() bool {
				_, err := f.engine.Cycle(t.Context(), workflow.Scope{Jobs: []record.JobID{f.job}})
				require.NoError(t, err)
				status, err := f.engine.Status(t.Context(), workflow.Scope{Jobs: []record.JobID{f.job}})
				require.NoError(t, err)
				return status.Jobs[0].Attempts[0].State == record.AttemptUncertain
			}, 5*time.Second, 10*time.Millisecond)
			f.api.err = nil
			f.ready()
			require.Eventually(t, func() bool {
				_, err := f.engine.Cycle(t.Context(), workflow.Scope{Jobs: []record.JobID{f.job}})
				require.NoError(t, err)
				status, err := f.engine.Status(t.Context(), workflow.Scope{Jobs: []record.JobID{f.job}})
				require.NoError(t, err)
				return status.Jobs[0].Job.State == record.JobCompleted
			}, 5*time.Second, 10*time.Millisecond)
		})
	}
}

func TestDriverProgressFollowsGitHubRun(t *testing.T) {
	t.Parallel()
	f := setup(t)
	scope := workflow.Scope{Jobs: []record.JobID{f.job}}
	require.Eventually(t, func() bool {
		_, err := f.engine.Cycle(t.Context(), scope)
		require.NoError(t, err)
		status, err := f.engine.Status(t.Context(), scope)
		require.NoError(t, err)
		return strings.Contains(status.Jobs[0].Job.Detail, "No matching GitHub Actions run observed for contributor/macports-ports:candidate")
	}, 5*time.Second, 10*time.Millisecond)
	f.ready()
	f.api.run.Conclusion = nil
	for _, phase := range []string{"queued", "in_progress"} {
		f.api.run.Status = gh.Ptr(phase)
		require.Eventually(t, func() bool {
			_, err := f.engine.Cycle(t.Context(), scope)
			require.NoError(t, err)
			status, err := f.engine.Status(t.Context(), scope)
			require.NoError(t, err)
			job := status.Jobs[0]
			if !strings.Contains(job.Job.Detail, "GitHub Actions "+phase) {
				return false
			}
			require.Contains(t, job.Job.Detail, "run 10 attempt 1")
			require.Contains(t, job.Job.Detail, f.api.run.GetHTMLURL())
			require.Contains(t, job.Job.Detail, string(f.request.Spec.Source.Commit))
			require.Equal(t, phase, job.Attempts[0].Evidence.Workflow.Status)
			require.Empty(t, job.Attempts[0].LastError)
			return true
		}, 5*time.Second, 10*time.Millisecond)
	}
}
