package workflow_test

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/state"
	"github.com/herbygillot/dockhand/internal/workflow"
	"github.com/herbygillot/dockhand/internal/workflow/preparation"
	"github.com/stretchr/testify/require"
)

func TestStoppedPreparationRetiresAndFreshSourceStartsAnew(t *testing.T) {
	f, request := preparationFixture(t, false)
	f.engine.Preparer = prepareFunc(func(context.Context, preparation.Request) (preparation.Result, error) {
		return preparation.Result{}, errors.New("missing helper")
	})
	id := submitPreparation(t, f, request)
	selected := workflow.ContributionSelector{Target: "fixture"}
	_, err := f.engine.AbandonContribution(t.Context(), selected)
	require.ErrorContains(t, err, "pending job")
	f.run(t, id)
	status := f.status(t, id)
	original := status.Jobs[0].Job
	require.Equal(t, record.JobNeedsAttention, original.State)
	require.Equal(t, record.ChangeClosed, status.Changes[0].Disposition, "a preparation that stopped before a branch retires on its own")
	require.Empty(t, status.Changes[0].Branch)
	_, err = f.engine.AbandonContribution(t.Context(), selected)
	require.ErrorIs(t, err, state.ErrNotFound, "nothing is open for the target any more")
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
		f, request := preparationFixture(t, true)
		request.Spec.Build = nil
		id := submitPreparation(t, f, request)
		candidateJob(t, f, id)
		f.run(t, id)
		f.run(t, id)
		stopped := f.status(t, id).Jobs[0].Job
		require.Equal(t, record.JobNeedsAttention, stopped.State)
		require.NotNil(t, stopped.Prepared, "a stopped preparation with a branch stays open for abandon or retry")
		changeID := stopped.ChangeID
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
		require.NotEqual(t, abandonErr == nil, submitErr == nil, "abandon %v; submit %v", abandonErr, submitErr)
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
			if remoteState == record.PullRequestMerged {
				require.Contains(t, result.Detail, "local branch candidate deleted")
				require.Contains(t, result.Detail, "fork branch author/ports:candidate deleted")
				local, err := f.repo.ReadRef(t.Context(), "refs/heads/candidate")
				require.NoError(t, err)
				require.False(t, local.Exists, "a merged contribution's local branch is residue")
				remote, err := f.repo.RemoteHead(t.Context(), hosting.remote, "candidate")
				require.NoError(t, err)
				require.False(t, remote.Exists, "the fork's head branch is residue too")
			} else {
				head, _, err := f.repo.Branch(t.Context(), "candidate")
				require.NoError(t, err)
				require.Equal(t, string(f.source.Commit), head, "a closed PR keeps its branch for a resumed contribution")
			}
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

func TestMergeCleanupKeepsCheckedOutBranchesAndGcSweepsLeftovers(t *testing.T) {
	f, hosting, selected := publishedLifecycleFixture(t)
	worktree := filepath.Join(t.TempDir(), "checkout")
	add := exec.CommandContext(t.Context(), "git", "worktree", "add", "-q", worktree, "candidate")
	add.Dir = f.repo.Root
	out, err := add.CombinedOutput()
	require.NoError(t, err, "%s", out)
	hosting.observation.PullRequest.State = record.PullRequestMerged
	result, err := f.engine.RefreshContribution(t.Context(), selected)
	require.NoError(t, err)
	require.Equal(t, record.ChangeMerged, result.Change.Disposition, result.Detail)
	require.Contains(t, result.Detail, "local branch candidate kept; it is checked out at")
	require.Contains(t, result.Detail, "fork branch author/ports:candidate deleted")
	// gc reports the checked-out branch and deletes it once the checkout is gone.
	collected, err := f.engine.Collect(t.Context(), workflow.RetentionOptions{})
	require.NoError(t, err)
	branches := branchItems(collected)
	require.Len(t, branches, 1)
	require.Equal(t, "delete-branch", branches[0].Action)
	require.False(t, branches[0].Completed)
	require.Contains(t, branches[0].Detail, "checked out at")
	remove := exec.CommandContext(t.Context(), "git", "worktree", "remove", "--force", worktree)
	remove.Dir = f.repo.Root
	out, err = remove.CombinedOutput()
	require.NoError(t, err, "%s", out)
	collected, err = f.engine.Collect(t.Context(), workflow.RetentionOptions{DryRun: true})
	require.NoError(t, err)
	branches = branchItems(collected)
	require.Len(t, branches, 1)
	require.Contains(t, branches[0].Detail, "would delete")
	collected, err = f.engine.Collect(t.Context(), workflow.RetentionOptions{})
	require.NoError(t, err)
	branches = branchItems(collected)
	require.Len(t, branches, 1)
	require.True(t, branches[0].Completed)
	local, err := f.repo.ReadRef(t.Context(), "refs/heads/candidate")
	require.NoError(t, err)
	require.False(t, local.Exists)
	// A leftover branch that moved past the published commit is kept.
	require.NoError(t, f.repo.UpdateRefs(t.Context(), []git.RefChange{{Name: "refs/heads/candidate", Desired: git.RefValue{Exists: true, Object: string(f.source.Base)}}}))
	collected, err = f.engine.Collect(t.Context(), workflow.RetentionOptions{})
	require.NoError(t, err)
	branches = branchItems(collected)
	require.Len(t, branches, 1)
	require.False(t, branches[0].Completed)
	require.Contains(t, branches[0].Detail, "no longer holds the published commit")
}

func branchItems(result workflow.RetentionResult) []workflow.CleanupItem {
	var items []workflow.CleanupItem
	for _, item := range result.Items {
		if item.Action == "delete-branch" {
			items = append(items, item)
		}
	}
	return items
}

func TestCycleObservesOpenPullRequestsOnASchedule(t *testing.T) {
	f, hosting, _ := publishedLifecycleFixture(t)
	hosting.status = record.PullRequestStatus{Mergeable: "yes", Review: "none", Checks: record.CheckSummary{Total: 3, Passed: 1, Pending: 2}}
	f.engine.PullRequestInterval = time.Hour
	_, err := f.engine.Cycle(t.Context(), workflow.Scope{All: true})
	require.NoError(t, err)
	require.Equal(t, 1, hosting.inspections, "a whole-repository cycle looks at the open PR")
	var stored record.PullRequest
	require.NoError(t, f.store.View(t.Context(), f.repository, func(ctx context.Context, r state.Reader) error {
		change, err := r.Change(ctx, "change")
		if err != nil {
			return err
		}
		stored, err = r.PullRequest(ctx, change.PullRequestID)
		return err
	}))
	require.NotNil(t, stored.Status)
	require.Equal(t, 2, stored.Status.Checks.Pending)
	_, err = f.engine.Cycle(t.Context(), workflow.Scope{All: true})
	require.NoError(t, err)
	require.Equal(t, 1, hosting.inspections, "within the interval the PR is left alone")
	hosting.inspectErr = errors.New("offline")
	_, err = f.engine.Cycle(t.Context(), workflow.Scope{All: true})
	require.NoError(t, err, "a failed look never fails the cycle")
	hosting.observation.PullRequest.State = record.PullRequestMerged
	f.advance(2 * time.Hour)
	_, err = f.engine.Cycle(t.Context(), workflow.Scope{All: true})
	require.NoError(t, err)
	var change record.Change
	require.NoError(t, f.store.View(t.Context(), f.repository, func(ctx context.Context, r state.Reader) error {
		var err error
		change, err = r.Change(ctx, "change")
		return err
	}))
	require.Equal(t, record.ChangeMerged, change.Disposition, "a merged PR retires the contribution from the cycle")
	local, err := f.repo.ReadRef(t.Context(), "refs/heads/candidate")
	require.NoError(t, err)
	require.False(t, local.Exists, "and cleans its branch as refresh would")
}
