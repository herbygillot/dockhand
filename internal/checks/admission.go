package checks

import (
	"slices"

	"github.com/herbygillot/dockhand/internal/model"
)

// PlanCovers reports whether a check still to finish, have, would stand
// for want once it passed: every cell want builds, each target in each
// environment, built in have's environment too, a test policy at least as
// strict, the same freshness and variants, and a guest kept where want
// keeps one. The sets alone covered a minmax check for check --build all,
// whose lists were the same and whose cells were more, and a check that
// keeps no guest for check --keep (Codex's review of 2ad896d2, W4). A
// rerun follows such a check, and queues another for one that doesn't.
func PlanCovers(have, want model.Plan) bool {
	if TestsRank(have.Tests) < TestsRank(want.Tests) || have.Fresh != want.Fresh || have.Variants != want.Variants || have.EachVariant != want.EachVariant {
		return false
	}
	if want.Keep && !have.Keep {
		return false
	}
	for _, environment := range want.Environments {
		if !slices.Contains(have.Environments, environment) {
			return false
		}
	}
	for _, build := range want.Builds {
		at := slices.IndexFunc(have.Builds, func(b model.EnvironmentPlan) bool { return b.Environment == build.Environment })
		if at < 0 {
			return false
		}
		for _, id := range build.Order {
			if !slices.Contains(have.Builds[at].Order, id) {
				return false
			}
		}
	}
	for _, target := range want.Targets {
		if !slices.ContainsFunc(have.Targets, func(other model.PlanTarget) bool { return other.ID == target.ID }) {
			return false
		}
	}
	return true
}

// TestsRank orders test policies by how much a pass under them shows:
// skipped tests, then declared ones reported, then required ones.
func TestsRank(policy model.TestPolicy) int {
	switch policy {
	case model.TestsRequired:
		return 2
	case model.TestsDeclared:
		return 1
	}
	return 0
}
