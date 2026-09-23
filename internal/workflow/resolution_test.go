package workflow_test

import (
	"context"
	"os/exec"
	"testing"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/state"
	"github.com/herbygillot/dockhand/internal/workflow"
	"github.com/stretchr/testify/require"
)

// masterAt makes the fixture repository answer the master fetch with its
// own master branch at the commit, as the CLI fixtures do.
func masterAt(t *testing.T, f *fixture, commit record.ObjectID) record.Source {
	t.Helper()
	require.NoError(t, f.repo.UpdateRefs(t.Context(), []git.RefChange{{Name: "refs/heads/master", Desired: git.RefValue{Exists: true, Object: string(commit)}}}))
	out, err := exec.CommandContext(t.Context(), "git", "-C", f.repo.Root, "config", "url."+f.repo.Root+".insteadOf", macports.PortsRepositoryURL).CombinedOutput()
	require.NoError(t, err, string(out))
	trees, err := f.repo.CommitTrees(t.Context(), []string{string(commit)})
	require.NoError(t, err)
	return record.Source{Commit: commit, Tree: record.ObjectID(trees[string(commit)]), Base: commit}
}

func TestResolveFreshWithoutRecordsOrContribution(t *testing.T) {
	t.Parallel()
	f, _ := bindingFixture(t)
	master := masterAt(t, f, f.source.Commit)
	request := workflow.ResolutionRequest{Action: record.Bump, Selection: macports.Selection{Selector: "fixture"}, Subject: "update", Platform: buildPlatform}
	// No database: nothing is read, and the selection is Fresh.
	without := &workflow.Engine{Repo: f.repo, Ports: f.engine.Ports}
	resolution, err := without.Resolve(t.Context(), request)
	require.NoError(t, err)
	require.Equal(t, workflow.Fresh, resolution.Kind)
	require.Equal(t, master, resolution.Source)
	require.Equal(t, "master", resolution.Branch)
	require.Equal(t, "update", resolution.Subject)
	require.Empty(t, resolution.ChangeID())
	_, err = without.Resolve(t.Context(), workflow.ResolutionRequest{Action: record.Verify, Selection: macports.Selection{Selector: "fixture"}})
	require.Error(t, err, "nothing is tracked without the records")
	// A database with no contribution for the port: Fresh too, and a path
	// selector never looks.
	for _, selector := range []string{"fixture", "devel/fixture/Portfile", "devel/fixture"} {
		resolution, err := f.engine.Resolve(t.Context(), workflow.ResolutionRequest{Action: record.Bump, Selection: macports.Selection{Selector: selector}, Platform: buildPlatform})
		require.NoError(t, err, selector)
		require.Equal(t, workflow.Fresh, resolution.Kind, selector)
		require.Equal(t, master, resolution.Source)
		require.Equal(t, selector, resolution.Selection.Selector)
	}
	// A verification requires a contribution and says so in its words.
	_, err = f.engine.Resolve(t.Context(), workflow.ResolutionRequest{Action: record.Verify, Selection: macports.Selection{Selector: "fixture"}})
	require.ErrorIs(t, err, state.ErrNotFound)
	require.ErrorContains(t, err, "no open contribution for fixture")
}

func TestResolveOntoAContributionWithoutAJobOfItsOwn(t *testing.T) {
	t.Parallel()
	f, _ := publicationFixtureWithTracking(t, true)
	masterAt(t, f, f.source.Base)
	resolution, err := f.engine.Resolve(t.Context(), workflow.ResolutionRequest{Action: record.BumpRevision, Selection: macports.Selection{Selector: "fixture", Variants: map[string]bool{"debug": true}}, Intent: record.EditIntent{KeepOldChecksums: true}, Platform: buildPlatform})
	require.NoError(t, err)
	require.Equal(t, workflow.Onto, resolution.Kind)
	require.Equal(t, f.source, resolution.Source, "the contribution's current revision")
	require.Equal(t, record.ChangeID("change"), resolution.ChangeID())
	require.Equal(t, "candidate", resolution.Branch)
	require.Equal(t, "update to 2", resolution.Subject, "the contribution's own subject")
	require.Equal(t, macports.Selection{Selector: "devel/fixture/Portfile", Variants: map[string]bool{"debug": true}}, resolution.Selection)
	require.Equal(t, record.EditIntent{KeepOldChecksums: true}, resolution.Intent, "an Onto inherits no intent from a job")
	require.Nil(t, resolution.Master, "an Onto does not fetch master")
	require.Contains(t, resolution.Detail, "lands as an amendment")
	// A verification resolves the same contribution as recorded: Tracked,
	// with the change and revision to check the captured branch against,
	// by name, by branch, or by change, and reads nothing else.
	for _, selection := range []workflow.ResolutionRequest{{Selection: macports.Selection{Selector: "fixture"}}, {Branch: "candidate"}, {ChangeID: "change"}, {Selection: macports.Selection{Selector: "fixture"}, Branch: "candidate"}} {
		selection.Action = record.Verify
		found, err := f.engine.Resolve(t.Context(), selection)
		require.NoError(t, err)
		require.Equal(t, workflow.Tracked, found.Kind)
		require.Equal(t, record.ChangeID("change"), found.ChangeID())
		require.Equal(t, "candidate", found.Branch)
		require.NotNil(t, found.Revision)
		require.Equal(t, f.source, found.Source)
		require.Nil(t, found.Master)
	}
	_, err = f.engine.Resolve(t.Context(), workflow.ResolutionRequest{Action: record.Verify, Selection: macports.Selection{Selector: "other"}, Branch: "candidate"})
	require.ErrorContains(t, err, "selector does not match contribution")
	_, err = f.engine.Resolve(t.Context(), workflow.ResolutionRequest{Action: record.Verify, Branch: "elsewhere"})
	require.ErrorIs(t, err, state.ErrNotFound)
	_, err = f.engine.Resolve(t.Context(), workflow.ResolutionRequest{Action: record.Verify})
	require.ErrorContains(t, err, "select a target, branch, or change")
	_, err = (&workflow.Engine{Repo: f.repo}).Resolve(t.Context(), workflow.ResolutionRequest{Action: record.Verify, Selection: macports.Selection{Selector: "fixture"}})
	require.Error(t, err, "a verification needs the records")
}

func TestResolveContinuesAPriorJobAndInheritsItsChoices(t *testing.T) {
	t.Parallel()
	f, req := preparationFixture(t, false)
	id := submitPreparation(t, f, req)
	for i := 0; i < 6; i++ {
		f.run(t, id)
		if job := f.status(t, id).Jobs[0].Job; job.State.Terminal() {
			break
		}
	}
	job := f.status(t, id).Jobs[0].Job
	require.Equal(t, record.JobCompleted, job.State)
	require.NotEmpty(t, job.ResultRevision)
	// Master holds the port at the commit the job prepared from, so the
	// continuation check can evaluate it there.
	master := masterAt(t, f, job.Spec.Source.Commit)
	request := workflow.ResolutionRequest{Action: record.BumpRevision, Selection: macports.Selection{Selector: "fixture", Variants: map[string]bool{"debug": true}}, Platform: buildPlatform}
	// A preview fetches master and does not check the continuation; the
	// prior job's choices are inherited as recorded.
	previewed, err := f.engine.Resolve(t.Context(), func() workflow.ResolutionRequest { r := request; r.Preview = true; return r }())
	require.NoError(t, err)
	require.Equal(t, workflow.Continue, previewed.Kind)
	require.Equal(t, &master, previewed.Master)
	require.False(t, previewed.Checked)
	require.Contains(t, previewed.Detail, "not checked in a preview")
	require.Equal(t, job.Spec.Source, previewed.Source, "the prior job's recorded source")
	require.Equal(t, "Rebuild dependents", previewed.Subject, "inherited from the prior job")
	require.Equal(t, job.Spec.Targets[0].Portfile, previewed.Selection.Selector)
	require.True(t, previewed.Selection.Variants["debug"], "the request's variants lie over the recorded ones")
	// The real thing checks master and the pull request; no PR is recorded
	// and master still holds the port, so the contribution continues.
	checked, err := f.engine.Resolve(t.Context(), request)
	require.NoError(t, err)
	require.Equal(t, workflow.Continue, checked.Kind)
	require.True(t, checked.Checked)
	require.Contains(t, checked.Detail, "no PR is recorded")
	// Master unreachable: the fetch is rewritten to a path that does not
	// exist, so it fails without the network; the contribution is
	// continued as recorded, and degraded says so.
	for _, args := range [][]string{{"config", "--unset", "url." + f.repo.Root + ".insteadOf"}, {"config", "url./missing/upstream.insteadOf", macports.PortsRepositoryURL}} {
		out, err := exec.CommandContext(t.Context(), "git", append([]string{"-C", f.repo.Root}, args...)...).CombinedOutput()
		require.NoError(t, err, string(out))
	}
	degraded, err := f.engine.Resolve(t.Context(), request)
	require.NoError(t, err)
	require.Equal(t, workflow.Continue, degraded.Kind)
	require.NotEmpty(t, degraded.Degraded)
	require.Contains(t, degraded.Detail, "master not checked")
	require.Nil(t, degraded.Master)
	// With no prior job to fall back on, the same failure is an error.
	_, err = f.engine.Resolve(t.Context(), workflow.ResolutionRequest{Action: record.Bump, Selection: macports.Selection{Selector: "other"}, Platform: buildPlatform})
	require.ErrorContains(t, err, "fetching authoritative MacPorts master")
}

// Adoption is the caller's write, made first; the resolution reads what it
// recorded, or in a dry run would have recorded, and writes nothing.
func TestResolveAdoptedReadsWhatAdoptionRecorded(t *testing.T) {
	t.Parallel()
	f, _ := manualPublicationFixture(t)
	master := masterAt(t, f, f.source.Base)
	request := workflow.ResolutionRequest{Action: record.BumpRevision, Selection: macports.Selection{Selector: "fixture"}, Platform: buildPlatform}
	dryRun, err := f.engine.AdoptContribution(t.Context(), workflow.AdoptRequest{Branch: "candidate", Target: "fixture", Upstream: master.Commit, Platform: buildPlatform, DryRun: true})
	require.NoError(t, err)
	previewed, err := f.engine.ResolveAdopted(t.Context(), dryRun, func() workflow.ResolutionRequest { r := request; r.Preview = true; return r }())
	require.NoError(t, err)
	require.Equal(t, workflow.Adopt, previewed.Kind)
	require.Equal(t, f.source, previewed.Source, "the revision adoption would record")
	require.Equal(t, "candidate", previewed.Branch)
	_, err = f.engine.SelectContribution(t.Context(), workflow.ContributionSelector{Branch: "candidate"})
	require.ErrorIs(t, err, state.ErrNotFound, "a dry run records nothing")
	recorded, err := f.engine.AdoptContribution(t.Context(), workflow.AdoptRequest{Branch: "candidate", Target: "fixture", Upstream: master.Commit, Platform: buildPlatform})
	require.NoError(t, err)
	adopted, err := f.engine.ResolveAdopted(t.Context(), recorded, request)
	require.NoError(t, err)
	require.Equal(t, workflow.Adopt, adopted.Kind)
	change, err := f.engine.SelectContribution(t.Context(), workflow.ContributionSelector{Branch: "candidate"})
	require.NoError(t, err)
	require.Equal(t, change.ID, adopted.ChangeID())
	require.Equal(t, "update to 2", adopted.Subject, "the contribution's own subject")
	_, err = f.engine.ResolveAdopted(t.Context(), recorded, workflow.ResolutionRequest{Action: record.Verify, Selection: macports.Selection{Selector: "fixture"}})
	require.ErrorIs(t, err, workflow.ErrInvalidRequest, "only an update prepares onto an adopted branch")
	_ = context.Background
}
