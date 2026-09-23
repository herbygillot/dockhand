package workflow_test

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/publish"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/state"
	"github.com/herbygillot/dockhand/internal/state/sqlite"
	"github.com/herbygillot/dockhand/internal/workflow"
	"github.com/stretchr/testify/require"
)

func requireUntrackedPublication(t *testing.T, f *fixture) {
	t.Helper()
	require.NoError(t, f.store.View(t.Context(), f.repository, func(ctx context.Context, r state.Reader) error {
		_, err := r.OpenChangeByBranch(ctx, "candidate")
		require.ErrorIs(t, err, state.ErrNotFound)
		return nil
	}))
}

func TestManualPublicationAdoptsAtomicallyAndResumesAfterLostPRResponse(t *testing.T) {
	t.Parallel()
	f, hosting := manualPublicationFixture(t)
	requireUntrackedPublication(t, f)
	request := bindPublication(t, f, "manual")
	require.Empty(t, request.Branch.ExpectedChange)
	require.Equal(t, f.source.Base, request.Spec.Source.Base)
	requireUntrackedPublication(t, f)
	receipt, err := f.engine.Submit(t.Context(), request)
	require.NoError(t, err)
	status := f.status(t, receipt.JobID)
	require.Len(t, status.Changes, 1)
	require.Len(t, status.Jobs[0].Publications, 1)
	require.Empty(t, status.Jobs[0].Attempts)
	change := status.Changes[0]
	require.Equal(t, "candidate", change.Branch)
	require.Equal(t, request.Spec.Targets, change.Targets)
	require.Equal(t, change.CurrentRevision, status.Jobs[0].Job.Spec.InputRevision)
	retry, err := f.engine.Submit(t.Context(), request)
	require.NoError(t, err)
	require.Equal(t, receipt, retry)
	f.run(t, receipt.JobID)
	hosting.writeErr = errors.New("lost PR response")
	f.run(t, receipt.JobID)
	require.Equal(t, 1, hosting.writes)
	reopened, err := sqlite.Open(t.Context(), f.store.Path(), sqlite.Options{})
	require.NoError(t, err)
	defer reopened.Close()
	other := *f.engine
	other.State, other.Owner = state.Bind(reopened, f.registration), "resumed"
	f.engine = &other
	f.run(t, receipt.JobID)
	finished := f.status(t, receipt.JobID)
	require.Equal(t, record.JobCompleted, finished.Jobs[0].Job.State)
	require.Equal(t, change.ID, finished.Changes[0].ID)
	require.Equal(t, record.PublicationConfirmed, finished.Jobs[0].Publications[0].State)
	require.Len(t, finished.PullRequests, 1)
	require.Equal(t, 1, hosting.writes)
	retry, err = f.engine.Submit(t.Context(), request)
	require.NoError(t, err)
	require.Equal(t, receipt, retry, "retry after adoption must preserve the original accepted request")
}

func TestManualPublicationRechecksEvidenceAndRollsBackAdoption(t *testing.T) {
	t.Parallel()
	f, hosting := manualPublicationFixture(t)
	request := bindPublication(t, f, "planned")
	fresh := reuseRequest(f, "failed-recheck")
	fresh.Spec.Targets = request.Spec.Targets
	fresh.Spec.FreshVerification = true
	f.engine.Provider = f.provider
	completeVerification(t, f, fresh, record.VerdictFailed)
	f.engine.Provider = nil
	receipt, err := f.engine.Submit(t.Context(), request)
	require.ErrorIs(t, err, publish.ErrPrecondition)
	require.Empty(t, receipt)
	requireUntrackedPublication(t, f)
	require.NoError(t, f.store.View(t.Context(), f.repository, func(ctx context.Context, r state.Reader) error {
		_, err := r.Request(ctx, request.ID)
		require.ErrorIs(t, err, state.ErrNotFound)
		changes, err := r.Changes(ctx, state.Query{Limit: 32})
		require.NoError(t, err)
		require.Len(t, changes, 1, "only the unrelated fixture change remains")
		revisions, err := r.Revisions(ctx, state.Query{Limit: 32})
		require.NoError(t, err)
		require.Len(t, revisions, 1)
		return nil
	}))
	_, err = f.engine.BindPublication(t.Context(), workflow.PublicationRequest{ID: "new-plan", Branch: "candidate", Adopt: true})
	require.ErrorIs(t, err, publish.ErrPrecondition)
	require.Zero(t, hosting.writes)
}

func TestManualPublicationCompetingAdmissionsCreateOneContribution(t *testing.T) {
	t.Parallel()
	f, _ := manualPublicationFixture(t)
	requests := []workflow.Request{bindPublication(t, f, "one"), bindPublication(t, f, "two")}
	reopened, err := sqlite.Open(t.Context(), f.store.Path(), sqlite.Options{})
	require.NoError(t, err)
	defer reopened.Close()
	other := *f.engine
	other.State = state.Bind(reopened, f.registration)
	engines := []*workflow.Engine{f.engine, &other}
	var receipts [2]workflow.Receipt
	var errs [2]error
	var wg sync.WaitGroup
	for i := range requests {
		wg.Go(func() { receipts[i], errs[i] = engines[i].Submit(t.Context(), requests[i]) })
	}
	wg.Wait()
	winner := 0
	if errs[0] != nil {
		winner = 1
	}
	require.NoError(t, errs[winner])
	require.ErrorIs(t, errs[1-winner], workflow.ErrStaleRevision)
	retry, err := f.engine.Submit(t.Context(), requests[winner])
	require.NoError(t, err)
	require.Equal(t, receipts[winner], retry)
	status, err := f.engine.Status(t.Context(), workflow.Scope{All: true})
	require.NoError(t, err)
	require.Len(t, status.Jobs, 2, "one verification and one publication")
	require.Len(t, status.Changes, 2, "one adopted contribution and the unrelated fixture change")
	otherRepo, err := f.store.RegisterRepository(t.Context(), filepath.Join(t.TempDir(), ".git"))
	require.NoError(t, err)
	require.NoError(t, f.store.View(t.Context(), otherRepo.ID, func(ctx context.Context, r state.Reader) error {
		_, err := r.OpenChangeByBranch(ctx, "candidate")
		require.ErrorIs(t, err, state.ErrNotFound)
		return nil
	}))
}

func TestManualPublicationRejectsMovedBranchesBeforeRemoteEffects(t *testing.T) {
	t.Parallel()
	for _, stage := range []string{"binding", "before-acceptance", "after-acceptance", "deleted"} {
		t.Run(stage, func(t *testing.T) {
			f, hosting := manualPublicationFixture(t)
			move := func() { commitPort(t, f, "candidate", "version 3\n") }
			if stage == "binding" {
				hosting.onFind = move
				_, err := f.engine.BindPublication(t.Context(), workflow.PublicationRequest{ID: "moving", Branch: "candidate", Adopt: true})
				require.ErrorIs(t, err, workflow.ErrStaleRevision)
				requireUntrackedPublication(t, f)
			} else {
				request := bindPublication(t, f, "manual")
				if stage == "deleted" {
					require.NoError(t, f.repo.UpdateRefs(t.Context(), []git.RefChange{{Name: "refs/heads/candidate", Expected: git.RefValue{Exists: true, Object: string(f.source.Commit)}}}))
				} else if stage == "before-acceptance" {
					move()
				}
				receipt, err := f.engine.Submit(t.Context(), request)
				require.NoError(t, err, "acceptance freezes the bound source; the driver rechecks Git before publishing")
				if stage == "after-acceptance" {
					move()
				}
				f.run(t, receipt.JobID)
				job := f.status(t, receipt.JobID).Jobs[0].Job
				if stage == "deleted" {
					require.Equal(t, record.JobNeedsAttention, job.State)
				} else {
					require.Equal(t, record.JobSuperseded, job.State)
				}
			}
			head, err := f.repo.RemoteHead(t.Context(), hosting.remote, "candidate")
			require.NoError(t, err)
			require.False(t, head.Exists)
			require.Zero(t, hosting.writes)
		})
	}
}

func TestManualPublicationRequiresEvidenceForTheWholeTreeAndPort(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"new-tree", "other-port", "foreign-repository", "two-commits", "merged"} {
		t.Run(mode, func(t *testing.T) {
			f, hosting := manualPublicationFixture(t)
			switch mode {
			case "new-tree":
				tree := commitPort(t, f, "scratch", "version 3\n").Tree
				sig := git.Signature{Name: "Fixture", Email: "fixture@example.invalid", When: f.now()}
				commit, err := f.repo.WriteCommit(t.Context(), git.Commit{Tree: string(tree), Parents: []string{string(f.source.Base)}, Message: "fixture: update", Author: sig, Committer: sig})
				require.NoError(t, err)
				require.NoError(t, f.repo.UpdateRefs(t.Context(), []git.RefChange{{Name: "refs/heads/candidate", Expected: git.RefValue{Exists: true, Object: string(f.source.Commit)}, Desired: git.RefValue{Exists: true, Object: commit}}}))
			case "other-port":
				request := reuseRequest(f, "other-port")
				request.Spec.Targets = []record.Target{{Name: "other", Portfile: "devel/other/Portfile"}}
				request.Spec.FreshVerification = true
				f.engine.Provider = f.provider
				completeVerification(t, f, request, record.VerdictFailed)
				f.engine.Provider = nil
				// The other port's newer failure must not hide this port's passing evidence.
				_, err := f.engine.BindPublication(t.Context(), workflow.PublicationRequest{ID: "good-plan", Branch: "candidate", Adopt: true})
				require.NoError(t, err)
				return
			case "foreign-repository":
				other, err := f.store.RegisterRepository(t.Context(), filepath.Join(t.TempDir(), ".git"))
				require.NoError(t, err)
				f.engine.State = state.Bind(f.store, other)
			case "two-commits":
				tree := commitPort(t, f, "scratch", "version 3\n").Tree
				sig := git.Signature{Name: "Fixture", Email: "fixture@example.invalid", When: f.now()}
				commit, err := f.repo.WriteCommit(t.Context(), git.Commit{Tree: string(tree), Parents: []string{string(f.source.Commit)}, Message: "fixture: another change", Author: sig, Committer: sig})
				require.NoError(t, err)
				require.NoError(t, f.repo.UpdateRefs(t.Context(), []git.RefChange{{Name: "refs/heads/candidate", Expected: git.RefValue{Exists: true, Object: string(f.source.Commit)}, Desired: git.RefValue{Exists: true, Object: commit}}}))
				f.source.Commit, f.source.Tree = record.ObjectID(commit), tree
				request := reuseRequest(f, "verified-two-commits")
				request.Spec.Targets = []record.Target{{Name: "fixture", Portfile: "devel/fixture/Portfile"}}
				f.engine.Provider = f.provider
				completeVerification(t, f, request, record.VerdictPassed)
				f.engine.Provider = nil
			case "merged":
				require.NoError(t, f.repo.Push(t.Context(), git.Push{Remote: hosting.remote, Branch: "main", Commit: string(f.source.Commit), ExpectedRemote: git.RefValue{Exists: true, Object: string(f.source.Base)}}))
			}
			_, err := f.engine.BindPublication(t.Context(), workflow.PublicationRequest{ID: "invalid", Branch: "candidate", Adopt: true})
			require.Error(t, err)
			if mode == "two-commits" {
				require.ErrorContains(t, err, "base is not in")
			}
			require.Zero(t, hosting.writes)
			requireUntrackedPublication(t, f)
		})
	}
}

func TestManualPublicationPreservesVerifiedSubportAndVariants(t *testing.T) {
	t.Parallel()
	f, _ := manualPublicationFixture(t)
	request := reuseRequest(f, "subport")
	request.Spec.Targets = []record.Target{{Name: "fixture-cli", Portfile: "devel/fixture/Portfile", Subport: "fixture-cli", Variants: map[string]bool{"ssl": true}}}
	request.Spec.FreshVerification = true
	f.engine.Provider = f.provider
	attempt := completeVerification(t, f, request, record.VerdictPassed)
	f.engine.Provider = nil
	plan := bindPublication(t, f, "publish-subport")
	require.Equal(t, attempt.Spec.Target, plan.Spec.Targets[0])
	require.Equal(t, attempt.Spec.Config, *plan.Spec.Build)
	require.Equal(t, attempt.ID, plan.Spec.Publication.EvidenceAttempt)
	receipt, err := f.engine.Submit(t.Context(), plan)
	require.NoError(t, err)
	require.Equal(t, plan.Spec.Targets, f.status(t, receipt.JobID).Changes[0].Targets)
}

func TestManualPublicationUsesWorkingTreeEvidenceAfterCommit(t *testing.T) {
	t.Parallel()
	f, _ := manualPublicationFixture(t)
	request := reuseRequest(f, "working-tree")
	request.Spec.Source.Commit, request.Spec.Source.Base = "", ""
	request.Spec.Targets = []record.Target{{Name: "fixture", Portfile: "devel/fixture/Portfile"}}
	request.Spec.FreshVerification = true
	f.engine.Provider = f.provider
	attempt := completeVerification(t, f, request, record.VerdictPassed)
	f.engine.Provider = nil
	sig := git.Signature{Name: "Fixture", Email: "fixture@example.invalid", When: f.now()}
	commit, err := f.repo.WriteCommit(t.Context(), git.Commit{Tree: string(f.source.Tree), Parents: []string{string(f.source.Base)}, Message: "fixture: correct build\n\nHuman commit message", Author: sig, Committer: sig})
	require.NoError(t, err)
	require.NoError(t, f.repo.UpdateRefs(t.Context(), []git.RefChange{{Name: "refs/heads/candidate", Expected: git.RefValue{Exists: true, Object: string(f.source.Commit)}, Desired: git.RefValue{Exists: true, Object: commit}}}))
	plan := bindPublication(t, f, "publish-working-tree")
	require.Equal(t, attempt.ID, plan.Spec.Publication.EvidenceAttempt)
	require.Equal(t, record.ObjectID(commit), plan.Spec.Publication.Desired.Head)
	require.Equal(t, "fixture: correct build", plan.Spec.Publication.Desired.Title)
	receipt, err := f.engine.Submit(t.Context(), plan)
	require.NoError(t, err)
	for range 3 {
		f.run(t, receipt.JobID)
	}
	require.Equal(t, record.JobCompleted, f.status(t, receipt.JobID).Jobs[0].Job.State)
}
