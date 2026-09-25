package engine

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/model"
)

// newReleases stands in for upstream discovery: jq has a newer release,
// libharbor is current, and lost can't be checked.
type newReleases struct{ asked []OutdatedRequest }

func (n *newReleases) Outdated(_ context.Context, _ model.ObjectID, request OutdatedRequest) ([]OutdatedPort, error) {
	n.asked = append(n.asked, request)
	return []OutdatedPort{
		{Port: "lost", Problem: "no forge could be found for its master_sites"},
		{Port: "libharbor", Current: "2", Newest: "2"},
		{Port: "jq", Current: "1.7.1", Newest: "1.8.1", Outdated: true},
	}, nil
}

func TestOutdatedPortsArePreparedOneBranchEach(t *testing.T) {
	f := setup(t)
	e, _ := f.withPreparer(t)
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
