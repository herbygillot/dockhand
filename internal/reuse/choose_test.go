package reuse

import (
	"maps"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/model"
)

// Each target reuses its newest earlier build that stands, and builds
// otherwise; a target that builds takes the ones it needs with it, since
// a reused build isn't in the guest to be installed.
func TestTargetsReuseWhatStandsAndBuildWhatTheBuiltOnesNeed(t *testing.T) {
	now := map[string]model.ObjectID{"devel/lib": "1", "devel/cli": "2", "graphics/viewer": "3", "graphics/tools": "4", macports.ResourcesDirectory: "5"}
	built := func(target, directory string, active ...model.ActivePort) Candidate {
		inputs := model.NewTargetInputs("origin a", directory, now[directory], now[macports.ResourcesDirectory], nil, append([]model.ActivePort{}, active...))
		return Candidate{Result: model.TargetResult{Target: model.TargetID(target), Outcome: model.OutcomePassed, Execution: "tart_1"}, Inputs: inputs}
	}
	lib := model.ActivePort{Name: "Lib", Spec: "@1_0", Directory: "devel/lib", Tree: "1", Archive: "sha256:aa"}
	target := func(id, directory string, dependsOn []model.TargetID, earlier ...Candidate) Target {
		return Target{PlanTarget: model.PlanTarget{ID: model.TargetID(id), Target: model.Target{Name: id}, Directory: directory}, DependsOn: dependsOn, Earlier: earlier}
	}
	targets := func() []Target {
		return []Target{
			target("lib", "devel/lib", nil, built("lib", "devel/lib")),
			target("cli", "devel/cli", []model.TargetID{"lib"}, built("cli", "devel/cli", lib)),
			target("viewer", "graphics/viewer", nil, built("viewer", "graphics/viewer")),
			// tools reaches lib through a port the branch doesn't change:
			// the plan doesn't say so, and its last build had lib active.
			target("tools", "graphics/tools", nil, built("tools", "graphics/tools", lib)),
		}
	}
	stands := func(model.TargetResult) bool { return true }
	none := func(Candidate) bool { return false }
	reused := func(choice Choice) []model.TargetID {
		return slices.Sorted(maps.Keys(choice.Reused))
	}

	require.Equal(t, []model.TargetID{"cli", "lib", "tools", "viewer"}, reused(Choose(targets(), "origin a", now, stands, none)), "nothing changed")
	require.Empty(t, reused(Choose(targets(), "origin b", now, stands, none)), "the environment was made again")
	require.Empty(t, reused(Choose(targets(), "origin a", now, func(model.TargetResult) bool { return false }, none)), "the check's policy lets none stand")

	changed := maps.Clone(now)
	changed["graphics/viewer"] = "9"
	require.Equal(t, []model.TargetID{"cli", "lib", "tools"}, reused(Choose(targets(), "origin a", changed, stands, none)), "viewer changed, and needs none of the others")

	changed = maps.Clone(now)
	changed["devel/cli"] = "9"
	require.Equal(t, []model.TargetID{"tools", "viewer"}, reused(Choose(targets(), "origin a", changed, stands, none)), "cli builds, and takes lib, which the plan says it needs")
	kept := Choose(targets(), "origin a", changed, stands, func(Candidate) bool { return true })
	require.Equal(t, []model.TargetID{"lib", "tools", "viewer"}, reused(kept), "with lib's archive kept, lib is reused")
	require.Equal(t, []model.TargetID{"lib"}, kept.Installs, "and the guest installs it for cli")

	changed = maps.Clone(now)
	changed["graphics/tools"] = "9"
	require.Equal(t, []model.TargetID{"cli", "viewer"}, reused(Choose(targets(), "origin a", changed, stands, none)), "tools builds, and takes lib, active as it last built")
	require.Equal(t, []model.TargetID{"lib"}, Choose(targets(), "origin a", changed, stands, func(Candidate) bool { return true }).Installs, "or installs it, kept")

	// What one taken target needs is taken too.
	chain := targets()
	chain[2].DependsOn = []model.TargetID{"cli"}
	changed = maps.Clone(now)
	changed["graphics/viewer"] = "9"
	require.Equal(t, []model.TargetID{"tools"}, reused(Choose(chain, "origin a", changed, stands, none)), "viewer takes cli, and cli takes lib")
	onlyCli := func(c Candidate) bool { return c.Result.Target == "cli" }
	installed := Choose(chain, "origin a", changed, stands, onlyCli)
	require.Equal(t, []model.TargetID{"cli", "lib", "tools"}, reused(installed), "cli, installed, takes nothing with it")
	require.Equal(t, []model.TargetID{"cli"}, installed.Installs)

	// The newest build that stands is the one reused.
	older := built("lib", "devel/lib")
	older.Result.Execution = "tart_0"
	stale := built("lib", "devel/lib")
	stale.Inputs.Tree = "8"
	newest := targets()
	newest[0].Earlier = []Candidate{stale, older}
	require.Equal(t, model.ExecutionID("tart_0"), Choose(newest, "origin a", now, stands, none).Reused["lib"].Result.Execution, "the newest doesn't read what lib would now")
}

// A moved tag doesn't let earlier build evidence stand for the commit it
// names now (batch 20): the Git-fetched target builds, and so does what
// was built against its earlier build, which read what that commit made,
// and what needs one of those in turn. What was built against a build of
// the commit expected now stands, and what needs none of them.
func TestAMovedTagRebuildsWhatWasBuiltAgainstIt(t *testing.T) {
	now := map[string]model.ObjectID{"devel/lib": "1", "devel/cli": "2", "graphics/viewer": "3", "graphics/tools": "4", "graphics/app": "6", macports.ResourcesDirectory: "5"}
	a, b := model.ObjectID(strings.Repeat("a", 40)), model.ObjectID(strings.Repeat("b", 40))
	built := func(target, directory, archive string, fetched model.ObjectID, active ...model.ActivePort) Candidate {
		inputs := model.NewTargetInputs("origin a", directory, now[directory], now[macports.ResourcesDirectory], nil, append([]model.ActivePort{}, active...))
		inputs.Fetched = fetched
		return Candidate{Result: model.TargetResult{Target: model.TargetID(target), Outcome: model.OutcomePassed, Execution: "tart_1", Archive: archive}, Inputs: inputs}
	}
	libFrom := func(archive string) model.ActivePort {
		return model.ActivePort{Name: "lib", Spec: "@4_0", Directory: "devel/lib", Tree: "1", Archive: archive}
	}
	cliBuilt := model.ActivePort{Name: "cli", Spec: "@1_0", Directory: "devel/cli", Tree: "2", Archive: "sha256:cli"}
	expecting := func(commit model.ObjectID) *model.GitSource {
		return &model.GitSource{URL: "https://github.com/harbor/lib.git", Ref: "v4", Commit: commit, ResolvedAt: time.Now()}
	}
	targets := func(expected model.ObjectID, libBuilds ...Candidate) []Target {
		return []Target{
			{PlanTarget: model.PlanTarget{ID: "lib", Target: model.Target{Name: "lib"}, Directory: "devel/lib"}, Git: expecting(expected), Earlier: libBuilds},
			{PlanTarget: model.PlanTarget{ID: "cli", Target: model.Target{Name: "cli"}, Directory: "devel/cli"}, DependsOn: []model.TargetID{"lib"},
				Earlier: []Candidate{built("cli", "devel/cli", "sha256:cli", "", libFrom("sha256:lib-a"))}},
			// tools reaches lib through a port the branch doesn't change.
			{PlanTarget: model.PlanTarget{ID: "tools", Target: model.Target{Name: "tools"}, Directory: "graphics/tools"},
				Earlier: []Candidate{built("tools", "graphics/tools", "sha256:tools", "", libFrom("sha256:lib-a"))}},
			// app needs cli, and not lib itself.
			{PlanTarget: model.PlanTarget{ID: "app", Target: model.Target{Name: "app"}, Directory: "graphics/app"}, DependsOn: []model.TargetID{"cli"},
				Earlier: []Candidate{built("app", "graphics/app", "sha256:app", "", cliBuilt)}},
			{PlanTarget: model.PlanTarget{ID: "viewer", Target: model.Target{Name: "viewer"}, Directory: "graphics/viewer"},
				Earlier: []Candidate{built("viewer", "graphics/viewer", "sha256:viewer", "")}},
		}
	}
	stands := func(model.TargetResult) bool { return true }
	kept := func(Candidate) bool { return true }
	reused := func(choice Choice) []model.TargetID { return slices.Sorted(maps.Keys(choice.Reused)) }
	fromA := built("lib", "devel/lib", "sha256:lib-a", a)

	require.Equal(t, []model.TargetID{"app", "cli", "lib", "tools", "viewer"}, reused(Choose(targets(a, fromA), "origin a", now, stands, kept)), "the tag names the commit it did")
	require.Equal(t, []model.TargetID{"viewer"}, reused(Choose(targets(b, fromA), "origin a", now, stands, kept)),
		"moved: lib builds from its new commit, cli and tools were built against the old one, and app against that cli")

	fromB := built("lib", "devel/lib", "sha256:lib-b", b)
	require.Equal(t, []model.TargetID{"lib", "viewer"}, reused(Choose(targets(b, fromB, fromA), "origin a", now, stands, kept)),
		"lib reuses its build of the new commit; cli and tools were built against the old one's")
	againstB := targets(b, fromB, fromA)
	againstB[1].Earlier = []Candidate{built("cli", "devel/cli", "sha256:cli", "", libFrom("sha256:lib-b"))}
	require.Equal(t, []model.TargetID{"app", "cli", "lib", "viewer"}, reused(Choose(againstB, "origin a", now, stands, kept)), "cli was built against the new commit's build")

	unrecorded := built("lib", "devel/lib", "sha256:lib-a", "")
	require.Equal(t, []model.TargetID{"viewer"}, reused(Choose(targets(a, unrecorded), "origin a", now, stands, kept)),
		"a build that recorded no commit, as every one before batch 20, stands for no Git-fetched target, nor for what was built against it")
}

// What was built against another commit's build of a Git-fetched target
// than its plan expects is one rule, which reuse rebuilds by and evidence
// judges an earlier check's results by, whatever their outcome: built
// against it directly, or against a build in that case, or by a build
// that didn't say which ports were active, which can't be established to
// have had the commit expected. The Git-fetched target's own commit isn't
// this rule's (Current), and a build needing none of them stands.
func TestWhatWasBuiltAgainstAnotherSourceIsOneRule(t *testing.T) {
	a, b := model.ObjectID(strings.Repeat("a", 40)), model.ObjectID(strings.Repeat("b", 40))
	build := func(target string, outcome model.Outcome, archive string, fetched model.ObjectID, active []model.ActivePort) Candidate {
		return Candidate{Result: model.TargetResult{Target: model.TargetID(target), Outcome: outcome, Archive: archive}, Inputs: model.TargetInputs{Active: active, Fetched: fetched}}
	}
	chosen := map[model.TargetID]Candidate{
		"lib": build("lib", model.OutcomePassed, "sha256:lib-a", a, []model.ActivePort{}),
		// cli failed, built against lib's build of a.
		"cli": build("cli", model.OutcomeFailed, "", "", []model.ActivePort{{Name: "lib", Archive: "sha256:lib-a"}}),
		// app reaches cli through what was active, not the plan.
		"app":   build("app", model.OutcomePassed, "sha256:app", "", []model.ActivePort{{Name: "cli", Archive: "sha256:cli"}}),
		"tools": build("tools", model.OutcomePassed, "sha256:tools", "", nil),
		"docs":  build("docs", model.OutcomePassed, "sha256:docs", "", []model.ActivePort{}),
	}
	targets := func(expected model.ObjectID) []Target {
		target := func(id string, dependsOn ...model.TargetID) Target {
			return Target{PlanTarget: model.PlanTarget{ID: model.TargetID(id)}, DependsOn: dependsOn, Earlier: []Candidate{chosen[model.TargetID(id)]}}
		}
		lib := target("lib")
		lib.Git = &model.GitSource{URL: "https://github.com/harbor/lib.git", Ref: "v4", Commit: expected, ResolvedAt: time.Now()}
		return []Target{lib, target("cli", "lib"), target("app"), target("tools", "lib"), target("docs")}
	}
	require.Equal(t, map[model.TargetID]bool{"tools": true}, AgainstOtherSources(targets(a), chosen), "tools didn't say what it had active")
	require.Equal(t, map[model.TargetID]bool{"cli": true, "app": true, "tools": true}, AgainstOtherSources(targets(b), chosen),
		"the tag moved: cli was built against lib's build of a, and app against that cli")
}
