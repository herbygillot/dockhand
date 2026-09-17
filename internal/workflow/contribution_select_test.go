package workflow_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/state"
	"github.com/herbygillot/dockhand/internal/workflow"
	"github.com/stretchr/testify/require"
)

func TestTargetContinuationDoesNotFallBackToCheckout(t *testing.T) {
	t.Parallel()
	f, input := preparationFixture(t, false)
	id := submitPreparation(t, f, input)
	selected := workflow.ContributionSelector{Target: "fixture"}
	req := bindRequest(f, "continue")
	req.Branch = ""
	req.Continue = &selected
	req.UseRecordedBuild = true
	_, err := f.engine.BindVerification(t.Context(), req)
	require.ErrorContains(t, err, "no prepared update branch")
	candidateJob(t, f, id)
	f.run(t, id)
	prepared := f.status(t, id).Jobs[0].Job
	bound, err := f.engine.BindVerification(t.Context(), req)
	require.NoError(t, err)
	require.Equal(t, prepared.Prepared.Source, bound.Request.Spec.Source)
	require.Equal(t, prepared.ChangeID, bound.Request.Branch.ExpectedChange)
	require.Equal(t, prepared.ResultRevision, bound.Request.Branch.ExpectedRevision)
	receipt, err := f.engine.Submit(t.Context(), bound.Request)
	require.NoError(t, err)
	require.Equal(t, prepared.ChangeID, receipt.ChangeID)

	// An unrelated successful job must not become the selected contribution.
	selected.Target = "untracked"
	_, err = f.engine.BindVerification(t.Context(), req)
	require.ErrorContains(t, err, "no open contribution")
	selected.Target = "fixture"
	require.NoError(t, f.store.Update(t.Context(), f.repository, func(ctx context.Context, tx state.Tx) error {
		return tx.PutChange(ctx, record.Change{ID: "independent", InitiatingTarget: "fixture", Targets: input.Spec.Targets, Disposition: record.ChangeOpen})
	}))
	_, err = f.engine.BindVerification(t.Context(), req)
	require.ErrorContains(t, err, "multiple open contributions")
	selected.ChangeID = prepared.ChangeID
	_, err = f.engine.BindVerification(t.Context(), req)
	require.NoError(t, err)
}

func TestTargetContinuationRejectsDirtyCheckoutAndMissingBranch(t *testing.T) {
	t.Parallel()
	f, input := preparationFixture(t, false)
	id := submitPreparation(t, f, input)
	candidateJob(t, f, id)
	f.run(t, id)
	prepared := f.status(t, id).Jobs[0].Job
	branch := prepared.Prepared.Branch
	out, err := exec.CommandContext(t.Context(), "git", "-C", f.repo.Root, "checkout", "-q", branch).CombinedOutput()
	require.NoError(t, err, "%s", out)
	portfile := filepath.Join(f.repo.Root, "devel/fixture/Portfile")
	require.NoError(t, os.WriteFile(portfile, []byte("version 2\n"), 0600))
	req := bindRequest(f, "verify")
	req.Continue = &workflow.ContributionSelector{Target: "fixture"}
	_, err = f.engine.BindVerification(t.Context(), req)
	require.ErrorContains(t, err, "uncommitted files")
	req.Continue = nil
	req.Branch = ""
	bound, err := f.engine.BindVerification(t.Context(), req)
	require.NoError(t, err)
	require.NotEqual(t, prepared.Prepared.Source.Tree, bound.Request.Spec.Source.Tree)
	out, err = exec.CommandContext(t.Context(), "git", "-C", f.repo.Root, "checkout", "-q", "-f", "--detach").CombinedOutput()
	require.NoError(t, err, "%s", out)
	out, err = exec.CommandContext(t.Context(), "git", "-C", f.repo.Root, "branch", "-D", branch).CombinedOutput()
	require.NoError(t, err, "%s", out)
	req.Continue = &workflow.ContributionSelector{Target: "fixture"}
	_, err = f.engine.BindVerification(t.Context(), req)
	require.Error(t, err)
}

func TestTargetPublicationUsesPreparedRevisionAndRecordedBuild(t *testing.T) {
	t.Parallel()
	f, _, input := combinedFixture(t, record.BumpRevision)
	id := prepareCombined(t, f, input)
	passCombined(t, f, id)
	prepared := f.status(t, id).Jobs[0].Job
	pub, err := f.engine.BindPublication(t.Context(), workflow.PublicationRequest{ID: "publish-by-target", Target: "fixture"})
	require.NoError(t, err)
	require.Equal(t, prepared.ResultRevision, pub.Branch.ExpectedRevision)
	require.Equal(t, prepared.Prepared.Source.Commit, pub.Spec.Source.Commit)
	req := bindRequest(f, "verify-by-target")
	req.Continue = &workflow.ContributionSelector{Target: "fixture"}
	req.UseRecordedBuild = true
	bound, err := f.engine.BindVerification(t.Context(), req)
	require.NoError(t, err)
	require.Equal(t, input.Spec.Build, bound.Request.Spec.Build)
	status, err := f.engine.FilteredStatus(t.Context(), workflow.StatusFilter{Target: "FIXTURE"})
	require.NoError(t, err)
	require.Len(t, status.Changes, 1)
	require.Len(t, status.Jobs, 1)
	require.Equal(t, prepared.ChangeID, status.Changes[0].ID)
}

func TestTargetControlRetainsFrozenJobsBeforeBranchExists(t *testing.T) {
	t.Parallel()
	f, input := preparationFixture(t, false)
	first := submitPreparation(t, f, input)
	selected := workflow.ContributionSelector{Target: "fixture"}
	scope, err := f.engine.ContributionScope(t.Context(), selected)
	require.NoError(t, err)
	require.Equal(t, []record.JobID{first}, scope.Jobs)
	request := record.ControlRequest{ID: "cancel-target", Kind: record.Cancel}
	frozen, err := f.engine.ControlContribution(t.Context(), request, selected)
	require.NoError(t, err)
	require.Equal(t, scope, frozen)
	f.run(t, first)
	input.ID = "retry"
	later := submitPreparation(t, f, input)
	require.NotEqual(t, first, later)
	replay, err := f.engine.ControlContribution(t.Context(), request, selected)
	require.NoError(t, err)
	require.Equal(t, frozen, replay)
	require.Equal(t, record.JobQueued, f.status(t, later).Jobs[0].Job.State)
}
