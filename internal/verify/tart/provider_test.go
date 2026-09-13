package tart

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/herbygillot/dockhand/v2/internal/git"
	"github.com/herbygillot/dockhand/v2/internal/record"
	"github.com/herbygillot/dockhand/v2/internal/state"
	"github.com/herbygillot/dockhand/v2/internal/state/sqlite"
	"github.com/herbygillot/dockhand/v2/internal/verify"
	"github.com/herbygillot/dockhand/v2/internal/workflow"
	"github.com/stretchr/testify/require"
)

var testPlatform = record.Platform{OS: "darwin", Version: "25", Architecture: "arm64"}

type fakeMachine struct {
	mu                                 sync.Mutex
	calls                              map[string]int
	running                            map[string]bool
	results                            map[string]guestResult
	stageError, launchError, stopError error
	stageHook                          func()
}

func newMachine() *fakeMachine {
	return &fakeMachine{calls: map[string]int{}, running: map[string]bool{}, results: map[string]guestResult{}}
}
func (m *fakeMachine) Environment(context.Context) (Environment, error) {
	return Environment{Digest: "sha256:fixture", Platform: testPlatform}, nil
}
func (m *fakeMachine) Running(context.Context) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var names []string
	for vm, yes := range m.running {
		if yes {
			names = append(names, vm)
		}
	}
	return names, nil
}
func (m *fakeMachine) Clone(ctx context.Context, image, vm string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls["clone"]++
	return nil
}
func (m *fakeMachine) Start(ctx context.Context, vm, directory string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls["start"]++
	m.running[vm] = true
	return nil
}
func (m *fakeMachine) Ready(context.Context, string) error { return nil }
func (m *fakeMachine) Stage(ctx context.Context, vm, path string) error {
	if m.stageHook != nil {
		m.stageHook()
	}
	return m.stageError
}
func (m *fakeMachine) Launch(ctx context.Context, vm string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls["launch"]++
	return m.launchError
}
func (m *fakeMachine) Inspect(ctx context.Context, vm string) (guestResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.results[vm], nil
}
func (m *fakeMachine) Logs(ctx context.Context, vm, path string) error {
	return os.WriteFile(path, []byte("build log\n"), 0600)
}
func (m *fakeMachine) Stop(ctx context.Context, vm string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls["stop"]++
	if m.stopError != nil {
		return m.stopError
	}
	m.running[vm] = false
	return nil
}
func (m *fakeMachine) Delete(ctx context.Context, vm string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls["delete"]++
	return nil
}

type testRun struct {
	provider *Provider
	store    *sqlite.Store
	request  verify.Request
}

func fixtureRun(t *testing.T, db, home, artifacts, id string, m machine) *testRun {
	t.Helper()
	directory := t.TempDir()
	out, err := exec.CommandContext(t.Context(), "git", "init", "--quiet", directory).CombinedOutput()
	require.NoError(t, err, "%s", out)
	repo, err := git.Open(t.Context(), directory, "")
	require.NoError(t, err)
	blob, err := repo.WriteBlob(t.Context(), []byte("PortSystem 1.0\nname fixture\nversion 1\n"))
	require.NoError(t, err)
	tree, err := repo.WriteTree(t.Context(), []git.TreeEntry{{Name: "Portfile", Mode: 0100644, Type: "blob", Object: blob}})
	require.NoError(t, err)
	for _, name := range []string{"fixture", "devel"} {
		tree, err = repo.WriteTree(t.Context(), []git.TreeEntry{{Name: name, Mode: 040000, Type: "tree", Object: tree}})
		require.NoError(t, err)
	}
	sig := git.Signature{Name: "Fixture", Email: "test@example.invalid", When: time.Now()}
	commit, err := repo.WriteCommit(t.Context(), git.Commit{Tree: tree, Message: "fixture", Author: sig, Committer: sig})
	require.NoError(t, err)
	store, err := sqlite.Open(t.Context(), db, sqlite.Options{})
	require.NoError(t, err)
	t.Cleanup(func() { store.Close() })
	repository, err := store.RegisterRepository(t.Context(), repo.CommonDir)
	require.NoError(t, err)
	source := record.Source{Commit: record.ObjectID(commit), Tree: record.ObjectID(tree)}
	require.NoError(t, store.Update(t.Context(), repository.ID, func(ctx context.Context, tx state.Tx) error {
		if err := tx.PutChange(ctx, record.Change{ID: record.ChangeID(id), Branch: id, CurrentRevision: record.RevisionID(id), Disposition: record.ChangeOpen}); err != nil {
			return err
		}
		return tx.PutRevision(ctx, record.Revision{ID: record.RevisionID(id), ChangeID: record.ChangeID(id), Source: source, CreatedAt: time.Now()})
	}))
	p := &Provider{State: store, Repository: repository.ID, Repo: repo, Config: Config{Home: home, Image: "fixture", ArtifactDirectory: artifacts, Capacity: 1, Platform: testPlatform}, backend: m}
	e := &workflow.Engine{State: store, Repository: repository.ID, Provider: p, RetryDelay: time.Millisecond}
	config := record.BuildConfig{Provider: "tart", Platform: testPlatform, EnvironmentDigest: "sha256:fixture", FromSource: true, Tests: record.TestSkip}
	receipt, err := e.Submit(t.Context(), workflow.Request{ID: record.RequestID(id), Spec: record.JobSpec{Action: record.Verify, InputRevision: record.RevisionID(id), Targets: []record.Target{{Name: "fixture", Portfile: "devel/fixture/Portfile"}}, Destination: record.VerificationComplete, Verification: record.VerificationRequired, Build: &config}})
	require.NoError(t, err)
	// Create the attempt through the engine, refusing admission while assembling the fixture.
	e.Provider = capacityProvider{}
	_, err = e.Cycle(t.Context(), workflow.Scope{Jobs: []record.JobID{receipt.JobID}})
	require.NoError(t, err)
	status, err := e.Status(t.Context(), workflow.Scope{Jobs: []record.JobID{receipt.JobID}})
	require.NoError(t, err)
	a := status.Jobs[0].Attempts[0]
	e.Provider = p
	return &testRun{p, store, verify.Request{ID: a.SubmissionID, AttemptID: a.ID, Spec: a.Spec}}
}

type capacityProvider struct{ verify.Provider }

func (capacityProvider) Capabilities(context.Context) (verify.Capabilities, error) {
	return verify.Capabilities{Name: "tart", Platforms: []record.Platform{testPlatform}, Isolated: true, Capacity: 1}, nil
}

func (capacityProvider) Submit(context.Context, verify.Request) (verify.Submission, error) {
	return verify.Submission{State: verify.AtCapacity}, nil
}
func singleRun(t *testing.T) (*testRun, *fakeMachine) {
	t.Helper()
	root := t.TempDir()
	m := newMachine()
	return fixtureRun(t, filepath.Join(root, "state.db"), root, filepath.Join(root, "artifacts"), "one", m), m
}

func TestAdmissionIsIdempotentAndClosesUnknownIDs(t *testing.T) {
	f, m := singleRun(t)
	result, err := f.provider.Submit(t.Context(), f.request)
	require.NoError(t, err)
	require.Equal(t, verify.Admitted, result.State)
	again, err := f.provider.Submit(t.Context(), f.request)
	require.NoError(t, err)
	require.Equal(t, result, again)
	require.Equal(t, 1, m.calls["clone"])
	changed := f.request
	changed.Spec.Config.Tests = record.TestDeclared
	_, err = f.provider.Submit(t.Context(), changed)
	require.ErrorIs(t, err, state.ErrConflict)
	closed, err := f.provider.Reconcile(t.Context(), "late")
	require.NoError(t, err)
	require.Equal(t, verify.RequestClosed, closed.State)
	late := f.request
	late.ID = "late"
	_, err = f.provider.Submit(t.Context(), late)
	require.ErrorIs(t, err, ErrClosed)
	require.Equal(t, 1, m.calls["clone"])
}
func TestCapacityCountsReservationsAcrossRepositoriesAndExternalVMs(t *testing.T) {
	root := t.TempDir()
	m := newMachine()
	a := fixtureRun(t, filepath.Join(root, "state.db"), root, filepath.Join(root, "artifacts"), "a", m)
	b := fixtureRun(t, filepath.Join(root, "state.db"), root, filepath.Join(root, "artifacts"), "b", m)
	m.running["personal-vm"] = true
	result, err := a.provider.Submit(t.Context(), a.request)
	require.NoError(t, err)
	require.Equal(t, verify.AtCapacity, result.State)
	require.Empty(t, m.calls)
	delete(m.running, "personal-vm")
	m.stageError = errors.New("interrupted staging")
	_, err = a.provider.Submit(t.Context(), a.request)
	require.Error(t, err)
	// No running VM is visible, but the unclosed reservation still owns capacity.
	clear(m.running)
	result, err = b.provider.Submit(t.Context(), b.request)
	require.NoError(t, err)
	require.Equal(t, verify.AtCapacity, result.State)
	reconciliation, err := a.provider.Reconcile(t.Context(), a.request.ID)
	require.NoError(t, err)
	require.Equal(t, verify.RequestClosed, reconciliation.State)
	require.Len(t, reconciliation.Submission.Resources, 1)
	_, err = a.provider.Submit(t.Context(), a.request)
	require.ErrorIs(t, err, ErrClosed)
	m.stageError = nil
	result, err = b.provider.Submit(t.Context(), b.request)
	require.NoError(t, err)
	require.Equal(t, verify.Admitted, result.State)
}
func TestRecoveryCompletesAdmittedLaunchAndPreservesResultsAcrossStopFailure(t *testing.T) {
	f, m := singleRun(t)
	m.launchError = errors.New("lost launch reply")
	_, err := f.provider.Submit(t.Context(), f.request)
	require.Error(t, err)
	recovered := &Provider{State: f.store, Repository: f.provider.Repository, Repo: f.provider.Repo, Config: f.provider.Config, backend: m}
	reconciliation, err := recovered.Reconcile(t.Context(), f.request.ID)
	require.NoError(t, err)
	require.Equal(t, verify.RunFound, reconciliation.State)
	run := reconciliation.Submission.Run
	m.results[run.RunID] = guestResult{State: "not-started"}
	m.launchError = nil
	observed, err := recovered.Observe(t.Context(), run)
	require.NoError(t, err)
	require.Equal(t, record.AttemptRunning, observed.State)
	m.results[run.RunID] = guestResult{Protocol: 1, ID: string(run.RequestID), Digest: buildDigest(f.request.Spec), State: "finished", Verdict: record.VerdictPassed, Steps: []record.StepResult{{Package: "fixture", Phase: "install", Verdict: record.VerdictPassed}}}
	m.stopError = errors.New("lost shutdown reply")
	_, err = recovered.Observe(t.Context(), run)
	require.Error(t, err)
	m.stopError = nil
	m.results[run.RunID] = guestResult{State: "stopped"}
	observed, err = recovered.Observe(t.Context(), run)
	require.NoError(t, err)
	require.Equal(t, record.VerdictPassed, observed.Verdict)
	require.FileExists(t, observed.Logs[0].Location)
	released, err := recovered.Release(t.Context(), reconciliation.Submission.Resources[0])
	require.NoError(t, err)
	require.True(t, released.Confirmed)
	again, err := recovered.Observe(t.Context(), run)
	require.NoError(t, err)
	require.Equal(t, observed, again)
	require.FileExists(t, observed.Logs[0].Location)
	released, err = recovered.Release(t.Context(), reconciliation.Submission.Resources[0])
	require.NoError(t, err)
	require.True(t, released.Confirmed)
	require.Equal(t, 1, m.calls["delete"])
}
func TestCancellationDoesNotFreeCapacityUntilStopped(t *testing.T) {
	f, m := singleRun(t)
	result, err := f.provider.Submit(t.Context(), f.request)
	require.NoError(t, err)
	m.stopError = errors.New("not stopped")
	require.Error(t, f.provider.Cancel(t.Context(), result.Run))
	o, err := f.provider.begin(t.Context(), f.request.ID)
	require.NoError(t, err)
	v, err := o.read(t.Context(), f.request.ID)
	o.close()
	require.NoError(t, err)
	require.True(t, v.Occupied)
	m.stopError = nil
	require.NoError(t, f.provider.Cancel(t.Context(), result.Run))
	observed, err := f.provider.Observe(t.Context(), result.Run)
	require.NoError(t, err)
	require.Equal(t, record.VerdictCanceled, observed.Verdict)
}
func TestProviderCallsDoNotHoldDatabaseTransactions(t *testing.T) {
	f, m := singleRun(t)
	m.stageHook = func() {
		require.NoError(t, f.store.Update(t.Context(), f.provider.Repository, func(context.Context, state.Tx) error { return nil }))
	}
	_, err := f.provider.Submit(t.Context(), f.request)
	require.NoError(t, err)
}
func TestResultIdentityMustMatchAcceptedInputs(t *testing.T) {
	f, m := singleRun(t)
	result, err := f.provider.Submit(t.Context(), f.request)
	require.NoError(t, err)
	m.results[result.Run.RunID] = guestResult{Protocol: 1, ID: "different", State: "finished", Verdict: record.VerdictPassed}
	_, err = f.provider.Observe(t.Context(), result.Run)
	require.ErrorContains(t, err, "different inputs")
	_, err = f.provider.Release(t.Context(), result.Resources[0])
	require.ErrorContains(t, err, "no confirmed terminal")
}
func TestExecutionLockIsHeldBySurvivingChild(t *testing.T) {
	path := filepath.Join(t.TempDir(), "operation.lock")
	file, err := acquire(t.Context(), path)
	require.NoError(t, err)
	cmd := exec.Command("/bin/sh", "-c", "read line || true")
	cmd.ExtraFiles = []*os.File{file}
	stdin, err := cmd.StdinPipe()
	require.NoError(t, err)
	require.NoError(t, cmd.Start())
	require.NoError(t, file.Close())
	t.Cleanup(func() { stdin.Close(); cmd.Wait() })
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	_, err = acquire(ctx, path)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.NoError(t, stdin.Close())
	require.NoError(t, cmd.Wait())
	next, err := acquire(t.Context(), path)
	require.NoError(t, err)
	next.Close()
}

func TestConcurrentAdmissionHonorsPoolCapacity(t *testing.T) {
	root := t.TempDir()
	machine := newMachine()
	a := fixtureRun(t, filepath.Join(root, "state.db"), root, filepath.Join(root, "artifacts"), "a", machine)
	b := fixtureRun(t, filepath.Join(root, "state.db"), root, filepath.Join(root, "artifacts"), "b", machine)
	type outcome struct {
		result verify.Submission
		err    error
	}
	results := make(chan outcome, 2)
	start := make(chan struct{})
	for _, f := range []*testRun{a, b} {
		go func() {
			<-start
			result, err := f.provider.Submit(t.Context(), f.request)
			results <- outcome{result, err}
		}()
	}
	close(start)
	states := []verify.SubmissionState{}
	for range 2 {
		result := <-results
		require.NoError(t, result.err)
		states = append(states, result.result.State)
	}
	require.ElementsMatch(t, []verify.SubmissionState{verify.Admitted, verify.AtCapacity}, states)
	require.Equal(t, 1, machine.calls["clone"])
}

func TestCancellationPreservesAnAlreadyFinishedGuest(t *testing.T) {
	f, m := singleRun(t)
	submitted, err := f.provider.Submit(t.Context(), f.request)
	require.NoError(t, err)
	m.results[submitted.Run.RunID] = guestResult{Protocol: 1, ID: string(f.request.ID), Digest: buildDigest(f.request.Spec), State: "finished", Verdict: record.VerdictPassed, Steps: []record.StepResult{{Package: "fixture", Phase: "install", Verdict: record.VerdictPassed}}}
	require.NoError(t, f.provider.Cancel(t.Context(), submitted.Run))
	observed, err := f.provider.Observe(t.Context(), submitted.Run)
	require.NoError(t, err)
	require.Equal(t, record.VerdictPassed, observed.Verdict)
	require.NotEmpty(t, observed.Logs)
	require.Equal(t, 1, m.calls["stop"])
}

func TestFrozenProviderChoicesResumeWithoutImageOrCapacityFlags(t *testing.T) {
	f, m := singleRun(t)
	config, err := f.provider.BuildConfig(t.Context(), testPlatform, record.TestSkip, true)
	require.NoError(t, err)
	f.request.Spec.Config = config
	p := &Provider{State: f.store, Repository: f.provider.Repository, Repo: f.provider.Repo, Config: Config{Home: f.provider.Config.Home, ArtifactDirectory: f.provider.Config.ArtifactDirectory}, backend: m}
	admitted, err := p.Submit(t.Context(), f.request)
	require.NoError(t, err)
	require.Equal(t, verify.Admitted, admitted.State)
	again, err := p.Reconcile(t.Context(), f.request.ID)
	require.NoError(t, err)
	require.Equal(t, verify.RunFound, again.State)
	require.NoError(t, p.Cancel(t.Context(), admitted.Run))
	observed, err := p.Observe(t.Context(), admitted.Run)
	require.NoError(t, err)
	require.Equal(t, record.VerdictCanceled, observed.Verdict)
	var frozen Config
	require.NoError(t, json.Unmarshal(config.ProviderConfig, &frozen))
	require.Equal(t, "fixture", frozen.Image)
	require.Equal(t, 1, frozen.Capacity)
	chunk, err := p.ReadLog(t.Context(), admitted.Run, 0, 65536)
	require.NoError(t, err)
	require.Equal(t, "build log\n", string(chunk.Data))
	require.True(t, chunk.Complete)
	_, err = p.Release(t.Context(), admitted.Resources[0])
	require.NoError(t, err)
	chunk, err = p.ReadLog(t.Context(), admitted.Run, 6, 65536)
	require.NoError(t, err)
	require.Equal(t, "log\n", string(chunk.Data))
}

func TestStandaloneVerificationAdmissionRequiresNoContributionRevision(t *testing.T) {
	f, m := singleRun(t)
	engine := workflow.Engine{State: f.store, Repository: f.provider.Repository, Provider: capacityProvider{}}
	receipt, err := engine.Submit(t.Context(), workflow.Request{ID: "standalone", Spec: record.JobSpec{
		Action: record.Verify, Source: f.request.Spec.Source, Targets: []record.Target{f.request.Spec.Target},
		Destination: record.VerificationComplete, Verification: record.VerificationRequired, Build: &f.request.Spec.Config,
	}})
	require.NoError(t, err)
	_, err = engine.Cycle(t.Context(), workflow.Scope{Jobs: []record.JobID{receipt.JobID}})
	require.NoError(t, err)
	status, err := engine.Status(t.Context(), workflow.Scope{Jobs: []record.JobID{receipt.JobID}})
	require.NoError(t, err)
	require.Empty(t, status.Changes)
	attempt := status.Jobs[0].Attempts[0]
	require.Empty(t, attempt.Spec.RevisionID)
	request := verify.Request{ID: attempt.SubmissionID, AttemptID: attempt.ID, Spec: attempt.Spec}
	result, err := f.provider.Submit(t.Context(), request)
	require.NoError(t, err)
	require.Equal(t, verify.Admitted, result.State)
	require.Equal(t, 1, m.calls["clone"])
	recovered, err := f.provider.Reconcile(t.Context(), request.ID)
	require.NoError(t, err)
	require.Equal(t, verify.RunFound, recovered.State)
	require.Equal(t, result.Run, recovered.Submission.Run)
}
