package engine

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/model"
)

// servePrepared has serve prepare and check jq's update, and returns the
// branch.
func servePrepared(t *testing.T, e *Engine) model.Branch {
	t.Helper()
	e.OutdatedReader = &newReleases{}
	e.PortReader = fakePorts{directories: map[string][]macports.PortInfo{"textproc/jq": {port("jq")}}}
	e.Providers = map[string]Provider{"command": &scriptedProvider{}}
	report, err := e.Outdated(t.Context(), OutdatedRequest{Maintainers: []string{"@ada"}})
	require.NoError(t, err)
	plan, err := e.PlanOutdated(t.Context(), report)
	require.NoError(t, err)
	prepared := e.PrepareOutdated(t.Context(), plan, PrepareOptions{Origin: model.OriginServe, Check: true, Environments: []model.Environment{{Provider: "command"}}, Tests: model.TestsDeclared})
	require.Len(t, prepared, 1)
	require.Empty(t, prepared[0].Problem)
	run, err := e.Drive(t.Context(), session(t, e), prepared[0].Run.ID)
	require.NoError(t, err)
	require.Equal(t, model.RunPassed, run.State, run.Detail)
	return prepared[0].Branch
}

func TestServeSubmitsOnlyWhatPassedWithNothingToLookAt(t *testing.T) {
	f := setup(t)
	e, _ := f.withPreparer(t)
	fake := f.withFork(t, e)
	branch := servePrepared(t, e)
	committedUpdate(t, e) // a person's branch, which serve never submits

	candidates, err := e.ServeCandidates(t.Context())
	require.NoError(t, err)
	require.Len(t, candidates, 1, "only the branch serve prepared, and only once its check passed")
	require.Equal(t, branch.ID, candidates[0].Branch.ID)
	require.Empty(t, candidates[0].Held)

	submitted, err := e.SubmitForServe(t.Context(), candidates[0])
	require.NoError(t, err)
	require.True(t, submitted.Created)
	require.Contains(t, fake.created[0].Desired.Body, ServeNote, "the pull request says no person reviewed it")
	require.Contains(t, fake.created[0].Desired.Body, "- [ ] tested basic functionality of all binary files?", "serve states nothing only a person can")

	again, err := e.ServeCandidates(t.Context())
	require.NoError(t, err)
	require.Empty(t, again, "a branch with a pull request is done")
}

func TestServeHoldsAnUpdateWhoseUpstreamChangedItsLicense(t *testing.T) {
	f := setup(t)
	e, p := f.withPreparer(t)
	f.withFork(t, e)
	p.upstream = [2]map[string]string{{"LICENSE": "MIT\n"}, {"LICENSE": "GPL-3\n"}}
	branch := servePrepared(t, e)

	status, err := e.BranchStatus(t.Context(), branch)
	require.NoError(t, err)
	require.Equal(t, []string{"upstream's LICENSE changed; the Portfile's license line may need to follow"}, status.Held)
	candidates, err := e.ServeCandidates(t.Context())
	require.NoError(t, err)
	require.Equal(t, []string{"upstream's LICENSE changed; the Portfile's license line may need to follow"}, candidates[0].Held)
	_, err = e.SubmitForServe(t.Context(), candidates[0])
	require.ErrorContains(t, err, "is held for a look: upstream's LICENSE changed")
}

// submit --passing and serve read one definition of a passing branch:
// editing a passed branch takes it out of both.
func TestPassingIsOneDefinitionForSubmitAndServe(t *testing.T) {
	f := setup(t)
	e, _ := f.withPreparer(t)
	f.withFork(t, e)
	branch := servePrepared(t, e)
	committedUpdate(t, e) // checked by nobody, so neither passing nor not

	passing, err := e.PassingBranches(t.Context())
	require.NoError(t, err)
	require.Len(t, passing.Ready, 1)
	require.Equal(t, branch.ID, passing.Ready[0].Branch.ID)
	require.Zero(t, passing.Others)

	portfile := filepath.Join(branch.Worktree, "textproc", "jq", "Portfile")
	data, err := os.ReadFile(portfile)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(portfile, append(data, "# edited after the check\n"...), 0o644))
	passing, err = e.PassingBranches(t.Context())
	require.NoError(t, err)
	require.Empty(t, passing.Ready)
	require.Equal(t, 1, passing.Others, "its check no longer covers its files")
	candidates, err := e.ServeCandidates(t.Context())
	require.NoError(t, err)
	require.Empty(t, candidates, "and serve submits it no more than submit --passing would")
}
