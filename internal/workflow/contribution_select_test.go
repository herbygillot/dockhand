package workflow_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/state"
	"github.com/herbygillot/dockhand/internal/workflow"
	"github.com/herbygillot/dockhand/internal/workflow/view"
	"github.com/stretchr/testify/require"
)

func TestTargetContinuationDoesNotFallBackToCheckout(t *testing.T) {
	t.Parallel()
	f, input := preparationFixture(t, false)
	id := submitPreparation(t, f, input)
	selected := workflow.ContributionSelector{Target: "fixture"}
	req := bindRequest(f, "continue")
	req.Branch = ""
	req.Continue = continuing(t, f, selected)
	req.UseRecordedBuild = true
	_, err := f.engine.BindVerification(t.Context(), req)
	require.ErrorContains(t, err, "no prepared update branch")
	candidateJob(t, f, id)
	f.run(t, id)
	prepared := f.status(t, id).Jobs[0].Job
	req.Continue = continuing(t, f, selected)
	bound, err := f.engine.BindVerification(t.Context(), req)
	require.NoError(t, err)
	require.Equal(t, prepared.Prepared.Source, bound.Request.Spec.Source)
	require.Equal(t, prepared.ChangeID, bound.Request.Branch.ExpectedChange)
	require.Equal(t, prepared.ResultRevision, bound.Request.Branch.ExpectedRevision)
	receipt, err := f.engine.Submit(t.Context(), bound.Request)
	require.NoError(t, err)
	require.Equal(t, prepared.ChangeID, receipt.ChangeID)

	// An unrelated successful job must not become the selected contribution.
	_, err = f.engine.Resolve(t.Context(), workflow.ResolutionRequest{Action: record.Verify, Selection: macports.Selection{Selector: "untracked"}})
	require.ErrorContains(t, err, "no open contribution")
	require.NoError(t, f.store.Update(t.Context(), f.repository, func(ctx context.Context, tx state.Tx) error {
		return tx.PutChange(ctx, record.Change{ID: "independent", InitiatingTarget: "fixture", Targets: input.Spec.Targets, Disposition: record.ChangeOpen})
	}))
	_, err = f.engine.Resolve(t.Context(), workflow.ResolutionRequest{Action: record.Verify, Selection: macports.Selection{Selector: "fixture"}})
	require.ErrorContains(t, err, "multiple open contributions")
	selected.ChangeID = prepared.ChangeID
	req.Continue = continuing(t, f, selected)
	_, err = f.engine.BindVerification(t.Context(), req)
	require.NoError(t, err)
}

// continuing resolves the tracked contribution a verification continues.
func continuing(t *testing.T, f *fixture, selected workflow.ContributionSelector) *workflow.Resolution {
	t.Helper()
	resolution, err := f.engine.Resolve(t.Context(), workflow.ResolutionRequest{Action: record.Verify, Selection: macports.Selection{Selector: selected.Target}, Branch: selected.Branch, ChangeID: selected.ChangeID})
	require.NoError(t, err)
	require.Equal(t, workflow.Continue, resolution.Kind)
	return &resolution
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
	req.Continue = continuing(t, f, workflow.ContributionSelector{Target: "fixture"})
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
	req.Continue = continuing(t, f, workflow.ContributionSelector{Target: "fixture"})
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
	req.Continue = continuing(t, f, workflow.ContributionSelector{Target: "fixture"})
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

// A shared release is initiated by its stub and prepared as one subport of it.
// Status names that subport when it says what to run next, so the same name
// has to reach the contribution the stub initiated -- and a name the
// contribution does not carry still must not.
func TestSubportOfSharedReleaseSelectsItsContribution(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	change := record.Change{
		ID:               "shared",
		InitiatingTarget: "rb-mustache",
		Branch:           "dockhand/bump/rb-mustache",
		Targets:          []record.Target{{Name: "rb33-mustache", Portfile: "ruby/rb-mustache/Portfile", Subport: "rb33-mustache"}},
		Disposition:      record.ChangeOpen,
	}
	require.NoError(t, f.store.Update(t.Context(), f.repository, func(ctx context.Context, tx state.Tx) error {
		return tx.PutChange(ctx, change)
	}))
	for _, name := range []string{"rb-mustache", "rb33-mustache", "RB33-Mustache"} {
		selected, err := f.engine.SelectContribution(t.Context(), workflow.ContributionSelector{Target: name})
		require.NoError(t, err, name)
		require.Equal(t, change.ID, selected.ID, name)
	}
	_, err := f.engine.SelectContribution(t.Context(), workflow.ContributionSelector{Target: "rb24-mustache"})
	require.ErrorIs(t, err, state.ErrNotFound)
}

// The status table tells a reader what to run next by naming a contribution,
// and the resolver behind that verb has to accept the name it printed. They
// are composed in different packages from different fields, so the agreement
// is asserted rather than assumed: every hint must resolve back to the
// contribution it was composed from.
func TestStatusHintsResolveBackToTheirContribution(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	for _, shape := range []struct {
		label  string
		change record.Change
	}{
		{"plain port", record.Change{
			ID: "plain", InitiatingTarget: "bashunit", Branch: "dockhand/bump/bashunit",
			Targets: []record.Target{{Name: "bashunit", Portfile: "devel/bashunit/Portfile"}},
		}},
		{"shared release built as its newest subport", record.Change{
			ID: "shared", InitiatingTarget: "rb-mustache", Branch: "dockhand/bump/rb-mustache",
			Targets: []record.Target{{Name: "rb33-mustache", Portfile: "ruby/rb-mustache/Portfile", Subport: "rb33-mustache"}},
		}},
		{"several targets", record.Change{
			ID: "several", InitiatingTarget: "rsync", Branch: "dockhand/bump/rsync",
			Targets: []record.Target{{Name: "rrsync", Portfile: "net/rsync/Portfile", Subport: "rrsync"}, {Name: "rsync", Portfile: "net/rsync/Portfile"}},
		}},
	} {
		change := shape.change
		change.Disposition = record.ChangeOpen
		require.NoError(t, f.store.Update(t.Context(), f.repository, func(ctx context.Context, tx state.Tx) error {
			return tx.PutChange(ctx, change)
		}))
		hint := view.PortSelector(record.Job{ID: record.JobID("job_" + change.ID), Spec: record.JobSpec{Targets: change.Targets}})
		selected, err := f.engine.SelectContribution(t.Context(), workflow.ContributionSelector{Target: hint})
		require.NoError(t, err, "%s: status prints %q", shape.label, hint)
		require.Equal(t, change.ID, selected.ID, shape.label)
	}
}
