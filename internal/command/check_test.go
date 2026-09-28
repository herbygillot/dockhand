package command

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/engine"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/model"
)

// onePort stands in for MacPorts: every directory defines one port, named
// after it.
type onePort struct{}

func (onePort) Ports(_ context.Context, _ model.Source, directory string, _ model.Environment) ([]macports.PortInfo, error) {
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
	require.Equal(t, "jq-update · captured working files as snapshot 1\nChanged     jq\nProvider    command · tests declared\n", out)
	_, _, err = dockhand(t, "check", "--on", "tart:tahoe")
	require.ErrorContains(t, err, "Tart isn't installed here")

	out, errs, err := dockhand(t, "check")
	require.NoError(t, err)
	require.Contains(t, errs, "check-1 runs here, since no dockhand serve is running.")
	require.Contains(t, errs, "command: jq passed")
	require.Contains(t, out, "jq-update · checking snapshot 1\n")
	require.Contains(t, out, "  jq  ✓\n\nPassed for snapshot 1.\nNext: dockhand tidy --branch jq-update\n", "what moves the branch on, as status says it")

	out, _, err = dockhand(t, "check", "-d")
	require.NoError(t, err)
	require.Contains(t, out, "check-2 queued; nothing is running it: dockhand serve\n")

	withScript(t, w, "failed")
	_, _, err = dockhand(t, "check")
	require.ErrorContains(t, err, "check-2 is already queued for these files; dockhand wait check-2 follows it", "one check of a branch at a time")
	out, _, err = dockhand(t, "check", "--replace")
	require.Contains(t, out, "Stopped check-2; what it finished is kept.\ncheck-3 replaces check-2.\n")
	require.Equal(t, 2, ExitCode(err), "a failed check exits 2")
	require.ErrorContains(t, err, "check-3 failed for snapshot 1: jq did not pass. Logs: dockhand logs check-3")
	require.Contains(t, out, "  jq  ✗ failed at install\n")
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
		{Target: targets[0], Outcomes: []model.TargetResult{passed, passed}},
		{Target: targets[1], Outcomes: []model.TargetResult{passed, failed}},
	}}
	var out bytes.Buffer
	writeResults(&out, "  ", evidence, false)
	require.Equal(t, "  PORT          macOS 26   macOS 15\n"+
		"  flatbuffers   ✓          ✓\n"+
		"  libsigmf      ✓          ✗ failed at install\n", out.String())

	evidence.Plan.Environments = []model.Environment{tahoe}
	evidence.Targets = evidence.Targets[:1]
	evidence.Targets[0].Outcomes = []model.TargetResult{passed}
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
func TestBaselineResultsShowWhereItRebuilt(t *testing.T) {
	tahoe := model.Environment{Provider: "tart", Platform: model.Platform{OS: "darwin", Version: "25", Architecture: "arm64"}}
	sequoia := model.Environment{Provider: "tart", Platform: model.Platform{OS: "darwin", Version: "24", Architecture: "arm64"}}
	jq := model.PlanTarget{ID: "jq", Target: model.Target{Name: "jq"}}
	gone := model.PlanTarget{ID: "gone", Target: model.Target{Name: "gone"}}
	passed := model.TargetResult{Outcome: model.OutcomePassed}
	failed := model.TargetResult{Outcome: model.OutcomeFailed, Phase: model.PhaseInstall}
	notRun := model.TargetResult{Outcome: model.OutcomeNotRun}
	branch := engine.Evidence{Run: model.Run{Number: 4}, Plan: model.Plan{Environments: []model.Environment{tahoe, sequoia}}, Targets: []engine.TargetEvidence{
		{Target: jq, Outcomes: []model.TargetResult{passed, failed}},
		{Target: gone, Outcomes: []model.TargetResult{failed, passed}},
	}}
	base := engine.Evidence{Plan: model.Plan{Environments: []model.Environment{tahoe, sequoia}, Builds: []model.EnvironmentPlan{
		{Environment: tahoe, Exclusions: []model.Exclusion{{Target: jq.Target, Reason: "check-4 didn't fail it there"}, {Target: gone.Target, Reason: "replaced by other"}}},
		{Environment: sequoia, Order: []model.TargetID{"jq"}, Exclusions: []model.Exclusion{{Target: gone.Target, Reason: "check-4 didn't fail it there"}}},
	}}, Targets: []engine.TargetEvidence{
		{Target: jq, Outcomes: []model.TargetResult{notRun, failed}},
		{Target: gone, Outcomes: []model.TargetResult{notRun, notRun}},
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
