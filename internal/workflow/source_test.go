package workflow_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/eval"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/state"
	"github.com/herbygillot/dockhand/internal/state/sqlite"
	"github.com/herbygillot/dockhand/internal/workflow"
	"github.com/stretchr/testify/require"
)

type boundPorts struct {
	targetName string
	selection  macports.Selection
	seenRoot   string
	seenBytes  string
	err        error
	onEvaluate func()
}

func (p *boundPorts) Resolve(ctx context.Context, tree macports.Tree, sel macports.Selection) ([]record.Target, error) {
	p.seenRoot = tree.Root()
	p.selection = sel
	data, err := os.ReadFile(filepath.Join(tree.Root(), "devel/fixture/Portfile"))
	if err != nil {
		return nil, err
	}
	p.seenBytes = string(data)
	name := p.targetName
	if name == "" {
		name = sel.Subport
		if name == "" {
			name = "fixture"
		}
	}
	return []record.Target{{Name: name, Portfile: "devel/fixture/Portfile", Subport: sel.Subport, Variants: sel.Variants}}, p.err
}
func (p *boundPorts) Evaluate(ctx context.Context, c macports.Context) (macports.Snapshot, error) {
	if p.onEvaluate != nil {
		p.onEvaluate()
	}
	return macports.Snapshot{Source: c.Source(), Target: c.Target(), Platform: c.Platform(), Ports: map[string]macports.PortInfo{c.Target().Name: {Name: c.Target().Name, Version: "1"}}}, p.err
}

func commitPort(t *testing.T, f *fixture, branch, contents string) record.Source {
	t.Helper()
	blob, err := f.repo.WriteBlob(t.Context(), []byte(contents))
	require.NoError(t, err)
	port, err := f.repo.WriteTree(t.Context(), []git.TreeEntry{{Name: "Portfile", Object: blob, Type: "blob", Mode: 0100644}})
	require.NoError(t, err)
	category, err := f.repo.WriteTree(t.Context(), []git.TreeEntry{{Name: "fixture", Object: port, Type: "tree", Mode: 040000}})
	require.NoError(t, err)
	tree, err := f.repo.WriteTree(t.Context(), []git.TreeEntry{{Name: "devel", Object: category, Type: "tree", Mode: 040000}})
	require.NoError(t, err)
	sig := git.Signature{Name: "Fixture", Email: "test@example.invalid", When: time.Now()}
	commit, err := f.repo.WriteCommit(t.Context(), git.Commit{Tree: tree, Message: contents, Author: sig, Committer: sig})
	require.NoError(t, err)
	previous, err := f.repo.ReadRef(t.Context(), "refs/heads/"+branch)
	require.NoError(t, err)
	require.NoError(t, f.repo.UpdateRefs(t.Context(), []git.RefChange{{Name: "refs/heads/" + branch, Expected: previous, Desired: git.RefValue{Exists: true, Object: commit}}}))
	return record.Source{Tree: record.ObjectID(tree), Commit: record.ObjectID(commit)}
}
func bindRequest(f *fixture, id string) workflow.VerificationRequest {
	return workflow.VerificationRequest{ID: record.RequestID(id), Branch: "candidate", Selection: macports.Selection{Selector: "fixture"}, Build: *f.request(id).Spec.Build}
}
func bindingFixture(t *testing.T) (*fixture, *boundPorts) {
	t.Helper()
	f := newFixture(t)
	p := &boundPorts{}
	f.engine.Repo = f.repo
	f.engine.Ports = p
	commitPort(t, f, "candidate", "version 1\n")
	return f, p
}

func TestBindVerificationFreezesCommittedInputAndCreatesNoState(t *testing.T) {
	f, ports := bindingFixture(t)
	require.NoError(t, os.MkdirAll(filepath.Join(f.repo.Root, "devel/fixture"), 0700))
	require.NoError(t, os.WriteFile(filepath.Join(f.repo.Root, "devel/fixture/Portfile"), []byte("dirty"), 0600))
	// A database write during evaluation proves it runs outside a transaction.
	ports.onEvaluate = func() {
		require.NoError(t, f.store.Update(t.Context(), f.repository, func(context.Context, state.Tx) error { return nil }))
	}
	bound, err := f.engine.BindVerification(t.Context(), bindRequest(f, "bound"))
	require.NoError(t, err)
	require.Equal(t, "version 1\n", ports.seenBytes)
	require.NoDirExists(t, ports.seenRoot)
	status, err := f.engine.Status(t.Context(), workflow.Scope{All: true})
	require.NoError(t, err)
	require.Empty(t, status.Jobs)
	require.NoError(t, f.store.View(t.Context(), f.repository, func(ctx context.Context, r state.Reader) error {
		_, err := r.OpenChangeByBranch(ctx, "candidate")
		require.ErrorIs(t, err, state.ErrNotFound)
		return nil
	}))
	original := bound.Request.Spec.Source
	commitPort(t, f, "candidate", "version 2\n")
	receipt, err := f.engine.Submit(t.Context(), bound.Request)
	require.NoError(t, err)
	job := f.status(t, receipt.JobID).Jobs[0].Job
	require.Equal(t, original, job.Spec.Source)
	require.Empty(t, job.Spec.InputRevision)
	require.Empty(t, job.ChangeID)
	f.run(t, receipt.JobID)
	require.Equal(t, original, f.attempt(t, receipt.JobID).Spec.Source)
	retry, err := f.engine.Submit(t.Context(), bound.Request)
	require.NoError(t, err)
	require.Equal(t, receipt, retry)
}

func TestCorrectedBranchAddsRevisionAndSameSourceReusesIt(t *testing.T) {
	f, _ := bindingFixture(t)
	trackCandidate(t, f)
	first, err := f.engine.BindVerification(t.Context(), bindRequest(f, "first"))
	require.NoError(t, err)
	receipt, err := f.engine.Submit(t.Context(), first.Request)
	require.NoError(t, err)
	original := f.status(t, receipt.JobID).Jobs[0].Job
	second, err := f.engine.BindVerification(t.Context(), bindRequest(f, "same"))
	require.NoError(t, err)
	receipt, err = f.engine.Submit(t.Context(), second.Request)
	require.NoError(t, err)
	same := f.status(t, receipt.JobID).Jobs[0].Job
	require.Equal(t, original.Spec.InputRevision, same.Spec.InputRevision)
	changed := commitPort(t, f, "candidate", "version 2\n")
	changed.Base = original.Spec.Source.Base
	third, err := f.engine.BindVerification(t.Context(), bindRequest(f, "corrected"))
	require.NoError(t, err)
	receipt, err = f.engine.Submit(t.Context(), third.Request)
	require.NoError(t, err)
	corrected := f.status(t, receipt.JobID).Jobs[0].Job
	require.Equal(t, original.ChangeID, corrected.ChangeID)
	require.Equal(t, changed, corrected.Spec.Source)
	require.NotEqual(t, original.Spec.InputRevision, corrected.Spec.InputRevision)
	require.NoError(t, f.store.View(t.Context(), f.repository, func(ctx context.Context, r state.Reader) error {
		old, err := r.Revision(ctx, original.Spec.InputRevision)
		require.NoError(t, err)
		require.Equal(t, original.Spec.Source, old.Source)
		current, err := r.Revision(ctx, corrected.Spec.InputRevision)
		require.NoError(t, err)
		require.Equal(t, old.ID, current.Previous)
		return nil
	}))
	retry, err := f.engine.Submit(t.Context(), first.Request)
	require.NoError(t, err)
	require.Equal(t, original.ID, retry.JobID)
}

func TestCompetingBranchBindingsHaveOneAtomicWinner(t *testing.T) {
	f, _ := bindingFixture(t)
	trackCandidate(t, f)
	one, err := f.engine.BindVerification(t.Context(), bindRequest(f, "one"))
	require.NoError(t, err)
	two, err := f.engine.BindVerification(t.Context(), bindRequest(f, "two"))
	require.NoError(t, err)
	var wg sync.WaitGroup
	var successes, conflicts atomic.Int64
	for _, bound := range []workflow.BoundVerification{one, two} {
		wg.Go(func() {
			_, err := f.engine.Submit(t.Context(), bound.Request)
			if err == nil {
				successes.Add(1)
			} else if errors.Is(err, workflow.ErrStaleRevision) {
				conflicts.Add(1)
			} else {
				t.Errorf("unexpected submit error: %v", err)
			}
		})
	}
	wg.Wait()
	require.EqualValues(t, 1, successes.Load())
	require.EqualValues(t, 1, conflicts.Load())
	status, err := f.engine.Status(t.Context(), workflow.Scope{All: true})
	require.NoError(t, err)
	require.Len(t, status.Jobs, 1)
}

func TestBranchBindingFailureLeavesNoJobAndCleansSnapshot(t *testing.T) {
	f, ports := bindingFixture(t)
	ports.err = errors.New("evaluation failed")
	_, err := f.engine.BindVerification(t.Context(), bindRequest(f, "failed"))
	require.ErrorContains(t, err, "evaluation failed")
	require.NoDirExists(t, ports.seenRoot)
	request := bindRequest(f, "missing")
	request.Branch = "renamed"
	_, err = f.engine.BindVerification(t.Context(), request)
	require.ErrorIs(t, err, git.ErrBranchMissing)
	status, err := f.engine.Status(t.Context(), workflow.Scope{All: true})
	require.NoError(t, err)
	require.Empty(t, status.Jobs)
	other, err := f.store.RegisterRepository(t.Context(), t.TempDir())
	require.NoError(t, err)
	f.engine.Repository = other.ID
	_, err = f.engine.BindVerification(t.Context(), bindRequest(f, "wrong-repo"))
	require.ErrorIs(t, err, workflow.ErrInvalidRequest)
}

func TestVerificationBindingAgainstPortsTree(t *testing.T) {
	directory := os.Getenv("DOCKHAND_TEST_PORTS_REPO")
	branch := os.Getenv("DOCKHAND_TEST_PORTS_BRANCH")
	if directory == "" || branch == "" {
		t.Skip("set DOCKHAND_TEST_PORTS_REPO and DOCKHAND_TEST_PORTS_BRANCH for a real ports-tree check")
	}
	repo, err := git.Open(t.Context(), directory, "")
	require.NoError(t, err)
	store, err := sqlite.Open(t.Context(), filepath.Join(t.TempDir(), "state.db"), sqlite.Options{})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	registered, err := store.RegisterRepository(t.Context(), repo.CommonDir)
	require.NoError(t, err)
	ports := &eval.Evaluator{}
	platform, err := ports.NativePlatform(t.Context())
	require.NoError(t, err)
	engine := workflow.Engine{State: store, Repository: registered.ID, Repo: repo, Ports: ports}
	for _, selector := range []string{"jq", "terraform"} {
		request := workflow.VerificationRequest{ID: record.RequestID("live-" + selector), Branch: branch, Selection: macports.Selection{Selector: selector}, Build: record.BuildConfig{Provider: "fixture", Platform: platform, EnvironmentDigest: "fixture:binding-only", FromSource: true, Tests: record.TestDeclared}}
		bound, err := engine.BindVerification(t.Context(), request)
		require.NoError(t, err)
		require.Equal(t, platform, bound.Evaluation.Platform)
		require.NotEmpty(t, bound.Evaluation.Ports)
		for _, info := range bound.Evaluation.Ports {
			t.Logf("%s: version=%s revision=%d dependencies=%d optional-errors=%d", info.Name, info.Version, info.Revision, len(info.Dependencies), len(info.OptionErrors))
		}
		t.Logf("branch=%s commit=%s tree=%s platform=%+v", branch, bound.Request.Spec.Source.Commit, bound.Request.Spec.Source.Tree, platform)
		receipt, err := engine.Submit(t.Context(), bound.Request)
		require.NoError(t, err)
		status, err := engine.Status(t.Context(), workflow.Scope{Jobs: []record.JobID{receipt.JobID}})
		require.NoError(t, err)
		require.Empty(t, status.Jobs[0].Job.ChangeID)
	}
}

func TestBranchAdoptionRollsBackWhenRequestWriteFails(t *testing.T) {
	f, _ := bindingFixture(t)
	trackCandidate(t, f)
	bound, err := f.engine.BindVerification(t.Context(), bindRequest(f, "collision"))
	require.NoError(t, err)
	other, err := f.store.RegisterRepository(t.Context(), t.TempDir())
	require.NoError(t, err)
	require.NoError(t, f.store.Update(t.Context(), other.ID, func(ctx context.Context, tx state.Tx) error {
		return tx.PutRequest(ctx, record.AcceptedRequest{ID: bound.Request.ID, Kind: record.JobRequest, Payload: []byte("{}"), AcceptedAt: time.Now()})
	}))
	_, err = f.engine.Submit(t.Context(), bound.Request)
	require.ErrorIs(t, err, state.ErrConflict)
	require.NoError(t, f.store.View(t.Context(), f.repository, func(ctx context.Context, r state.Reader) error {
		change, err := r.OpenChangeByBranch(ctx, "candidate")
		require.NoError(t, err)
		require.Equal(t, record.RevisionID("revision"), change.CurrentRevision)
		revisions, err := r.Revisions(ctx, state.Query{})
		require.NoError(t, err)
		require.Len(t, revisions, 1)
		jobs, err := r.Jobs(ctx, state.Query{})
		require.NoError(t, err)
		require.Empty(t, jobs)
		return nil
	}))
}

func trackCandidate(t *testing.T, f *fixture) {
	t.Helper()
	require.NoError(t, f.store.Update(t.Context(), f.repository, func(ctx context.Context, tx state.Tx) error {
		change, err := tx.Change(ctx, "change")
		if err != nil {
			return err
		}
		change.Branch = "candidate"
		return tx.PutChange(ctx, change)
	}))
}

func TestVerificationTargetsShareSourceWithoutClaimingContributionBranch(t *testing.T) {
	f, p := bindingFixture(t)
	first, err := f.engine.BindVerification(t.Context(), bindRequest(f, "first-port"))
	require.NoError(t, err)
	p.targetName = "other-port"
	second, err := f.engine.BindVerification(t.Context(), bindRequest(f, "second-port"))
	require.NoError(t, err)
	var jobs []record.JobID
	for _, bound := range []workflow.BoundVerification{first, second} {
		receipt, err := f.engine.Submit(t.Context(), bound.Request)
		require.NoError(t, err)
		jobs = append(jobs, receipt.JobID)
	}
	f.run(t, jobs...)
	for _, id := range jobs {
		status := f.status(t, id)
		require.Empty(t, status.Jobs[0].Job.ChangeID)
		require.Empty(t, status.Changes)
		attempt := f.attempt(t, id)
		require.Empty(t, attempt.Spec.RevisionID)
		require.Equal(t, first.Request.Spec.Source, attempt.Spec.Source)
	}
	require.NotEqual(t, f.attempt(t, jobs[0]).Spec.Target.Name, f.attempt(t, jobs[1]).Spec.Target.Name)
}

func TestTrackedVerificationPreservesEditedTargetsWhenTestingOtherPorts(t *testing.T) {
	f, p := bindingFixture(t)
	trackCandidate(t, f)
	edited := []record.Target{{Name: "edited-port", Portfile: "devel/edited/Portfile"}}
	require.NoError(t, f.store.Update(t.Context(), f.repository, func(ctx context.Context, tx state.Tx) error {
		change, err := tx.Change(ctx, "change")
		if err != nil {
			return err
		}
		change.Targets = edited
		return tx.PutChange(ctx, change)
	}))
	p.targetName = "downstream-port"
	bound, err := f.engine.BindVerification(t.Context(), bindRequest(f, "downstream"))
	require.NoError(t, err)
	receipt, err := f.engine.Submit(t.Context(), bound.Request)
	require.NoError(t, err)
	f.run(t, receipt.JobID)
	status := f.status(t, receipt.JobID)
	require.Equal(t, edited, status.Changes[0].Targets)
	require.Equal(t, "downstream-port", f.attempt(t, receipt.JobID).Spec.Target.Name)
	require.NotEmpty(t, status.Jobs[0].Job.Spec.InputRevision)
}
