package engine

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/model"
)

// A baseline is planned the way a check is, from the base's own Portfiles
// in each environment: a port that needs Xcode where there are only the
// Command Line Tools is unmet there, not sent there; and it builds the
// ports it names, not the rest of their directories. (The architecture
// review of 2026-09-27, finding 2.)
func TestABaselineKeepsEachEnvironmentsRequirements(t *testing.T) {
	f := setup(t)
	e := f.open(t)
	revision := harborBranch(t, e)
	e.PortReader = xcodeOnArm{fakePorts: harborPorts()}
	arm := tahoeArm
	arm.DeveloperTools = model.DeveloperToolsCommandLine
	intel := tahoeX86
	intel.DeveloperTools = model.DeveloperToolsXcode
	plan, err := e.PlanCheck(t.Context(), PlanRequest{Revision: revision, Environments: []model.Environment{arm, intel}})
	require.NoError(t, err)
	branch, err := e.Branch(t.Context(), revision.Branch)
	require.NoError(t, err)
	e.Providers = map[string]Provider{"command": &scriptedProvider{}}
	queued, err := e.Enqueue(t.Context(), branch, plan, model.OriginPerson)
	require.NoError(t, err)
	_, err = e.Drive(t.Context(), session(t, e), queued.ID)
	require.NoError(t, err)

	baseline, err := e.PlanBaseline(t.Context(), branch, []string{"libharbor", "harbor-viewer", "harbor-viewer-legacy"})
	require.NoError(t, err)
	require.Equal(t, revision.Source.Base, baseline.Revision.Source.Commit)
	require.Equal(t, []string{"libharbor:unchanged:also", "harbor-viewer:unchanged:also", "harbor-viewer-legacy:unchanged:also"}, names(baseline.Plan.Targets),
		"not harbor-viewer's other subports, nor the ports it depends on")
	library, _ := baseline.Plan.Target("libharbor")
	require.True(t, library.NeedsXcodeIn(arm))
	_, unmet := baseline.Plan.UnmetIn(arm, "libharbor")
	require.True(t, unmet, "unmet where there is no Xcode, not sent there")
	_, unmet = baseline.Plan.UnmetIn(intel, "libharbor")
	require.False(t, unmet)
	_, unmet = baseline.Plan.UnmetIn(arm, "harbor-viewer")
	require.True(t, unmet, "not built against master's libharbor there")
	require.Equal(t, []model.Exclusion{{Target: model.Target{Name: "harbor-viewer-legacy", Portfile: "graphics/harbor-viewer/Portfile", Subport: "harbor-viewer-legacy"}, Platform: arm.Platform, Reason: "supported_archs x86_64 only"}},
		baseline.Plan.Exclusions, "excluded once where it's unsupported, and built where it is, though its directory was named twice")
}

// A baseline explains a check, so it builds at the base that check
// started from, even after a rebase has moved the branch's base on. (The
// architecture review of 2026-09-27, finding 2.)
func TestABaselineUsesTheCheckedBase(t *testing.T) {
	f := setup(t)
	e, _ := f.withPreparer(t)
	branch := twoPortBranch(t, e)
	checkHead(t, e, branch)
	checked, err := e.Branch(t.Context(), branch.ID)
	require.NoError(t, err)
	write(t, f.upstream, map[string]string{"devel/other/Portfile": "name other\nversion 1\n"})
	run(t, f.upstream, "add", "devel/other/Portfile")
	run(t, f.upstream, "commit", "-q", "-m", "other: new port")
	_, err = e.Rebase(t.Context(), checked)
	require.NoError(t, err)
	current, err := e.Branch(t.Context(), branch.ID)
	require.NoError(t, err)
	require.NotEqual(t, checked.Base, current.Base, "the rebase moved the branch's base on")

	baseline, err := e.PlanBaseline(t.Context(), current, []string{"jq"})
	require.NoError(t, err)
	require.Equal(t, checked.Base, baseline.Revision.Source.Commit)
	require.Equal(t, checked.Base, baseline.Revision.Source.Base)
}

// Without --only, a baseline takes the ports that failed at install or
// test somewhere. One that failed only before building, at lint, fetch, or
// checksum, is left out, since master can't speak to it; one blocked or
// unmet didn't fail.
func TestABaselineTakesPortsThatFailedWhileBuilding(t *testing.T) {
	failed := func(phase model.Phase) model.TargetResult {
		return model.TargetResult{Outcome: model.OutcomeFailed, Phase: phase}
	}
	passed := model.TargetResult{Outcome: model.OutcomePassed}
	target := func(id string, outcomes ...model.TargetResult) TargetEvidence {
		return TargetEvidence{Target: model.PlanTarget{ID: model.TargetID(id), Target: model.Target{Name: id}}, Outcomes: outcomes}
	}
	evidence := Evidence{Plan: model.Plan{Environments: []model.Environment{tahoeArm, tahoeX86}}, Targets: []TargetEvidence{
		target("installs", failed(model.PhaseInstall), passed),
		target("tests", passed, failed(model.PhaseTest)),
		target("both", failed(model.PhaseChecksum), failed(model.PhaseInstall)),
		target("lints", failed(model.PhaseLint), failed(model.PhaseLint)),
		target("fetches", failed(model.PhaseFetch), passed),
		target("blocked", model.TargetResult{Outcome: model.OutcomeBlocked}, passed),
		target("passes", passed, passed),
	}}
	worthy, skipped := BaselineWorthy(evidence)
	require.Equal(t, []string{"installs", "tests", "both"}, worthy)
	require.Equal(t, []string{"lints", "fetches"}, skipped)
}
