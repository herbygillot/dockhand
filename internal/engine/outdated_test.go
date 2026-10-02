package engine

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/outdated"
	"github.com/herbygillot/dockhand/internal/upstream"
)

// newReleases stands in for upstream discovery: jq has a newer release,
// libharbor is current, and lost can't be checked.
type newReleases struct{ asked []OutdatedRequest }

func (n *newReleases) Outdated(_ context.Context, _ model.ObjectID, request OutdatedRequest) ([]OutdatedPort, error) {
	n.asked = append(n.asked, request)
	return []OutdatedPort{
		{Port: "lost", Problem: "no forge could be found for its master_sites"},
		{Port: "libharbor", Current: "2", Newest: "2"},
		{Port: "jq", Current: "1.7.1", Newest: "1.8.1", Outdated: true, Release: &model.Release{Version: "1.8.1", Forge: "github", Tag: "jq-1.8.1", Commit: "found by outdated"}},
	}, nil
}

func TestOutdatedPortsArePreparedOneBranchEach(t *testing.T) {
	f := setup(t)
	e, p := f.withPreparer(t)
	releases := &newReleases{}
	e.OutdatedReader = releases
	e.PortReader = fakePorts{directories: map[string][]macports.PortInfo{"textproc/jq": {port("jq")}}}

	_, err := e.Outdated(t.Context(), OutdatedRequest{})
	require.ErrorContains(t, err, "name ports, or ask for yours with --mine")
	report, err := e.Outdated(t.Context(), OutdatedRequest{Maintainers: []string{"@ada"}})
	require.NoError(t, err)
	require.Equal(t, f.upstreamMaster(t), report.Master, "outdated looks at master as fetched now")
	require.Equal(t, []string{"jq", "libharbor", "lost"}, []string{report.Ports[0].Port, report.Ports[1].Port, report.Ports[2].Port})

	plan, err := e.PlanOutdated(t.Context(), report)
	require.NoError(t, err)
	require.Len(t, plan.Updates, 1)
	require.Equal(t, "jq", plan.Updates[0].Port.Port)
	require.Regexp(t, `^jq-[a-z0-9]{4}$`, plan.Updates[0].Name)
	require.Equal(t, []SkippedUpdate{{Port: "lost", Reason: "no forge could be found for its master_sites"}}, plan.Skipped)

	prepared := e.PrepareOutdated(t.Context(), plan, PrepareOptions{Origin: model.OriginServe, Check: true, Environments: []model.Environment{tahoeArm}, Tests: model.TestsDeclared})
	require.Len(t, prepared, 1)
	done := prepared[0]
	require.Empty(t, done.Problem)
	require.True(t, done.Tidied)
	require.Equal(t, "found by outdated", p.requests[0].Release.Commit, "the release outdated found, not one asked for again (finding 36)")
	require.Equal(t, model.OriginServe, done.Branch.Origin)
	require.Equal(t, []string{"jq: update to 1.8.1"}, log(t, done.Branch.Worktree, done.Branch.Base))
	require.NotNil(t, done.Run)
	require.Equal(t, model.RunQueued, done.Run.State)
	require.Equal(t, model.OriginServe, done.Run.Origin)
	stored, err := e.Branch(t.Context(), done.Branch.ID)
	require.NoError(t, err)
	require.Equal(t, model.OriginServe, stored.Origin, "the record says serve started it")

	again, err := e.PlanOutdated(t.Context(), report)
	require.NoError(t, err)
	require.Empty(t, again.Updates)
	require.Equal(t, "already in "+done.Branch.ShortName(), again.Skipped[0].Reason)
}

// Discovery's assessment decides what outdated says of a port. One whose
// newer version was set aside, with nothing newer beyond it, is neither
// outdated nor current: it carries what was set aside, and no branch is
// planned for it (the update-workflow review's finding 6). An update found
// beyond a version set aside is outdated as any.
func TestAnUncertainPortIsNeitherOutdatedNorPlanned(t *testing.T) {
	aside := []SetAside{{Tag: "v2.0", Version: "2.0", Source: "2.0", Predates: "v1.0"}}
	uncertain := outdatedPort(outdated.Port{Selector: "yq", Result: upstream.Result{CurrentVersion: "1.0", CandidateVersion: "2.0", Assessment: upstream.Uncertain, SetAside: aside}})
	require.Equal(t, OutdatedPort{Port: "yq", Current: "1.0", Newest: "2.0", Uncertain: aside}, uncertain)
	release := &model.Release{Version: "1.1"}
	updated := outdatedPort(outdated.Port{Selector: "yq", Result: upstream.Result{CurrentVersion: "1.0", CandidateVersion: "1.1", Assessment: upstream.UpdateAvailable, SetAside: aside, Release: release}})
	require.Equal(t, OutdatedPort{Port: "yq", Current: "1.0", Newest: "1.1", Outdated: true, Release: release}, updated)

	f := setup(t)
	e, _ := f.withPreparer(t)
	plan, err := e.PlanOutdated(t.Context(), OutdatedReport{Ports: []OutdatedPort{uncertain}})
	require.NoError(t, err)
	require.Empty(t, plan.Updates)
	require.Empty(t, plan.Skipped, "the command says why it's left for a look")
}

// A subport checked with its sibling, sharing its release, moves with
// that sibling's update rather than starting a branch of its own (batch
// 30).
func TestASubportCheckedWithItsSiblingMovesWithIt(t *testing.T) {
	release := &model.Release{Version: "6.1.5"}
	sibling := outdatedPort(outdated.Port{Selector: "py310-cbor2", With: "py-cbor2", Result: upstream.Result{CurrentVersion: "5.7.1", CandidateVersion: "6.1.5", Assessment: upstream.UpdateAvailable, Release: release}})
	require.Equal(t, "py-cbor2", sibling.With)
	main := outdatedPort(outdated.Port{Selector: "py-cbor2", Result: upstream.Result{CurrentVersion: "5.7.1", CandidateVersion: "6.1.5", Assessment: upstream.UpdateAvailable, Release: release}})

	f := setup(t)
	e, _ := f.withPreparer(t)
	plan, err := e.PlanOutdated(t.Context(), OutdatedReport{Ports: []OutdatedPort{main, sibling}})
	require.NoError(t, err)
	require.Len(t, plan.Updates, 1)
	require.Equal(t, "py-cbor2", plan.Updates[0].Port.Port)
	require.Equal(t, []SkippedUpdate{{Port: "py310-cbor2", Reason: "moves with py-cbor2, whose release it shares"}}, plan.Skipped)
}
