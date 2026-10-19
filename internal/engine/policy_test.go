package engine

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/buildenv"
	"github.com/herbygillot/dockhand/internal/macos"
	"github.com/herbygillot/dockhand/internal/model"
)

// advisoryHarbor builds every target, and libharbor's tests fail when the
// check's policy lets them: advisory in one check, required in another.
type advisoryHarbor struct{}

func (*advisoryHarbor) Name() string { return "command" }
func (*advisoryHarbor) Execute(_ context.Context, job buildenv.Job, build buildenv.Build) error {
	for _, target := range job.Targets {
		tests := model.TestsPassed
		if target.ID == "libharbor" {
			tests = model.TestsFailed
		}
		if err := build.Record(model.TargetResult{Target: target.ID, Outcome: model.OutcomePassed, Tests: tests}); err != nil {
			return err
		}
	}
	return nil
}

// A result keeps the policy of the check that built it (the person's
// decision on D1, 2026-09-27). After a full advisory check, a check of jq
// alone with --tests required leaves libharbor's advisory result standing:
// the submission isn't blocked by it, and the pull request says under
// which check's policy its tests failed.
func TestAnEarlierAdvisoryResultKeepsItsPolicy(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e, _ := f.withPreparer(t)
	f.withFork(t, e)
	branch := twoPortBranch(t, e)
	e.Providers = map[string]buildenv.Provider{"command": &advisoryHarbor{}}
	checkHead(t, e, branch)
	capture, err := e.Capture(t.Context(), CaptureRequest{Branch: branch, Mode: CaptureHead})
	require.NoError(t, err)
	plan, err := e.PlanCheck(t.Context(), PlanRequest{Revision: capture.Revision, Environments: []model.Environment{tahoeArm}, Only: []string{"jq"}, Tests: model.TestsRequired})
	require.NoError(t, err)
	queued, err := e.Enqueue(t.Context(), branch, plan, model.OriginPerson)
	require.NoError(t, err)
	run, err := e.Drive(t.Context(), session(t, e), queued.ID)
	require.NoError(t, err)
	require.Equal(t, model.RunPassed, run.State, run.Detail)

	submission, err := e.PlanSubmit(t.Context(), SubmitRequest{Branch: branch, Title: "jq, libharbor: update"})
	require.NoError(t, err)
	require.Empty(t, submission.Blocking)
	require.Contains(t, submission.Body, "| libharbor | ✓ build passed; tests failed (advisory, check-1) |")
}

// behindProvider builds with an older MacPorts than this Mac's.
type behindProvider struct{ scriptedProvider }

func (*behindProvider) Name() string { return buildenv.Tart }

func (*behindProvider) MacPortsBehind(context.Context, model.Environment) (string, string) {
	return "2.12.6", "2.12.7"
}

// A plan says the environments it builds on whose MacPorts is older than
// this Mac's, which reads their ports, in one line, with how to make each
// image again, an Xcode image's Xcode named; one it builds nothing on isn't
// said (the person's "yes", 2026-10-16; field testing, the same day).
func TestPlanNotesSayAnEnvironmentBehindThisMacsMacPorts(t *testing.T) {
	e := &Engine{Providers: map[string]buildenv.Provider{buildenv.Tart: &behindProvider{}}}
	at := func(darwin string) model.Environment {
		return model.Environment{Provider: buildenv.Tart, Platform: model.Platform{OS: "darwin", Version: darwin, Architecture: "arm64"}}
	}
	xcode := at("25")
	xcode.DeveloperTools = model.DeveloperToolsXcode
	monterey, sonoma := at("21"), at("23")
	tahoe, err := macos.ReleaseForDarwin(25)
	require.NoError(t, err)
	plan := model.Plan{Environments: []model.Environment{monterey, sonoma, xcode}, Tests: model.TestsDeclared,
		Builds: []model.EnvironmentPlan{{Environment: monterey, Order: []model.TargetID{"jq"}}, {Environment: sonoma}, {Environment: xcode, Order: []model.TargetID{"jq"}}}}
	require.Equal(t, []string{"tart macOS 12 (Monterey) arm64 and tart macOS 26 (Tahoe) arm64 with Xcode have MacPorts 2.12.6, older than this Mac's 2.12.7, which reads their ports: " +
		"dockhand setup tart monterey --rebuild; dockhand setup tart tahoe --rebuild --xcode <folder> (Xcode " + tahoe.Xcode + "'s .xip, or a folder xcodes downloads it into)"}, e.PlanNotes(t.Context(), plan))
}
