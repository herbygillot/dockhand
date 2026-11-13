package checks

import (
	"maps"
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
			// A joining request must consume the same configured builds,
			// rather than merely share the target's own build ID (#236).
			has, hasErr := have.CheckBuilds(build.Environment).Planned(id)
			wants, wantErr := want.CheckBuilds(build.Environment).Planned(id)
			if hasErr != nil || wantErr != nil || !maps.Equal(has, wants) {
				return false
			}
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
	return SourcesCover(have, want)
}

// SourcesCover reports whether a queued or running check's resolved
// sources let another request join it: for each target want builds in
// each environment, the Git source and the tag judge same, or unresolved
// on both sides (model.SourceAgainst). Joining shares a request and claims
// nothing of the source. A tag a port fetches by that moved between the
// plans names another source though the Portfile is the same, and a check
// of the old one stood for the new (#114; #71 fixed the reuse of builds
// after it).
func SourcesCover(have, want model.Plan) bool {
	return sourcesStand(have, want, func(standing model.SourceStanding, _ bool) bool {
		return standing == model.SourceSame || standing == model.SourceUnresolved
	})
}

// FinishedSourcesCover reports whether a finished check's resolved sources
// stand for another request: a Git source judges same, since what isn't
// known of a Git fetch stands for nothing, and a tag same or unresolved,
// since the Portfile's checksums say what an archive was (docs/designs/
// source-comparison.md, change 1). Its caller asks each Git result what
// it fetched too.
func FinishedSourcesCover(have, want model.Plan) bool {
	return sourcesStand(have, want, func(standing model.SourceStanding, git bool) bool {
		return standing == model.SourceSame || !git && standing == model.SourceUnresolved
	})
}

// sourcesStand asks the judgment of each target's Git source and tag in
// each environment want builds, per environment, and stands only where
// stands accepts every answer.
func sourcesStand(have, want model.Plan, stands func(standing model.SourceStanding, git bool) bool) bool {
	for _, build := range want.Builds {
		at := slices.IndexFunc(have.Builds, func(b model.EnvironmentPlan) bool { return b.Environment == build.Environment })
		if at < 0 {
			return false
		}
		for _, id := range build.Order {
			for i, sources := range [][2]map[model.TargetID]model.GitSource{{have.Builds[at].Git, build.Git}, {have.Builds[at].Tags, build.Tags}} {
				if !stands(model.SourceAgainst(sourceOf(sources[0], id), sourceOf(sources[1], id)), i == 0) {
					return false
				}
			}
		}
	}
	return true
}

// sourceOf is a target's source in a plan's map, nil where it isn't
// fetched that way.
func sourceOf(sources map[model.TargetID]model.GitSource, id model.TargetID) *model.GitSource {
	if source, ok := sources[id]; ok {
		return &source
	}
	return nil
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
