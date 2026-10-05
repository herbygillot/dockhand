package command

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/engine"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/testsupport"
)

// onePort stands in for MacPorts: every directory defines one port, named
// after it.
type onePort struct{}

func (onePort) Ports(_ context.Context, _ model.Source, directory string, _ model.Environment, variants map[string]bool) ([]macports.PortInfo, error) {
	return []macports.PortInfo{{Name: filepath.Base(directory), Options: map[string]string{}}}, nil
}

func (onePort) Directory(context.Context, model.Source, string) (string, error) {
	return "", errors.New("no such port")
}

// withScript configures a command provider that reports jq with the given
// outcome.
func withScript(t *testing.T, w world, outcome string) {
	t.Helper()
	script := filepath.Join(w.home, "bin", "build-ports")
	require.NoError(t, os.MkdirAll(filepath.Dir(script), 0o755))
	phase := ""
	if outcome == "failed" {
		phase = `, "phase": "install"`
	}
	require.NoError(t, os.WriteFile(script, []byte(`#!/bin/sh
echo "building from $1"
cat > "$(dirname "$1")/result.json" <<JSON
{"version": 1, "targets": [{"id": "jq", "outcome": "`+outcome+`"`+phase+`}]}
JSON
`), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(w.home, ".dockhand"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(w.home, ".dockhand", "config.toml"), []byte("[providers.command]\nrun = \"~/bin/build-ports\"\nname = \"my build box\"\n"), 0o644))
	testPortReader = onePort{}
	t.Cleanup(func() { testPortReader = nil })
}

func TestCheckRunsHereWithoutServe(t *testing.T) {
	w := newWorld(t)
	versioned(t, w)
	withBumper(t)
	_, _, err := dockhand(t, "start", "jq-update")
	require.NoError(t, err)
	t.Setenv("MACPORTS_TREE", filepath.Join(w.home, "Source", "macports-branches", "jq-update"))
	_, _, err = dockhand(t, "update", "jq")
	require.NoError(t, err)

	_, _, err = dockhand(t, "check")
	require.ErrorContains(t, err, "[providers.command]")

	withScript(t, w, "passed")
	out, _, err := dockhand(t, "check", "--plan")
	require.NoError(t, err)
	require.Equal(t, "jq-update · would capture the working files as a new snapshot\nChanged     jq\nProvider    command · tests declared\n", out)
	_, _, err = dockhand(t, "check", "--on", "tart:tahoe")
	require.ErrorContains(t, err, "Tart isn't installed here")

	out, errs, err := dockhand(t, "check")
	require.NoError(t, err)
	require.Contains(t, errs, "check-1 runs here, since no dockhand serve is running.")
	require.Contains(t, errs, "command: jq passed")
	require.Contains(t, out, "jq-update · captured working files as snapshot 1\n", "the plan recorded nothing for the check to reuse")
	require.Contains(t, out, "  jq  ✓\n\nPassed for snapshot 1.\nNext: dockhand tidy --branch jq-update\n", "what moves the branch on, as status says it")
	out, _, err = dockhand(t, "check", "--plan")
	require.NoError(t, err)
	require.Contains(t, out, "jq-update · checking snapshot 1\n", "a plan finds a snapshot already recorded of the same files")

	out, _, err = dockhand(t, "check", "-d")
	require.NoError(t, err)
	require.Contains(t, out, "check-2 queued; nothing is running it: dockhand serve\n")

	withScript(t, w, "failed")
	_, _, err = dockhand(t, "check")
	require.ErrorContains(t, err, "check-2 is already queued for these files; dockhand wait check-2 follows it", "one check of a branch at a time")
	out, _, err = dockhand(t, "check", "--replace")
	require.Contains(t, out, "Canceled check-2 before it started.\ncheck-3 replaces check-2.\n", "it was only queued (the hugo exercise's certigo run, finding 5)")
	require.Equal(t, 2, ExitCode(err), "a failed check exits 2")
	require.ErrorContains(t, err, "check-3 failed for snapshot 1: jq did not pass. Logs: dockhand logs check-3")
	require.Contains(t, out, "  jq  ✗ failed at install\n")
	require.Contains(t, out, "jq failed at install; the last it printed:\n    building from ", "what no reading of its log explained (the Vx port's field testing)")
}

// A narrowed plan says what --only left out, and that submit still needs
// it checked (Design v3 §7).
func TestCheckPlanNamesWhatOnlyLeftOut(t *testing.T) {
	var out bytes.Buffer
	writePlan(&out, model.Plan{
		Environments: []model.Environment{{Provider: "command"}},
		Targets:      []model.PlanTarget{{ID: "jq", Target: model.Target{Name: "jq"}, Kind: model.Substantive, Role: model.Changed}},
		Omitted:      []model.PlanTarget{{ID: "libharbor", Target: model.Target{Name: "libharbor"}, Kind: model.Substantive, Role: model.Changed}},
		Tests:        model.TestsDeclared,
	}, nil, nil)
	require.Contains(t, out.String(), "Changed     jq\nLeft out    libharbor, by --only; submit still needs them checked\n")
}

// A plan says what each Git-fetched target's build must fetch: the commit
// its tag names as the check is planned, once where its environments
// agree, or why that isn't known (batch 20).
func TestCheckPlanSaysWhatAGitFetchMustCheckOut(t *testing.T) {
	arm := model.Environment{Provider: "tart", Platform: model.Platform{OS: "darwin", Version: "25", Architecture: "arm64"}}
	intel := model.Environment{Provider: "tart", Platform: model.Platform{OS: "darwin", Version: "25", Architecture: "x86_64"}}
	target := func(name string) model.PlanTarget {
		return model.PlanTarget{ID: model.TargetID(name), Target: model.Target{Name: name}, Kind: model.Substantive, Role: model.Changed}
	}
	commit := model.ObjectID("1a2b3c4d5e6f1a2b3c4d5e6f1a2b3c4d5e6f1a2b")
	tag := model.GitSource{URL: "https://github.com/harbor/libharbor.git", Ref: "v4", Commit: commit, ResolvedAt: time.Now()}
	cli := model.GitSource{URL: "https://github.com/harbor/cli.git", Ref: "v2", Unresolved: "its refs couldn't be read: timed out", ResolvedAt: time.Now()}
	plan := model.Plan{Environments: []model.Environment{arm, intel}, Tests: model.TestsDeclared,
		Targets: []model.PlanTarget{target("libharbor"), target("harbor-cli")},
		Builds: []model.EnvironmentPlan{
			{Environment: arm, Order: []model.TargetID{"libharbor", "harbor-cli"}, Git: map[model.TargetID]model.GitSource{"libharbor": tag, "harbor-cli": cli}},
			{Environment: intel, Order: []model.TargetID{"libharbor", "harbor-cli"}, Git: map[model.TargetID]model.GitSource{"libharbor": tag}},
		}}
	var out bytes.Buffer
	writePlan(&out, plan, nil, nil)
	require.Contains(t, out.String(), "Git         libharbor: git.branch v4 names 1a2b3c4 now, which its build must fetch\n"+
		"            harbor-cli: which commit git.branch v2 names isn't known (its refs couldn't be read: timed out); its build records what it fetches, and stands for no later check\n")

	view := planView(plan)
	require.Equal(t, gitSourceJSON{URL: tag.URL, Branch: "v4", Commit: string(commit), ResolvedAt: tag.ResolvedAt}, view.Builds[1].Git["libharbor"])
	require.NotContains(t, view.Builds[1].Git, "harbor-cli", "fetched otherwise there")
	require.Nil(t, view.Builds[0].OmittedGit, "nothing left out")
}

// A Git-fetched target --only left out has the commit its tag names
// expected of it too (planning.OmittedSources), and the plan shows it,
// marked as left out, in its text and its JSON apart from what it builds:
// the check doesn't build it, but an earlier check's result of it stands
// only where it fetched that commit.
func TestCheckPlanShowsTheGitSourcesOfWhatOnlyLeftOut(t *testing.T) {
	arm := model.Environment{Provider: "tart", Platform: model.Platform{OS: "darwin", Version: "25", Architecture: "arm64"}}
	target := func(name string) model.PlanTarget {
		return model.PlanTarget{ID: model.TargetID(name), Target: model.Target{Name: name}, Kind: model.Substantive, Role: model.Changed}
	}
	commit := model.ObjectID("1a2b3c4d5e6f1a2b3c4d5e6f1a2b3c4d5e6f1a2b")
	tag := model.GitSource{URL: "https://github.com/harbor/libharbor.git", Ref: "v4", Commit: commit, ResolvedAt: time.Now()}
	viewer := model.GitSource{URL: "https://github.com/harbor/viewer.git", Ref: "v1", Unresolved: "the repository has no branch or tag v1", ResolvedAt: time.Now()}
	cli := model.GitSource{URL: "https://github.com/harbor/cli.git", Ref: "v2", Commit: "9f8e7d6c5b4a9f8e7d6c5b4a9f8e7d6c5b4a9f8e", ResolvedAt: time.Now()}
	plan := model.Plan{Environments: []model.Environment{arm}, Tests: model.TestsDeclared, Only: []string{"harbor-cli"},
		Targets: []model.PlanTarget{target("harbor-cli")},
		Omitted: []model.PlanTarget{target("libharbor"), target("harbor-viewer")},
		Builds: []model.EnvironmentPlan{{Environment: arm, Order: []model.TargetID{"harbor-cli"},
			Git: map[model.TargetID]model.GitSource{"harbor-cli": cli, "libharbor": tag, "harbor-viewer": viewer}}}}
	var out bytes.Buffer
	writePlan(&out, plan, nil, nil)
	require.Contains(t, out.String(), "Git         harbor-cli: git.branch v2 names 9f8e7d6 now, which its build must fetch\n"+
		"            libharbor (left out): git.branch v4 names 1a2b3c4 now, which an earlier check's build of it must have fetched to stand\n"+
		"            harbor-viewer (left out): which commit git.branch v1 names isn't known (the repository has no branch or tag v1); no earlier check's result of it stands\n")

	view := planView(plan)
	require.Equal(t, map[string]gitSourceJSON{"harbor-cli": gitSourceView(cli)}, view.Builds[0].Git, "what it builds")
	require.Equal(t, map[string]gitSourceJSON{"libharbor": gitSourceView(tag), "harbor-viewer": gitSourceView(viewer)}, view.Builds[0].OmittedGit, "what it left out, apart")
}

// On several environments the results are a grid, a column each headed
// by its release; on one, a line each, the environment named only where
// asked.
func TestResultsOnSeveralReleasesAreAGrid(t *testing.T) {
	tahoe := model.Environment{Provider: "tart", Platform: model.Platform{OS: "darwin", Version: "25", Architecture: "arm64"}}
	sequoia := model.Environment{Provider: "tart", Platform: model.Platform{OS: "darwin", Version: "24", Architecture: "arm64"}}
	targets := []model.PlanTarget{{ID: "flatbuffers", Target: model.Target{Name: "flatbuffers"}}, {ID: "libsigmf", Target: model.Target{Name: "libsigmf"}}}
	passed := model.TargetResult{Outcome: model.OutcomePassed}
	failed := model.TargetResult{Outcome: model.OutcomeFailed, Phase: model.PhaseInstall}
	evidence := engine.Evidence{Plan: model.Plan{Environments: []model.Environment{tahoe, sequoia}, Targets: targets}, Targets: []engine.TargetEvidence{
		{Target: targets[0], Outcomes: cells([]model.TargetResult{passed, passed})},
		{Target: targets[1], Outcomes: cells([]model.TargetResult{passed, failed})},
	}}
	var out bytes.Buffer
	writeResults(&out, "  ", evidence, false)
	require.Equal(t, "  PORT          macOS 26   macOS 15\n"+
		"  flatbuffers   ✓          ✓\n"+
		"  libsigmf      ✓          ✗ failed at install\n", out.String())

	evidence.Plan.Environments = []model.Environment{tahoe}
	evidence.Targets = evidence.Targets[:1]
	evidence.Targets[0].Outcomes = cells([]model.TargetResult{passed})
	out.Reset()
	writeResults(&out, "  ", evidence, false)
	require.Equal(t, "  flatbuffers  ✓\n", out.String())
	out.Reset()
	writeResults(&out, "  ", evidence, true)
	require.Equal(t, "  flatbuffers  tart macOS 26 (Tahoe) arm64 ✓\n", out.String())
}

// Where environments build in different orders, the plan shows each one's;
// a port excluded everywhere for one reason shows once, and one excluded
// in some environments names them.
func TestCheckPlanShowsEachEnvironmentsOrder(t *testing.T) {
	arm := model.Environment{Provider: "tart", Platform: model.Platform{OS: "darwin", Version: "25", Architecture: "arm64"}}
	intel := model.Environment{Provider: "tart", Platform: model.Platform{OS: "darwin", Version: "25", Architecture: "x86_64"}}
	target := func(name string) model.PlanTarget {
		return model.PlanTarget{ID: model.TargetID(name), Target: model.Target{Name: name}, Kind: model.Substantive, Role: model.Changed}
	}
	old := model.Exclusion{Target: model.Target{Name: "harbor-cli-old"}, Reason: "replaced by harbor-cli"}
	plan := model.Plan{Environments: []model.Environment{arm, intel}, Tests: model.TestsDeclared,
		Targets: []model.PlanTarget{target("harbor-cli"), target("libharbor"), target("harbor-intel")},
		Builds: []model.EnvironmentPlan{
			{Environment: arm, Order: []model.TargetID{"harbor-cli", "libharbor"}, Dependencies: map[model.TargetID][]model.TargetID{"libharbor": {"harbor-cli"}},
				Exclusions: []model.Exclusion{old, {Target: model.Target{Name: "harbor-intel"}, Reason: "not defined there"}}},
			{Environment: intel, Order: []model.TargetID{"libharbor", "harbor-cli", "harbor-intel"}, Dependencies: map[model.TargetID][]model.TargetID{"harbor-cli": {"libharbor"}},
				Exclusions: []model.Exclusion{old}},
		}}
	var out bytes.Buffer
	writePlan(&out, plan, nil, nil)
	require.Contains(t, out.String(), "Order       on tart macOS 26 (Tahoe) arm64: harbor-cli → libharbor\n"+
		"            on tart macOS 26 (Tahoe) x86_64: libharbor → harbor-cli → harbor-intel\n"+
		"Excluded    harbor-cli-old: replaced by harbor-cli\n"+
		"Excluded    harbor-intel on tart macOS 26 (Tahoe) arm64: not defined there\n")

	plan.Builds[0].Order, plan.Builds[0].Dependencies = []model.TargetID{"libharbor", "harbor-cli"}, map[model.TargetID][]model.TargetID{"harbor-cli": {"libharbor"}}
	plan.Targets = []model.PlanTarget{target("libharbor"), target("harbor-cli"), target("harbor-intel")}
	out.Reset()
	writePlan(&out, plan, nil, nil)
	require.Contains(t, out.String(), "Order       libharbor → harbor-cli → harbor-intel\n", "where they agree, one line")
}

// A baseline's report sets each port beside the branch's result where the
// baseline rebuilt it, and says why where the check failed it and master
// doesn't build it; elsewhere it says nothing.
// A baseline run for tests that failed where the policy only reports them
// says what the tests did at the base, not only that the port built: uvw's
// failed there too, and uvw2's passed (the libuv run's finding 5).
func TestABaselineSaysWhatTheTestsDidAtTheBase(t *testing.T) {
	built := func(tests model.TestOutcome) model.TargetResult {
		return model.TargetResult{Outcome: model.OutcomePassed, Tests: tests}
	}
	for _, test := range []struct {
		base, branch model.TestOutcome
		words        string
	}{
		{model.TestsFailed, model.TestsFailed, "✓ builds at the base, as it does on the branch; its tests fail at the base too, as in check-38, so they did before this branch."},
		{model.TestsPassed, model.TestsTimedOut, "✓ builds at the base, as it does on the branch; its tests pass at the base, and time out in check-38; the cause isn't established."},
		{model.TestsSkipped, model.TestsFailed, "✓ builds at the base, as it does on the branch; its tests fail in check-38, and weren't run at the base (skipped), so there's nothing to set beside them."},
		{model.TestsFailed, model.TestsPassed, "✓ builds at the base, as it does on the branch; its tests fail at the base, and pass in check-38."},
		{model.TestsTimedOut, model.TestsNone, "✓ builds at the base, as it does on the branch; its tests time out at the base, and weren't run in check-38 (none)."},
		{model.TestsPassed, model.TestsPassed, "✓ builds at the base, as it does on the branch."},
	} {
		require.Equal(t, test.words, baselineWords(built(test.base), built(test.branch), "check-38"), "%s at the base, %s on the branch", test.base, test.branch)
	}
}

func TestBaselineResultsShowWhereItRebuilt(t *testing.T) {
	tahoe := model.Environment{Provider: "tart", Platform: model.Platform{OS: "darwin", Version: "25", Architecture: "arm64"}}
	sequoia := model.Environment{Provider: "tart", Platform: model.Platform{OS: "darwin", Version: "24", Architecture: "arm64"}}
	jq := model.PlanTarget{ID: "jq", Target: model.Target{Name: "jq"}}
	gone := model.PlanTarget{ID: "gone", Target: model.Target{Name: "gone"}}
	passed := model.TargetResult{Outcome: model.OutcomePassed}
	failed := model.TargetResult{Outcome: model.OutcomeFailed, Phase: model.PhaseInstall}
	notRun := model.TargetResult{Outcome: model.OutcomeNotRun}
	branch := engine.Evidence{Run: model.Run{Number: 4}, Plan: model.Plan{Environments: []model.Environment{tahoe, sequoia}}, Targets: []engine.TargetEvidence{
		{Target: jq, Outcomes: cells([]model.TargetResult{passed, failed})},
		{Target: gone, Outcomes: cells([]model.TargetResult{failed, passed})},
	}}
	base := engine.Evidence{Plan: model.Plan{Environments: []model.Environment{tahoe, sequoia}, Builds: []model.EnvironmentPlan{
		{Environment: tahoe, Exclusions: []model.Exclusion{{Target: jq.Target, Reason: "check-4 didn't fail it there"}, {Target: gone.Target, Reason: "replaced by other"}}},
		{Environment: sequoia, Order: []model.TargetID{"jq"}, Exclusions: []model.Exclusion{{Target: gone.Target, Reason: "check-4 didn't fail it there"}}},
	}}, Targets: []engine.TargetEvidence{
		{Target: jq, Outcomes: cells([]model.TargetResult{notRun, failed})},
		{Target: gone, Outcomes: cells([]model.TargetResult{notRun, notRun})},
	}}
	var out bytes.Buffer
	writeBaselineResults(&out, base, branch, "1a2b3c4")
	require.Equal(t, "jq at master 1a2b3c4 · tart macOS 15 (Sequoia) arm64\n"+
		"  ✗ fails at the base too, at install. Both results are kept; the cause isn't established.\n"+
		"gone at master 1a2b3c4 · tart macOS 26 (Tahoe) arm64\n"+
		"  · not built at the base: replaced by other\n", out.String())
}

// A misspelled --tests is refused before anything is captured or planned.
func TestCheckRefusesAnUnknownTestPolicyAtOnce(t *testing.T) {
	w := newWorld(t)
	versioned(t, w)
	withBumper(t)
	withScript(t, w, "passed")
	_, _, err := dockhand(t, "start", "jq-update")
	require.NoError(t, err)
	t.Setenv("MACPORTS_TREE", filepath.Join(w.home, "Source", "macports-branches", "jq-update"))
	_, _, err = dockhand(t, "update", "jq")
	require.NoError(t, err)

	_, _, err = dockhand(t, "check", "--tests", "requried")
	require.EqualError(t, err, `--tests "requried" is not declared, required, or skip`)
	out, _, err := dockhand(t, "status")
	require.NoError(t, err)
	require.NotContains(t, out, "check-1", "nothing was queued")
}

// What stopping a check says follows what it had done: one only queued was
// cancelled before it started, and one that recorded nothing finished
// nothing (the hugo exercise's certigo run, finding 5).
func TestAStoppedCheckSaysWhatItLeft(t *testing.T) {
	run := model.Run{ID: "run_2", Number: 2}
	require.Equal(t, "Canceled check-2 before it started.", stopWords(run, true))
	require.Equal(t, "Stopped check-2; what it finished is kept.", stopWords(run, false))
	require.EqualError(t, stoppedExit(run, engine.Evidence{Run: run}), "check-2 stopped before anything finished")
	recorded := engine.Evidence{Run: run, Executions: map[model.ExecutionID]model.GuestExecution{"tart_2": {ID: "tart_2", Run: "run_2"}}}
	require.EqualError(t, stoppedExit(run, recorded), "check-2 stopped; finished results are kept")
}

// A plan requiring tests names the ports that declare none, which pass
// under any policy, and beside them those that do; a plan that doesn't
// require them needn't.
func TestAPlanRequiringTestsNamesWhatDeclaresNone(t *testing.T) {
	tahoe := model.Environment{Provider: "tart", Platform: model.Platform{OS: "darwin", Version: "25", Architecture: "arm64"}}
	plan := model.Plan{Tests: model.TestsRequired, Environments: []model.Environment{tahoe},
		Targets: []model.PlanTarget{{ID: "ov", Target: model.Target{Name: "ov"}, Kind: model.Substantive, Role: model.Changed}, {ID: "jq", Target: model.Target{Name: "jq"}, Kind: model.Substantive, Role: model.Changed}},
		Builds:  []model.EnvironmentPlan{{Environment: tahoe, Order: []model.TargetID{"ov", "jq"}, Untested: []model.TargetID{"ov"}}}}
	var out bytes.Buffer
	writePlan(&out, plan, nil, nil)
	require.Contains(t, out.String(), "Tests       jq, which must pass\nNo tests    ov declares none, so requiring them asks nothing of it\n", "which do is said beside which don't")
	plan.Builds[0].Untested = []model.TargetID{"ov", "jq"}
	out.Reset()
	writePlan(&out, plan, nil, nil)
	require.Contains(t, out.String(), "No tests    ov, jq declare none, so requiring them asks nothing of them\n")
	require.NotContains(t, out.String(), "Tests       ")
	plan.Tests = model.TestsDeclared
	out.Reset()
	writePlan(&out, plan, nil, nil)
	require.NotContains(t, out.String(), "No tests")
}

// cells are hand-built results as the evidence's cells: recorded, but for
// a result not run, whose cell is of that kind.
func cells(results []model.TargetResult) []engine.Cell {
	var cells []engine.Cell
	for _, result := range results {
		kind := engine.CellRecorded
		if result.Outcome == model.OutcomeNotRun {
			kind = engine.CellNotRun
		}
		cells = append(cells, engine.Cell{TargetResult: result, Kind: kind})
	}
	return cells
}

// A --variants plan says what it builds, and --variants each asks before
// more builds than it would make unasked: on a terminal, a question;
// without one, --yes (item 8).
func TestAVariantsCheckSaysWhatItBuildsAndAsksFirst(t *testing.T) {
	arm := model.Environment{Provider: "tart", Platform: model.Platform{OS: "darwin", Version: "25", Architecture: "arm64"}}
	jq := model.PlanTarget{ID: "jq", Target: model.Target{Name: "jq"}, Directory: "textproc/jq", Kind: model.Substantive, Role: model.Changed}
	plan := model.Plan{Environments: []model.Environment{arm}, EachVariant: true, Targets: []model.PlanTarget{jq}}
	order := []model.TargetID{"jq"}
	for _, variant := range []string{"tests", "docs", "oniguruma", "static"} {
		build := jq
		build.Target.Variants = map[string]bool{variant: true}
		build.ID = build.Target.ID()
		plan.Targets = append(plan.Targets, build)
		order = append(order, build.ID)
	}
	plan.Builds = []model.EnvironmentPlan{{Environment: arm, Order: order}}
	var out strings.Builder
	writeVariants(&out, plan)
	require.Equal(t, "Variants    jq with its defaults, then with each of +tests, +docs, +oniguruma, +static over them (universal left out)\n", out.String())

	require.NoError(t, confirmVariantBuilds(Streams{}, plan, false), "five builds, asked nothing")
	for range 3 {
		plan.Builds = append(plan.Builds, model.EnvironmentPlan{Environment: arm, Order: order})
	}
	require.EqualError(t, confirmVariantBuilds(Streams{In: strings.NewReader("")}, plan, false),
		"nothing was checked: --variants each makes 20 builds here; without a terminal, --yes builds them")
	require.NoError(t, confirmVariantBuilds(Streams{}, plan, true))
	var asked strings.Builder
	require.NoError(t, confirmVariantBuilds(Streams{In: strings.NewReader("y\n"), Out: &asked, Err: &asked, interactive: true}, plan, false))
	require.EqualError(t, confirmVariantBuilds(Streams{In: strings.NewReader("n\n"), Out: &asked, Err: &asked, interactive: true}, plan, false), "nothing was checked")
	require.Contains(t, asked.String(), "? --variants each makes 20 builds, each a whole build; go ahead? [y/N] ")

	single := model.Plan{Variants: "+tests", Targets: []model.PlanTarget{plan.Targets[1]}}
	out.Reset()
	writeVariants(&out, single)
	require.Equal(t, "Variants    jq +tests, in place of its defaults\n", out.String())
	require.NoError(t, confirmVariantBuilds(Streams{}, single, false), "only each asks")
}

// check --branch from another checkout takes a branch's working files
// where it has no commits, since its head is only its base; with commits
// and edits beside them, which is meant is asked (the txt run's finding 2).
func TestCheckingABranchWithNoCommitsTakesItsWorkingFiles(t *testing.T) {
	w := checkedBranch(t)
	dir := filepath.Join(w.home, "Source", "macports-branches", "jq-update")
	t.Setenv("MACPORTS_TREE", w.clone)
	out, _, err := dockhand(t, "check", "--plan", "--branch", "jq-update")
	require.NoError(t, err)
	require.Contains(t, out, "jq-update · would capture the working files as a new snapshot\n")

	testsupport.Git(t, dir, "commit", "-q", "-am", "jq: update to 1.8.1")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "textproc/jq/Portfile"), []byte("name jq\nversion 1.8.1\nrevision 1\n"), 0o644))
	_, _, err = dockhand(t, "check", "--plan", "--branch", "jq-update")
	require.ErrorContains(t, err, "jq-update's worktree has edits (textproc/jq/Portfile); choose --head for the committed tip or --working-tree for the files")
}

// A branch whose only port is a new one's untracked Portfile, built with
// check --include, names it in status, not "none yet" (Codex's feedback,
// approved 2026-10-05).
func TestStatusNamesAPortBuiltWithInclude(t *testing.T) {
	w := newWorld(t)
	versioned(t, w)
	withScript(t, w, "passed")
	_, _, err := dockhand(t, "start", "vx")
	require.NoError(t, err)
	dir := filepath.Join(w.home, "Source", "macports-branches", "vx")
	t.Setenv("MACPORTS_TREE", dir)
	out, _, err := dockhand(t, "status")
	require.NoError(t, err)
	require.Contains(t, out, "  Ports    none yet\n")
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "lang", "vx"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "lang", "vx", "Portfile"), []byte("name vx\nversion 1\n"), 0o644))
	_, _, _ = dockhand(t, "check", "--include", "lang/vx/Portfile")
	out, _, err = dockhand(t, "status")
	require.NoError(t, err)
	require.Contains(t, out, "  Ports    none committed; vx built from untracked files (check --include)\n")
}
