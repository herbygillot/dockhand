package reuse

import (
	"maps"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/model"
)

// Each target reuses its newest earlier build that stands, and builds
// otherwise; a target that builds takes the ones it needs with it, since
// a reused build isn't in the guest to be installed.
func TestTargetsReuseWhatStandsAndBuildWhatTheBuiltOnesNeed(t *testing.T) {
	now := map[string]model.ObjectID{"devel/lib": "1", "devel/cli": "2", "graphics/viewer": "3", "graphics/tools": "4", Resources: "5"}
	built := func(target, directory string, active ...model.ActivePort) Candidate {
		inputs := model.NewTargetInputs("origin a", directory, now[directory], now[Resources], nil, active)
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
