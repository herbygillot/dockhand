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
	_, err = without.Resolve(t.Context(), workflow.ResolutionRequest{Action: record.Bump, Selection: macports.Selection{Selector: "fixture"}, Branch: "candidate", Adopt: true})
	require.ErrorContains(t, err, "needs the state database")
	// A database with no contribution for the port: Fresh too, and a path
	// selector never looks.
	for _, selector := range []string{"fixture", "devel/fixture/Portfile", "devel/fixture"} {
		resolution, err := f.engine.Resolve(t.Context(), workflow.ResolutionRequest{Action: record.Bump, Selection: macports.Selection{Selector: selector}, Platform: buildPlatform})
		require.NoError(t, err, selector)
		require.Equal(t, workflow.Fresh, resolution.Kind, selector)
		require.Equal(t, master, resolution.Source)
		require.Equal(t, selector, resolution.Selection.Selector)
	}
	// Offline forbids the fetch a Fresh needs.
	_, err = f.engine.Resolve(t.Context(), workflow.ResolutionRequest{Action: record.Bump, Selection: macports.Selection{Selector: "fixture"}, Offline: true})
	require.ErrorIs(t, err, workflow.ErrOffline)
	// A verification requires a contribution and says so in its words.
	_, err = f.engine.Resolve(t.Context(), workflow.ResolutionRequest{Action: record.Verify, Selection: macports.Selection{Selector: "fixture"}, Require: true, Lookup: true})
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
	// A verification's lookup finds the same contribution as a Continue of
	// nothing in particular: the change to check the branch against.
	found, err := f.engine.Resolve(t.Context(), workflow.ResolutionRequest{Action: record.Verify, Selection: macports.Selection{Selector: "fixture"}, Require: true, Lookup: true})
	require.NoError(t, err)
	require.Equal(t, workflow.Onto, found.Kind)
	require.Equal(t, record.ChangeID("change"), found.ChangeID())
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
	// Lookup stops at the records: no master, no check.
	looked, err := f.engine.Resolve(t.Context(), func() workflow.ResolutionRequest { r := request; r.Lookup = true; return r }())
	require.NoError(t, err)
	require.Equal(t, workflow.Continue, looked.Kind)
	require.Nil(t, looked.Master)
	require.False(t, looked.Checked)
	require.Equal(t, job.Spec.Source, looked.Source, "the prior job's recorded source")
	require.Equal(t, job.ID, looked.Prior.ID)
	require.Equal(t, "Rebuild dependents", looked.Subject, "inherited from the prior job")
	require.Equal(t, job.Spec.Targets[0].Portfile, looked.Selection.Selector)
	require.True(t, looked.Selection.Variants["debug"], "the request's variants lie over the recorded ones")
	// A preview fetches master and does not check the continuation.
	previewed, err := f.engine.Resolve(t.Context(), func() workflow.ResolutionRequest { r := request; r.Preview = true; return r }())
	require.NoError(t, err)
	require.Equal(t, workflow.Continue, previewed.Kind)
	require.Equal(t, &master, previewed.Master)
	require.False(t, previewed.Checked)
	require.Contains(t, previewed.Detail, "not checked in a preview")
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

func TestResolveAdoptsABranchThenPreparesOntoIt(t *testing.T) {
	t.Parallel()
	f, _ := manualPublicationFixture(t)
	master := masterAt(t, f, f.source.Base)
	request := workflow.ResolutionRequest{Action: record.BumpRevision, Selection: macports.Selection{Selector: "fixture"}, Branch: "candidate", Adopt: true, Platform: buildPlatform}
	previewed, err := f.engine.Resolve(t.Context(), func() workflow.ResolutionRequest { r := request; r.Preview = true; return r }())
	require.NoError(t, err)
	require.Equal(t, workflow.Adopt, previewed.Kind)
	require.Equal(t, f.source, previewed.Source, "the revision adoption would record")
	require.Equal(t, "candidate", previewed.Branch)
	require.Equal(t, &master, previewed.Master)
	_, err = f.engine.SelectContribution(t.Context(), workflow.ContributionSelector{Branch: "candidate"})
	require.ErrorIs(t, err, state.ErrNotFound, "a preview records nothing")
	adopted, err := f.engine.Resolve(t.Context(), request)
	require.NoError(t, err)
	require.Equal(t, workflow.Adopt, adopted.Kind)
	change, err := f.engine.SelectContribution(t.Context(), workflow.ContributionSelector{Branch: "candidate"})
	require.NoError(t, err)
	require.Equal(t, change.ID, adopted.ChangeID())
	// A path selector with --adopt is refused, as adoption refuses it.
	_, err = f.engine.Resolve(t.Context(), workflow.ResolutionRequest{Action: record.BumpRevision, Selection: macports.Selection{Selector: "devel/fixture/Portfile"}, Branch: "candidate", Adopt: true, Platform: buildPlatform})
	require.ErrorContains(t, err, "is not a port name")
	_ = context.Background
}
