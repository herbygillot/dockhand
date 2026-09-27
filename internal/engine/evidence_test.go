package engine

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/provider"
)

// A branch changing jq and libharbor, committed, and checkable with the
// command provider.
func twoPortBranch(t *testing.T, e *Engine) model.Branch {
	t.Helper()
	branch := committedUpdate(t, e)
	run(t, branch.Worktree, "sparse-checkout", "add", "devel/libharbor")
	write(t, branch.Worktree, map[string]string{"devel/libharbor/Portfile": "name libharbor\nversion 3\n"})
	run(t, branch.Worktree, "commit", "-q", "-am", "libharbor: update to 3")
	e.PortReader = fakePorts{directories: map[string][]macports.PortInfo{
		"textproc/jq": {port("jq")}, "devel/libharbor": {port("libharbor")},
	}}
	e.Providers = map[string]provider.Provider{"command": &scriptedProvider{}}
	return branch
}

// checkHead checks the branch's committed files, narrowed to only when
// given, and requires the check to pass.
func checkHead(t *testing.T, e *Engine, branch model.Branch, only ...string) model.Run {
	t.Helper()
	capture, err := e.Capture(t.Context(), CaptureRequest{Branch: branch, Mode: CaptureHead})
	require.NoError(t, err)
	plan, err := e.PlanCheck(t.Context(), PlanRequest{Revision: capture.Revision, Environments: []model.Environment{tahoeArm}, Only: only})
	require.NoError(t, err)
	queued, err := e.Enqueue(t.Context(), branch, plan, model.OriginPerson)
	require.NoError(t, err)
	completed, err := e.Drive(t.Context(), session(t, e), queued.ID)
	require.NoError(t, err)
	require.Equal(t, model.RunPassed, completed.State)
	return completed
}

// A check narrowed with --only never quietly shrinks what submit requires
// (Design v3 §7), for submit, submit --passing, and serve alike; and a
// narrowed check after a full one keeps the full one's results, since a
// result holds for its tree. From the 2026-09-25 implementation review.
func TestANarrowedCheckNeverShrinksWhatSubmitRequires(t *testing.T) {
	f := setup(t)
	e, _ := f.withPreparer(t)
	f.withFork(t, e)
	branch := twoPortBranch(t, e)

	narrowed := checkHead(t, e, branch, "jq")
	submission, err := e.PlanSubmit(t.Context(), SubmitRequest{Branch: branch, Title: "jq, libharbor: update"})
	require.NoError(t, err)
	require.Len(t, submission.Blocking, 1)
	require.Contains(t, submission.Blocking[0], "libharbor is changed, and no check of these files built it")
	require.Contains(t, submission.Body, "| libharbor | · not run |", "the pull request shows what wasn't built")
	_, err = e.PlanSubmit(t.Context(), SubmitRequest{Branch: branch, Title: "jq, libharbor: update", Accept: []string{"libharbor"}})
	require.ErrorContains(t, err, "--accept libharbor: no check of these files built it")
	passing, err := e.PassingBranches(t.Context())
	require.NoError(t, err)
	require.Empty(t, passing.Ready, "submit --passing and serve don't count it as passing")
	require.Equal(t, 1, passing.Others)

	full := checkHead(t, e, branch)
	again := checkHead(t, e, branch, "jq")
	submission, err = e.PlanSubmit(t.Context(), SubmitRequest{Branch: branch, Title: "jq, libharbor: update"})
	require.NoError(t, err)
	require.Empty(t, submission.Blocking, "the full check's result for libharbor still holds")
	require.Equal(t, again.ID, submission.Evidence.Run.ID)
	require.Len(t, submission.Evidence.Earlier, 1, "the first narrowed check adds nothing")
	require.Equal(t, full.ID, submission.Evidence.Earlier[0].ID)
	require.NotEqual(t, narrowed.ID, full.ID)
	// Tested on names both checks, each beside the run it had.
	require.Regexp(t, `\(Run IDs: command_[a-z0-9]{16} - checked in check-\d+; command_[a-z0-9]{16} - checked in check-\d+\)`, submission.Body)
	require.Contains(t, submission.Body, " - checked in "+again.Name())
	require.Contains(t, submission.Body, " - checked in "+full.Name())
	passing, err = e.PassingBranches(t.Context())
	require.NoError(t, err)
	require.Len(t, passing.Ready, 1)
}

// One rule says whether a check's result stands for a target in an
// environment: the check ran in that whole environment, and planned the
// target there. (The architecture review of 2026-09-27, finding 1.)
func TestAResultCountsWhereItsCheckPlannedTheTarget(t *testing.T) {
	arm := tahoeArm
	arm.DeveloperTools = model.DeveloperToolsCommandLine
	xcode := arm
	xcode.DeveloperTools = model.DeveloperToolsXcode
	recorded := model.Plan{Environments: []model.Environment{arm, tahoeX86},
		Builds: []model.EnvironmentPlan{
			{Environment: arm, Order: []model.TargetID{"libharbor", "harbor-cli"}, Unmet: []model.Unmet{{Target: "harbor-cli", Environment: arm, Needs: model.RequiresXcode}},
				Exclusions: []model.Exclusion{{Target: model.Target{Name: "harbor-intel"}, Reason: "not defined there"}}},
			{Environment: tahoeX86, Order: []model.TargetID{"libharbor", "harbor-intel"}},
		}}
	require.True(t, Counts(recorded, "libharbor", arm))
	require.True(t, Counts(recorded, "harbor-cli", arm), "what it found there stands, an unmet need too")
	require.False(t, Counts(recorded, "harbor-intel", arm), "excluded there")
	require.True(t, Counts(recorded, "harbor-intel", tahoeX86))
	require.False(t, Counts(recorded, "harbor-cli", tahoeX86), "not planned there: --only left it out")
	require.False(t, Counts(recorded, "libharbor", xcode), "the same release with other tools is another environment")
}
