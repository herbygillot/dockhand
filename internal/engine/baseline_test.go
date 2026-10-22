package engine

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/buildenv"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/planning"
	"github.com/herbygillot/dockhand/internal/store"
	"github.com/herbygillot/dockhand/internal/testsupport"
)

// A baseline is planned the way a check is, from the base's own Portfiles
// in each environment: a port that needs Xcode where there are only the
// Command Line Tools is unmet there, not sent there; and it builds the
// ports it names, not the rest of their directories. (The architecture
// review of 2026-09-27, finding 2.)
func TestABaselineKeepsEachEnvironmentsRequirements(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e := f.open(t)
	revision := harborBranch(t, e)
	usePorts(e, xcodeOnArm{fakePorts: harborPorts()})
	arm := tahoeArm
	arm.DeveloperTools = model.DeveloperToolsCommandLine
	intel := tahoeX86
	intel.DeveloperTools = model.DeveloperToolsXcode
	plan, err := e.PlanCheck(t.Context(), PlanRequest{Revision: revision, Environments: []model.Environment{arm, intel}})
	require.NoError(t, err)
	branch, err := e.Branch(t.Context(), revision.Branch)
	require.NoError(t, err)
	e.Providers = map[string]buildenv.Provider{"command": &scriptedProvider{}}
	queued, err := e.Enqueue(t.Context(), branch, plan, model.OriginPerson)
	require.NoError(t, err)
	_, err = e.Drive(t.Context(), session(t, e), queued.ID)
	require.NoError(t, err)

	baseline, err := e.PlanBaseline(t.Context(), branch, []string{"libharbor", "harbor-viewer", "harbor-viewer-legacy"})
	require.NoError(t, err)
	require.Equal(t, revision.Source.Base, baseline.Revision.Source.Commit)
	require.Equal(t, []string{"libharbor:unchanged:also", "harbor-viewer:unchanged:also", "harbor-viewer-legacy:unchanged:also"}, names(baseline.Plan.Targets),
		"not harbor-viewer's other subports, nor the ports it depends on")
	require.True(t, baseline.Plan.NeedsXcodeIn(arm, "libharbor"))
	_, unmet := baseline.Plan.UnmetIn(arm, "libharbor")
	require.True(t, unmet, "unmet where there is no Xcode, not sent there")
	_, unmet = baseline.Plan.UnmetIn(intel, "libharbor")
	require.False(t, unmet)
	_, unmet = baseline.Plan.UnmetIn(arm, "harbor-viewer")
	require.True(t, unmet, "not built against master's libharbor there")
	armPlan, _ := baseline.Plan.In(arm)
	intelPlan, _ := baseline.Plan.In(intel)
	require.Equal(t, []model.Exclusion{{Target: model.Target{Name: "harbor-viewer-legacy", Portfile: "graphics/harbor-viewer/Portfile", Subport: "harbor-viewer-legacy"}, Reason: "supported_archs x86_64 only"}},
		armPlan.Exclusions, "excluded once where it's unsupported, though its directory was named twice")
	require.Empty(t, intelPlan.Exclusions, "and built where it is")
}

// A baseline explains a check, so it builds at the base that check
// started from, even after a rebase has moved the branch's base on. (The
// architecture review of 2026-09-27, finding 2.)
func TestABaselineUsesTheCheckedBase(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e, _ := f.withPreparer(t)
	branch := twoPortBranch(t, e)
	checkHead(t, e, branch)
	checked, err := e.Branch(t.Context(), branch.ID)
	require.NoError(t, err)
	write(t, f.upstream, map[string]string{"devel/other/Portfile": "name other\nversion 1\n"})
	testsupport.Git(t, f.upstream, "add", "devel/other/Portfile")
	testsupport.Git(t, f.upstream, "commit", "-q", "-m", "other: new port")
	_, err = e.Rebase(t.Context(), checked)
	require.NoError(t, err)
	current, err := e.Branch(t.Context(), branch.ID)
	require.NoError(t, err)
	require.NotEqual(t, checked.Base, current.Base, "the rebase moved the branch's base on")

	revisions := func() int {
		t.Helper()
		var found []model.Revision
		require.NoError(t, e.Store.View(t.Context(), e.Repository, func(r store.Reader) error {
			var err error
			found, err = r.Revisions(branch.ID)
			return err
		}))
		return len(found)
	}
	before := revisions()
	preview, err := e.PreviewBaseline(t.Context(), current, []string{"jq"})
	require.NoError(t, err)
	require.Equal(t, checked.Base, preview.Revision.Source.Commit)
	require.NotEmpty(t, preview.Revision.ID)
	require.Equal(t, before, revisions(), "a preview records nothing")

	baseline, err := e.PlanBaseline(t.Context(), current, []string{"jq"})
	require.NoError(t, err)
	require.Equal(t, checked.Base, baseline.Revision.Source.Commit)
	require.Equal(t, checked.Base, baseline.Revision.Source.Base)
	require.Equal(t, before+1, revisions(), "the base is recorded as the branch's revision once planned")
}

// Without --only, a baseline takes the ports that failed at install or
// test somewhere. One that failed only before building, at lint, fetch, or
// checksum, is left out, since master can't speak to it; one blocked or
// unmet didn't fail.
func TestABaselineTakesPortsThatFailedWhileBuilding(t *testing.T) {
	t.Parallel()
	failed := func(phase model.Phase) model.TargetResult {
		return model.TargetResult{Outcome: model.OutcomeFailed, Phase: phase}
	}
	passed := model.TargetResult{Outcome: model.OutcomePassed}
	target := func(id string, outcomes ...model.TargetResult) TargetEvidence {
		return TargetEvidence{Target: model.PlanTarget{ID: model.TargetID(id), Target: model.Target{Name: id}}, Outcomes: cells(outcomes)}
	}
	evidence := Evidence{Plan: model.Plan{Environments: []model.Environment{tahoeArm, tahoeX86}}, Targets: []TargetEvidence{
		target("installs", failed(model.PhaseInstall), passed),
		target("tests", passed, failed(model.PhaseTest)),
		target("both", failed(model.PhaseChecksum), failed(model.PhaseInstall)),
		target("lints", failed(model.PhaseLint), failed(model.PhaseLint)),
		target("fetches", failed(model.PhaseFetch), passed),
		target("blocked", model.TargetResult{Outcome: model.OutcomeBlocked}, passed),
		target("passes", passed, passed),
		target("advisory", passed, model.TargetResult{Outcome: model.OutcomePassed, Tests: model.TestsTimedOut}),
		target("tested", model.TargetResult{Outcome: model.OutcomePassed, Tests: model.TestsPassed}, passed),
		target("dependency", failed(model.PhaseDependencies), passed),
	}}
	worthy, skipped := BaselineWorthy(evidence)
	require.Equal(t, []string{"installs", "tests", "both", "advisory", "dependency"}, worthy, "tests that failed where they only report are worth one too (the libuv run's finding 5)")
	require.Equal(t, []string{"lints", "fetches"}, skipped)
	require.Equal(t, planning.Limited{Environments: []model.Environment{tahoeX86}, Elsewhere: evidence.Run.Name() + " didn't fail there"}, rebuildWhere(evidence, "advisory"), "rebuilt where its tests failed")
}

// A failed check points to a baseline of what it could explain, and only
// the branch's newest check does, the one check --baseline looks into. A
// check's own results are what it explains, as its report shows them: a
// port its --only left out isn't among them, whatever an earlier check of
// the same files found.
func TestOnlyTheNewestFailedCheckPointsToABaseline(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e, _ := f.withPreparer(t)
	branch := twoPortBranch(t, e)
	e.Providers = map[string]buildenv.Provider{"command": &scriptedProvider{outcomes: map[model.TargetID]model.Outcome{"jq": model.OutcomeFailed, "libharbor": model.OutcomeFailed}}}
	check := func(only ...string) model.Run {
		capture, err := e.Capture(t.Context(), CaptureRequest{Branch: branch, Mode: CaptureHead})
		require.NoError(t, err)
		plan, err := e.PlanCheck(t.Context(), PlanRequest{Revision: capture.Revision, Environments: []model.Environment{tahoeArm}, Only: only})
		require.NoError(t, err)
		queued, err := e.Enqueue(t.Context(), branch, plan, model.OriginPerson)
		require.NoError(t, err)
		completed, err := e.Drive(t.Context(), session(t, e), queued.ID)
		require.NoError(t, err)
		require.Equal(t, model.RunFailed, completed.State)
		return completed
	}
	first := check()
	offer, err := e.BaselineCandidates(t.Context(), first)
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"jq", "libharbor"}, offer.Ports)
	require.Equal(t, branch.Base, offer.Base)

	latest := check("libharbor")
	offer, err = e.BaselineCandidates(t.Context(), first)
	require.NoError(t, err)
	require.Empty(t, offer.Ports, "check-1 is no longer the newest")
	offer, err = e.BaselineCandidates(t.Context(), latest)
	require.NoError(t, err)
	require.Equal(t, []string{"libharbor"}, offer.Ports)

	baseline, err := e.PlanBaseline(t.Context(), branch, nil)
	require.NoError(t, err)
	require.Equal(t, latest.ID, baseline.Of.ID)
	require.Equal(t, []string{"libharbor:unchanged:also"}, names(baseline.Plan.Targets))
	_, err = e.PlanBaseline(t.Context(), branch, []string{"jq"})
	require.ErrorContains(t, err, "--only jq: "+latest.Name()+" did not build it")
}

// failsOn fails one target at install in one environment, and passes
// everything else.
type failsOn struct {
	environment model.Environment
	target      model.TargetID
}

func (failsOn) Name() string { return "command" }

func (p failsOn) Execute(_ context.Context, job buildenv.Job, build buildenv.Build) error {
	for _, target := range job.Targets {
		result := model.TargetResult{Target: target.ID, Outcome: model.OutcomePassed, Tests: model.TestsNone}
		if target.ID == p.target && job.Environment == p.environment {
			result.Outcome, result.Phase = model.OutcomeFailed, model.PhaseInstall
		}
		if err := build.Record(result); err != nil {
			return err
		}
	}
	return nil
}

// A baseline rebuilds a port only in the environments where it failed; a
// port named that failed nowhere, in every one the check built it in.
// (Roadmap item 2.)
func TestABaselineRebuildsAPortOnlyWhereItFailed(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e, _ := f.withPreparer(t)
	branch := twoPortBranch(t, e)
	e.Providers = map[string]buildenv.Provider{"command": failsOn{environment: tahoeX86, target: "jq"}}
	capture, err := e.Capture(t.Context(), CaptureRequest{Branch: branch, Mode: CaptureHead})
	require.NoError(t, err)
	plan, err := e.PlanCheck(t.Context(), PlanRequest{Revision: capture.Revision, Environments: []model.Environment{tahoeArm, tahoeX86}})
	require.NoError(t, err)
	queued, err := e.Enqueue(t.Context(), branch, plan, model.OriginPerson)
	require.NoError(t, err)
	checked, err := e.Drive(t.Context(), session(t, e), queued.ID)
	require.NoError(t, err)
	require.Equal(t, model.RunFailed, checked.State)

	baseline, err := e.PlanBaseline(t.Context(), branch, nil)
	require.NoError(t, err)
	require.Equal(t, []string{"jq:unchanged:also"}, names(baseline.Plan.Targets))
	armPlan, _ := baseline.Plan.In(tahoeArm)
	x86Plan, _ := baseline.Plan.In(tahoeX86)
	require.Empty(t, armPlan.Order)
	require.Equal(t, []model.Exclusion{{Target: model.Target{Name: "jq", Portfile: "textproc/jq/Portfile"}, Reason: checked.Name() + " didn't fail there"}}, armPlan.Exclusions)
	require.Equal(t, []model.TargetID{"jq"}, x86Plan.Order)

	named, err := e.PlanBaseline(t.Context(), branch, []string{"libharbor"})
	require.NoError(t, err)
	for _, environment := range []model.Environment{tahoeArm, tahoeX86} {
		planned, _ := named.Plan.In(environment)
		require.Equal(t, []model.TargetID{"libharbor"}, planned.Order, "it failed nowhere, so everywhere the check built it")
	}
}

// A port whose only failures are its tests', where its change record says
// the branch declares tests master doesn't, is no baseline's to compare:
// exiv2's check offered one (the exiv2 field notes, 2026-10-19). A failed
// install still is, and so are tests master runs too.
func TestTestsNewOnTheBranchAreNoBaselines(t *testing.T) {
	t.Parallel()
	target := model.PlanTarget{Target: model.Target{Name: "exiv2", Portfile: "graphics/exiv2/Portfile"}, Directory: "graphics/exiv2"}
	newTests := map[string]model.ChangeRecord{"graphics/exiv2": {Ports: []model.SubportChange{{Port: "exiv2", Kind: model.SubportChanged,
		Fields: []model.FieldChange{{Field: "version", From: "0.27.7", To: "0.28.9"}, {Field: "dockhand.test_run", From: "no", To: "yes"}}}}}}
	require.True(t, testsDeclaredHere(newTests, target))
	alike := map[string]model.ChangeRecord{"graphics/exiv2": {Ports: []model.SubportChange{{Port: "exiv2", Kind: model.SubportChanged,
		Fields: []model.FieldChange{{Field: "version", From: "0.27.7", To: "0.28.9"}}}}}}
	require.False(t, testsDeclaredHere(alike, target), "master runs them too")
	require.False(t, testsDeclaredHere(nil, target), "no record says")

	cell := func(result model.TargetResult) Cell { return Cell{TargetResult: result} }
	testsFailed := TargetEvidence{Outcomes: []Cell{cell(model.TargetResult{Outcome: model.OutcomePassed, Tests: model.TestsFailed}), cell(model.TargetResult{Outcome: model.OutcomeFailed, Phase: model.PhaseTest})}}
	require.True(t, onlyTestsFailed(testsFailed))
	installFailed := TargetEvidence{Outcomes: []Cell{cell(model.TargetResult{Outcome: model.OutcomePassed, Tests: model.TestsFailed}), cell(model.TargetResult{Outcome: model.OutcomeFailed, Phase: model.PhaseInstall})}}
	require.False(t, onlyTestsFailed(installFailed))
	require.Equal(t, "exiv2's tests are new on this branch, and master declares none, so a baseline has nothing to compare", NewTestsWords([]string{"exiv2"}))
}

// namedGraph reads the port index's graph and the ports each directory
// defines.
type namedGraph struct {
	graphPorts
	defined map[string][]string
}

func (n namedGraph) PortsDefined(_ context.Context, _ model.Source, directories []string) (map[string][]string, error) {
	found := map[string][]string{}
	for _, directory := range directories {
		found[directory] = n.defined[directory]
	}
	return found, nil
}

// A port whose build reads only what master has, its own directory,
// _resources, and its dependency closure, is master's build already, and
// a baseline can tell nothing of it: gnome-voice's check-484, built --also
// unchanged, offered one (field testing, 2026-10-20). An --also dependent
// of a port the branch changes builds against the change, and keeps its
// offer; so does the changed port itself, and every port where
// _resources changes.
func TestABaselineIsOfferedOnlyWhereTheBuildReadsTheBranch(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e, _ := f.withPreparer(t)
	branch := twoPortBranch(t, e)
	capture, err := e.Capture(t.Context(), CaptureRequest{Branch: branch, Mode: CaptureHead})
	require.NoError(t, err)
	usePorts(e, namedGraph{
		graphPorts: graphPorts{graph: map[string][]string{"harbor-cli": {"zlib"}, "zlib": {"libharbor"}, "gnome-voice": {"gtk3"}, "gtk3": {"glib2"}}},
		defined:    map[string][]string{"textproc/jq": {"jq"}, "devel/libharbor": {"libharbor"}},
	})
	target := func(id, directory string) TargetEvidence {
		return TargetEvidence{Target: model.PlanTarget{ID: model.TargetID(id), Directory: directory, Target: model.Target{Name: id}}}
	}
	evidence := Evidence{Targets: []TargetEvidence{target("jq", "textproc/jq"), target("harbor-cli", "devel/harbor-cli"), target("gnome-voice", "audio/gnome-voice")}}
	kept, err := e.readsChanged(t.Context(), capture.Revision, evidence, []string{"jq", "harbor-cli", "gnome-voice"})
	require.NoError(t, err)
	require.Equal(t, []string{"jq", "harbor-cli"}, kept, "gnome-voice reads nothing the branch changes; harbor-cli reads libharbor through zlib")

	// With no graph to read, every port is kept.
	usePorts(e, namedGraph{defined: map[string][]string{"textproc/jq": {"jq"}, "devel/libharbor": {"libharbor"}}})
	kept, err = e.readsChanged(t.Context(), capture.Revision, evidence, []string{"jq", "harbor-cli", "gnome-voice"})
	require.NoError(t, err)
	require.Equal(t, []string{"jq", "harbor-cli", "gnome-voice"}, kept)

	// Where _resources changes, every port's build reads it.
	write(t, branch.Worktree, map[string]string{"_resources/port1.0/group/harbor-1.0.tcl": "# harbor\n"})
	testsupport.Git(t, branch.Worktree, "add", "_resources/port1.0/group/harbor-1.0.tcl")
	testsupport.Git(t, branch.Worktree, "commit", "-q", "-m", "harbor 1.0: new PortGroup")
	capture, err = e.Capture(t.Context(), CaptureRequest{Branch: branch, Mode: CaptureHead})
	require.NoError(t, err)
	usePorts(e, namedGraph{graphPorts: graphPorts{graph: map[string][]string{}}, defined: map[string][]string{}})
	kept, err = e.readsChanged(t.Context(), capture.Revision, evidence, []string{"gnome-voice"})
	require.NoError(t, err)
	require.Equal(t, []string{"gnome-voice"}, kept)
}

// check --baseline without --only leaves out what the offer does, a port
// whose build reads only what master has, and refuses where that's all,
// with the same words; --only still builds it (check-484's gnome-voice,
// built --also unchanged).
func TestABaselineLeavesOutWhatMastersBuildReadsTheSame(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e, _ := f.withPreparer(t)
	branch, err := e.Start(t.Context(), StartRequest{Name: "libharbor-only", Here: true})
	require.NoError(t, err)
	write(t, branch.Worktree, map[string]string{"devel/libharbor/Portfile": "name libharbor\nversion 3\n"})
	testsupport.Git(t, branch.Worktree, "add", "devel/libharbor/Portfile")
	testsupport.Git(t, branch.Worktree, "commit", "-q", "-m", "libharbor: update to 3")
	usePorts(e, namedGraph{
		graphPorts: graphPorts{
			fakePorts: fakePorts{directories: map[string][]macports.PortInfo{"textproc/jq": {port("jq")}, "devel/libharbor": {port("libharbor")}}},
			graph:     map[string][]string{"jq": {"oniguruma"}},
		},
		defined: map[string][]string{"devel/libharbor": {"libharbor"}},
	})
	e.Providers = map[string]buildenv.Provider{"command": &scriptedProvider{outcomes: map[model.TargetID]model.Outcome{"jq": model.OutcomeFailed}}}
	capture, err := e.Capture(t.Context(), CaptureRequest{Branch: branch, Mode: CaptureHead})
	require.NoError(t, err)
	plan, err := e.PlanCheck(t.Context(), PlanRequest{Revision: capture.Revision, Environments: []model.Environment{tahoeArm}, Also: []string{"jq"}})
	require.NoError(t, err)
	queued, err := e.Enqueue(t.Context(), branch, plan, model.OriginPerson)
	require.NoError(t, err)
	run, err := e.Drive(t.Context(), session(t, e), queued.ID)
	require.NoError(t, err)
	require.Equal(t, model.RunFailed, run.State)

	offer, err := e.BaselineCandidates(t.Context(), run)
	require.NoError(t, err)
	require.Empty(t, offer.Ports, "jq reads nothing the branch changes")
	_, err = e.PlanBaseline(t.Context(), branch, nil)
	require.EqualError(t, err, "jq left out: master's build reads what "+run.Name()+"'s did")
	baseline, err := e.PlanBaseline(t.Context(), branch, []string{"jq"})
	require.NoError(t, err)
	require.Equal(t, []string{"jq:unchanged:also"}, names(baseline.Plan.Targets), "--only builds it whatever the rule says")
}
