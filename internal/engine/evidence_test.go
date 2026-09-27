package engine

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/buildenv"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/model"
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
	e.Providers = map[string]buildenv.Provider{"command": &scriptedProvider{}}
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

// identified is a scripted provider that says what its environments are
// now (buildenv.IdentityProvider).
type identified struct {
	scriptedProvider
	identity string
}

func (p *identified) Identity(context.Context, model.Environment) (string, error) {
	return p.identity, nil
}

// A result stands for the environment it ran in only while it is that
// environment: made again from another source, or with other tools, it is
// another, and what was built in it is built again (decision 28). Where
// the provider can't say what it is now, the result stands, as it did
// before environments had identities.
func TestAResultStandsOnlyWhileItsEnvironmentDoes(t *testing.T) {
	f := setup(t)
	e, _ := f.withPreparer(t)
	f.withFork(t, e)
	branch := twoPortBranch(t, e)
	provider := &identified{identity: "source sha256:a; setup 1"}
	e.Providers = map[string]buildenv.Provider{"command": provider}
	blocking := func() string {
		t.Helper()
		submission, err := e.PlanSubmit(t.Context(), SubmitRequest{Branch: branch, Title: "jq, libharbor: update"})
		require.NoError(t, err)
		return strings.Join(submission.Blocking, "\n")
	}

	checkHead(t, e, branch)
	require.Empty(t, blocking())
	evidence, _, err := e.EvidenceFor(t.Context(), branch.ID, model.ObjectID(run(t, branch.Worktree, "rev-parse", "HEAD^{tree}")))
	require.NoError(t, err)
	require.NotEmpty(t, evidence.Executions)
	for _, execution := range evidence.Executions {
		require.Equal(t, "source sha256:a; setup 1", execution.Identity, "recorded as the execution began")
	}

	provider.identity = ""
	require.Empty(t, blocking(), "an environment that can't say what it is now is taken as it was")

	provider.identity = "source sha256:b; setup 1"
	remade := blocking()
	require.Contains(t, remade, "jq was checked in "+DescribeEnvironment(tahoeArm)+" before it was made again")
	require.Contains(t, remade, "libharbor was checked in")
	passing, err := e.PassingBranches(t.Context())
	require.NoError(t, err)
	require.Empty(t, passing.Ready, "submit --passing and serve don't count it as passing")

	// A narrowed check in the environment as it is now builds what it
	// selects; what it left out still needs building there, whichever
	// check built it before.
	checkHead(t, e, branch, "jq")
	remade = blocking()
	require.NotContains(t, remade, "jq")
	require.Contains(t, remade, "libharbor was checked in "+DescribeEnvironment(tahoeArm)+" before it was made again")
	checkHead(t, e, branch, "libharbor")
	require.Empty(t, blocking(), "the two narrowed checks together stand for the environment as it is now")
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
	in := func(environment model.Environment) model.GuestExecution {
		return model.GuestExecution{ID: "execution", Environment: environment, Identity: "origin a"}
	}
	require.True(t, Counts(recorded, in(arm), "libharbor", "origin a"))
	require.True(t, Counts(recorded, in(arm), "harbor-cli", "origin a"), "what it found there stands, an unmet need too")
	require.False(t, Counts(recorded, in(arm), "harbor-intel", "origin a"), "excluded there")
	require.True(t, Counts(recorded, in(tahoeX86), "harbor-intel", "origin a"))
	require.False(t, Counts(recorded, in(tahoeX86), "harbor-cli", "origin a"), "not planned there: --only left it out")
	require.False(t, Counts(recorded, in(xcode), "libharbor", "origin a"), "the same release with other tools is another environment")

	// And the environment is still the one it ran in: made from the same
	// source, with the same tools, set up and verified the same way.
	require.False(t, Counts(recorded, in(arm), "libharbor", "origin b"), "remade since")
	require.True(t, Counts(recorded, in(arm), "libharbor", ""), "its identity now is unknown")
	legacy := in(arm)
	legacy.Identity = ""
	require.False(t, Counts(recorded, legacy, "libharbor", "origin a"), "it ran before identities were recorded, and the environment has been made since")
	require.True(t, Counts(recorded, model.GuestExecution{Environment: arm}, "libharbor", "origin b"), "no execution ran it: planning found it unmet")
}
