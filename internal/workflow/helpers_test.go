package workflow_test

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/herbygillot/dockhand/v2/internal/git"
	"github.com/herbygillot/dockhand/v2/internal/ledger"
	"github.com/herbygillot/dockhand/v2/internal/record"
	"github.com/herbygillot/dockhand/v2/internal/verify"
	"github.com/herbygillot/dockhand/v2/internal/workflow"
	"github.com/stretchr/testify/require"
)

var buildPlatform = record.Platform{OS: "darwin", Version: "25", Architecture: "arm64"}

type fixture struct {
	engine   *workflow.Engine
	store    *ledger.Store
	repo     *git.Repository
	provider *scriptedProvider
	clock    atomic.Int64
	source   record.Source
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	root := t.TempDir()
	command := exec.CommandContext(t.Context(), "git", "init", "--quiet", root)
	command.Env = isolatedGitEnv()
	output, err := command.CombinedOutput()
	require.NoError(t, err, "git init (Git is required): %v\n%s", err, output)

	repo, err := git.Open(t.Context(), root, "")
	require.NoError(t, err)
	store, err := ledger.New(repo, ledger.Options{Lockfile: filepath.Join(root, "locks", "ledger.lock"), LockTimeout: time.Second})
	require.NoError(t, err)
	f := &fixture{repo: repo, store: store}
	f.clock.Store(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC).UnixNano())
	blob, err := repo.WriteBlob(t.Context(), []byte("name fixture\nversion 1.0\n"))
	require.NoError(t, err)
	tree, err := repo.WriteTree(t.Context(), []git.TreeEntry{{Name: "Portfile", Mode: 0100644, Type: "blob", Object: blob}})
	require.NoError(t, err)
	signature := git.Signature{Name: "Dockhand test", Email: "test@example.invalid", When: f.now()}
	commit, err := repo.WriteCommit(t.Context(), git.Commit{Tree: tree, Message: "fixture", Author: signature, Committer: signature})
	require.NoError(t, err)
	f.source = record.Source{Tree: record.ObjectID(tree), Commit: record.ObjectID(commit), Base: record.ObjectID(commit)}
	require.NoError(t, store.Update(t.Context(), func(_ context.Context, tx *ledger.Transaction) error {
		tx.State.Changes["change"] = record.Change{ID: "change", CurrentRevision: "revision", Disposition: record.ChangeOpen}
		tx.State.Revisions["revision"] = record.Revision{ID: "revision", ChangeID: "change", Source: f.source, CreatedAt: f.now()}
		return nil
	}))
	f.provider = &scriptedProvider{store: store, now: f.now, calls: make(map[string]int)}
	f.engine = &workflow.Engine{Ledger: store, Provider: f.provider, Now: f.now, Owner: "driver", LeaseDuration: time.Minute, CallTimeout: 5 * time.Second, RetryDelay: time.Second}
	return f
}

func (f *fixture) now() time.Time          { return time.Unix(0, f.clock.Load()).UTC() }
func (f *fixture) advance(d time.Duration) { f.clock.Add(int64(d)) }
func (f *fixture) request(id string) workflow.Request {
	return workflow.Request{ID: record.RequestID(id), Spec: record.JobSpec{Action: record.Verify, InputRevision: "revision", Targets: []record.Target{{Name: "fixture", Portfile: "Portfile", Variants: map[string]bool{"debug": false}}}, Destination: record.VerificationComplete, Verification: record.VerificationRequired, Build: &record.BuildConfig{Provider: "scripted", Platform: buildPlatform, EnvironmentDigest: "sha256:fixture", FromSource: true, Tests: record.TestDeclared}}}
}
func (f *fixture) submit(t *testing.T, id string) record.JobID {
	t.Helper()
	r, err := f.engine.Submit(t.Context(), f.request(id))
	require.NoError(t, err)
	return r.JobID
}
func (f *fixture) run(t *testing.T, ids ...record.JobID) workflow.CycleResult {
	t.Helper()
	f.advance(2 * time.Second)
	scope := workflow.Scope{Jobs: ids}
	if len(ids) == 0 {
		scope.All = true
	}
	result, err := f.engine.Cycle(t.Context(), scope)
	require.NoError(t, err)
	return result
}
func (f *fixture) status(t *testing.T, id record.JobID) workflow.Status {
	t.Helper()
	s, err := f.engine.Status(t.Context(), workflow.Scope{Jobs: []record.JobID{id}})
	require.NoError(t, err)
	return s
}
func (f *fixture) attempt(t *testing.T, id record.JobID) record.Attempt {
	t.Helper()
	s := f.status(t, id)
	require.Len(t, s.Jobs[0].Attempts, 1, "want one attempt, got %+v", s.Jobs[0].Attempts)
	return s.Jobs[0].Attempts[0]
}
func (f *fixture) cancel(t *testing.T, id record.JobID) {
	t.Helper()
	require.NoError(t, f.engine.Control(t.Context(), record.ControlRequest{ID: record.RequestID("cancel-" + string(id)), Kind: record.Cancel, Jobs: []record.JobID{id}}))
}

type scriptedProvider struct {
	store     *ledger.Store
	now       func() time.Time
	mu        sync.Mutex
	calls     map[string]int
	submit    func(context.Context, verify.Request) (verify.Submission, error)
	reconcile func(context.Context, record.RequestID) (verify.Reconciliation, error)
	observe   func(context.Context, record.ProviderRun) (verify.Observation, error)
	cancel    func(context.Context, record.ProviderRun) error
	release   func(context.Context, record.ResourceHandle) (verify.ReleaseResult, error)
}

// Every provider operation checks that the workflow has released the ledger lock.
func (p *scriptedProvider) begin(ctx context.Context, op string) error {
	if _, err := p.store.Read(ctx); err != nil {
		return err
	}
	if err := p.store.Update(ctx, func(context.Context, *ledger.Transaction) error { return nil }); err != nil {
		return fmt.Errorf("provider %s could not access ledger: %w", op, err)
	}
	p.mu.Lock()
	p.calls[op]++
	p.mu.Unlock()
	return nil
}
func (p *scriptedProvider) count(op string) int { p.mu.Lock(); defer p.mu.Unlock(); return p.calls[op] }
func (p *scriptedProvider) Capabilities(ctx context.Context) (verify.Capabilities, error) {
	if err := p.begin(ctx, "capabilities"); err != nil {
		return verify.Capabilities{}, err
	}
	return verify.Capabilities{Name: "scripted", Platforms: []record.Platform{buildPlatform}, Capacity: 1, Isolated: true}, nil
}
func admitted(id record.RequestID) verify.Submission {
	return verify.Submission{State: verify.Admitted, Run: record.ProviderRun{Provider: "scripted", RequestID: id, RunID: "run-" + string(id)}, Resources: []record.ResourceHandle{{Provider: "scripted", ID: "vm-" + string(id)}}}
}
func (p *scriptedProvider) Submit(ctx context.Context, r verify.Request) (verify.Submission, error) {
	if err := p.begin(ctx, "submit"); err != nil {
		return verify.Submission{}, err
	}
	if p.submit != nil {
		return p.submit(ctx, r)
	}
	return admitted(r.ID), nil
}
func (p *scriptedProvider) Reconcile(ctx context.Context, id record.RequestID) (verify.Reconciliation, error) {
	if err := p.begin(ctx, "reconcile"); err != nil {
		return verify.Reconciliation{}, err
	}
	if p.reconcile != nil {
		return p.reconcile(ctx, id)
	}
	return verify.Reconciliation{State: verify.RunUnknown}, nil
}
func (p *scriptedProvider) Observe(ctx context.Context, r record.ProviderRun) (verify.Observation, error) {
	if err := p.begin(ctx, "observe"); err != nil {
		return verify.Observation{}, err
	}
	if p.observe != nil {
		return p.observe(ctx, r)
	}
	return verify.Observation{Run: r, State: record.AttemptRunning, Verdict: record.VerdictUnknown, ObservedAt: p.now()}, nil
}
func (p *scriptedProvider) Cancel(ctx context.Context, r record.ProviderRun) error {
	if err := p.begin(ctx, "cancel"); err != nil {
		return err
	}
	if p.cancel != nil {
		return p.cancel(ctx, r)
	}
	return nil
}
func (p *scriptedProvider) Release(ctx context.Context, h record.ResourceHandle) (verify.ReleaseResult, error) {
	if err := p.begin(ctx, "release"); err != nil {
		return verify.ReleaseResult{}, err
	}
	if p.release != nil {
		return p.release(ctx, h)
	}
	return verify.ReleaseResult{Confirmed: true}, nil
}
func terminal(f *fixture, verdict record.Verdict) func(context.Context, record.ProviderRun) (verify.Observation, error) {
	return func(_ context.Context, r record.ProviderRun) (verify.Observation, error) {
		state := record.AttemptFinished
		if verdict == record.VerdictCanceled {
			state = record.AttemptCanceled
		}
		return verify.Observation{Run: r, State: state, Verdict: verdict, ObservedAt: f.now()}, nil
	}
}

type cycleReply struct {
	result workflow.CycleResult
	err    error
}

func startCycle(ctx context.Context, e *workflow.Engine, id record.JobID) <-chan cycleReply {
	done := make(chan cycleReply, 1)
	go func() { r, err := e.Cycle(ctx, workflow.Scope{Jobs: []record.JobID{id}}); done <- cycleReply{r, err} }()
	return done
}
func receive[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(10 * time.Second):
		require.FailNow(t, "timed out waiting for test barrier")
		var zero T
		return zero
	}
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
