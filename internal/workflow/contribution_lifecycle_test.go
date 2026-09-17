package workflow_test

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/state"
	"github.com/herbygillot/dockhand/internal/workflow"
	"github.com/herbygillot/dockhand/internal/workflow/preparation"
	"github.com/stretchr/testify/require"
)

func TestAbandonPreparationAllowsFreshSourceAndPreservesHistory(t *testing.T) {
	f, request := preparationFixture(t, false)
	f.engine.Preparer = prepareFunc(func(context.Context, preparation.Request) (preparation.Result, error) {
		return preparation.Result{}, errors.New("missing helper")
	})
	id := submitPreparation(t, f, request)
	selected := workflow.ContributionSelector{Target: "fixture"}
	_, err := f.engine.AbandonContribution(t.Context(), selected)
	require.ErrorContains(t, err, "pending job")
	f.run(t, id)
	original := f.status(t, id).Jobs[0].Job
	abandoned, err := f.engine.AbandonContribution(t.Context(), selected)
	require.NoError(t, err)
	require.Equal(t, original.ChangeID, abandoned.Change.ID)
	require.Equal(t, record.ChangeAbandoned, abandoned.Change.Disposition)
	require.Empty(t, abandoned.Change.Branch)
	replay, err := f.engine.AbandonContribution(t.Context(), workflow.ContributionSelector{ChangeID: abandoned.Change.ID})
	require.NoError(t, err)
	require.Equal(t, abandoned, replay)
	previous, err := f.engine.PreparationInput(t.Context(), selected, request.Spec.Action)
	require.NoError(t, err)
	require.Nil(t, previous)
	request.ID = "new-release"
	request.Spec.Source = commitPort(t, f, "candidate", "version 3\n")
	request.Spec.Source.Base = request.Spec.Source.Commit
	receipt, err := f.engine.Submit(t.Context(), request)
	require.NoError(t, err)
	require.NotEqual(t, original.ChangeID, receipt.ChangeID)
	require.Equal(t, request.Spec.Source, f.status(t, receipt.JobID).Jobs[0].Job.Spec.Source)
	require.Equal(t, original, f.status(t, id).Jobs[0].Job)
}

func TestAbandonAndRetryAreSerialized(t *testing.T) {
	for range 4 {
		f, request := preparationFixture(t, false)
		f.engine.Preparer = prepareFunc(func(context.Context, preparation.Request) (preparation.Result, error) {
			return preparation.Result{}, errors.New("missing helper")
		})
		id := submitPreparation(t, f, request)
		f.run(t, id)
		changeID := f.status(t, id).Jobs[0].Job.ChangeID
		request.ID, request.Spec.ChangeID = "retry", changeID
		var abandonErr, submitErr error
		var receipt workflow.Receipt
		var group sync.WaitGroup
		group.Add(2)
		go func() {
			defer group.Done()
			_, abandonErr = f.engine.AbandonContribution(t.Context(), workflow.ContributionSelector{ChangeID: changeID})
		}()
		go func() { defer group.Done(); receipt, submitErr = f.engine.Submit(t.Context(), request) }()
		group.Wait()
		require.NotEqual(t, abandonErr == nil, submitErr == nil)
		if submitErr == nil {
			require.Equal(t, changeID, receipt.ChangeID)
		}
	}
}

func publishedLifecycleFixture(t *testing.T) (*fixture, *publicationForge, workflow.ContributionSelector) {
	t.Helper()
	f, hosting := publicationFixture(t)
	id := submitPublication(t, f, "publication")
	for range 3 {
		f.run(t, id)
	}
	require.Equal(t, record.JobCompleted, f.status(t, id).Jobs[0].Job.State)
	return f, hosting, workflow.ContributionSelector{ChangeID: "change"}
}

func TestRefreshRetiresMatchingPRAndNeverReopensLocalContribution(t *testing.T) {
	for _, remoteState := range []record.PullRequestState{record.PullRequestClosed, record.PullRequestMerged} {
		t.Run(string(remoteState), func(t *testing.T) {
			f, hosting, selected := publishedLifecycleFixture(t)
			hosting.observation.PullRequest.State = remoteState
			result, err := f.engine.RefreshContribution(t.Context(), selected)
			require.NoError(t, err)
			expected := record.ChangeClosed
			if remoteState == record.PullRequestMerged {
				expected = record.ChangeMerged
			}
			require.Equal(t, expected, result.Change.Disposition)
			require.Equal(t, remoteState, result.PullRequest.State)
			require.Contains(t, result.Detail, "later bump")
			head, _, err := f.repo.Branch(t.Context(), "candidate")
			require.NoError(t, err)
			require.Equal(t, string(f.source.Commit), head)
			require.NoError(t, f.store.Update(t.Context(), f.repository, func(ctx context.Context, tx state.Tx) error {
				return tx.PutChange(ctx, record.Change{ID: "new-update", Branch: "candidate", InitiatingTarget: "fixture", Disposition: record.ChangeOpen})
			}))
			hosting.observation.PullRequest.State = record.PullRequestOpen
			result, err = f.engine.RefreshContribution(t.Context(), selected)
			require.NoError(t, err)
			require.Equal(t, expected, result.Change.Disposition)
			require.Contains(t, result.Detail, "not reopened")
			require.Equal(t, 1, hosting.writes)
			current, err := f.engine.SelectContribution(t.Context(), workflow.ContributionSelector{Target: "fixture"})
			require.NoError(t, err)
			require.Equal(t, record.ChangeID("new-update"), current.ID)
		})
	}
}

func TestRefreshPreservesCorrectionsAndPendingWork(t *testing.T) {
	for _, mode := range []string{"recorded revision", "moved branch", "dirty checkout", "pending job", "moved PR", "deleted local branch"} {
		t.Run(mode, func(t *testing.T) {
			f, hosting, selected := publishedLifecycleFixture(t)
			hosting.observation.PullRequest.State = record.PullRequestMerged
			switch mode {
			case "recorded revision":
				require.NoError(t, f.store.Update(t.Context(), f.repository, func(ctx context.Context, tx state.Tx) error {
					change, err := tx.Change(ctx, "change")
					if err != nil {
						return err
					}
					if err := tx.PutRevision(ctx, record.Revision{ID: "correction", ChangeID: "change", Previous: change.CurrentRevision, Source: f.source, CreatedAt: f.now()}); err != nil {
						return err
					}
					change.CurrentRevision = "correction"
					return tx.PutChange(ctx, change)
				}))
			case "moved branch":
				commitPort(t, f, "candidate", "version 3\n")
			case "dirty checkout":
				out, err := exec.CommandContext(t.Context(), "git", "-C", f.repo.Root, "checkout", "-q", "candidate").CombinedOutput()
				require.NoError(t, err, "%s", out)
				require.NoError(t, os.WriteFile(filepath.Join(f.repo.Root, "devel/fixture/Portfile"), []byte("version 3\n"), 0600))
			case "pending job":
				bound, err := f.engine.BindVerification(t.Context(), bindRequest(f, "pending"))
				require.NoError(t, err)
				_, err = f.engine.Submit(t.Context(), bound.Request)
				require.NoError(t, err)
			case "moved PR":
				source := commitPort(t, f, "other", "version 3\n")
				require.NoError(t, f.repo.Push(t.Context(), git.Push{Remote: hosting.remote, Branch: "candidate", Commit: string(source.Commit), ExpectedRemote: git.RefValue{Exists: true, Object: string(f.source.Commit)}}))
			case "deleted local branch":
				head, err := f.repo.ReadRef(t.Context(), "refs/heads/candidate")
				require.NoError(t, err)
				require.NoError(t, f.repo.UpdateRefs(t.Context(), []git.RefChange{{Name: "refs/heads/candidate", Expected: head}}))
			}
			result, err := f.engine.RefreshContribution(t.Context(), selected)
			require.NoError(t, err)
			require.Equal(t, record.PullRequestMerged, result.PullRequest.State)
			if mode == "deleted local branch" {
				require.Equal(t, record.ChangeMerged, result.Change.Disposition)
			} else {
				require.Equal(t, record.ChangeOpen, result.Change.Disposition)
				require.Contains(t, result.Detail, "remains open")
			}
		})
	}
}

func TestRefreshFencesConcurrentDispositionChange(t *testing.T) {
	f, hosting, selected := publishedLifecycleFixture(t)
	hosting.observation.PullRequest.State = record.PullRequestMerged
	hosting.onFind = func() { _, err := f.engine.AbandonContribution(t.Context(), selected); require.NoError(t, err) }
	_, err := f.engine.RefreshContribution(t.Context(), selected)
	require.ErrorIs(t, err, workflow.ErrStaleRevision)
	hosting.onFind = nil
	result, err := f.engine.RefreshContribution(t.Context(), selected)
	require.NoError(t, err)
	require.Equal(t, record.ChangeAbandoned, result.Change.Disposition)
	require.Contains(t, result.Detail, "not reopened")
}

func TestRefreshRecordsPRStatusWithoutActingOnIt(t *testing.T) {
	f, hosting, selected := publishedLifecycleFixture(t)
	hosting.status = record.PullRequestStatus{Mergeable: "no", MergeableDetail: "dirty", Review: "changes-requested", ChangesRequested: 1, Checks: record.CheckSummary{Total: 2, Passed: 1, Failed: 1, Failing: []string{"Build ports (macos-15)"}}}
	result, err := f.engine.RefreshContribution(t.Context(), selected)
	require.NoError(t, err)
	require.Equal(t, record.ChangeOpen, result.Change.Disposition)
	require.Equal(t, 1, hosting.inspections)
	require.NotNil(t, result.PullRequest.Status)
	require.Equal(t, "changes-requested", result.PullRequest.Status.Review)
	require.Contains(t, result.Detail, "PR is open; mergeable: no (dirty); review: changes-requested; checks: 1 passed, 1 failed, 0 pending of 2 (failing: Build ports (macos-15))")
	require.Equal(t, 1, hosting.writes, "observation never writes to the forge")
	var stored record.PullRequest
	require.NoError(t, f.store.View(t.Context(), f.repository, func(ctx context.Context, r state.Reader) error {
		var err error
		stored, err = r.PullRequest(ctx, result.PullRequest.ID)
		return err
	}))
	require.Equal(t, result.PullRequest.Status.Checks, stored.Status.Checks, "the status persists with the PR")
	hosting.inspectErr = errors.New("api unavailable")
	result, err = f.engine.RefreshContribution(t.Context(), selected)
	require.NoError(t, err, "a failed inspection does not fail the refresh")
	require.Contains(t, result.Detail, "status unavailable: api unavailable")
	require.NotNil(t, result.PullRequest.Status, "the previous status is kept")
	hosting.inspectErr = nil
	hosting.observation.PullRequest.State = record.PullRequestMerged
	result, err = f.engine.RefreshContribution(t.Context(), selected)
	require.NoError(t, err)
	require.Equal(t, 2, hosting.inspections, "a merged PR is not inspected")
	require.NotContains(t, result.Detail, "mergeable")
}
