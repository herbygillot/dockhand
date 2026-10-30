package engine

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strconv"

	"github.com/herbygillot/dockhand/internal/buildenv"
	"github.com/herbygillot/dockhand/internal/macos"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/store"
)

// PlanNotes say what a plan's preview and its check's heading should
// add of where it builds: an environment whose MacPorts is older than this
// Mac's, which reads the ports it builds, and where the plan's test policy
// can't be carried out as asked.
func (e *Engine) PlanNotes(ctx context.Context, plan model.Plan) []string {
	notes := append(e.macPortsNotes(ctx, plan), e.tagMovedNotes(ctx, plan)...)
	if plan.Tests != model.TestsSkip {
		return notes
	}
	var seen []string
	for _, environment := range plan.Environments {
		name := environment.Provider
		if slices.Contains(seen, name) {
			continue
		}
		seen = append(seen, name)
		if provider, ok := e.Providers[name].(buildenv.OwnTestsProvider); ok && provider.RunsOwnTests() {
			notes = append(notes, fmt.Sprintf("%s runs its workflow's own tests; with --tests skip they run there, and don't count", name))
		}
	}
	return notes
}

// macPortsNotes say the environments the check builds on whose MacPorts
// is older than this Mac's, in one line for each pair of versions, with
// the setup that makes each image again: six lines, one per image, four of
// them images the check built nothing on, read as noise (field testing,
// 2026-10-16). An Xcode image's names its Xcode, where it said <Xcode>.
func (e *Engine) macPortsNotes(ctx context.Context, plan model.Plan) []string {
	var behind []MacPortsBehind
	for _, build := range plan.Builds {
		environment := build.Environment
		provider, ok := e.Providers[environment.Provider].(buildenv.MacPortsProvider)
		if !ok || len(build.Order) == 0 {
			continue
		}
		version, host := provider.MacPortsBehind(ctx, environment)
		if version == "" {
			continue
		}
		behind = append(behind, MacPortsBehind{Name: DescribeEnvironment(environment), Image: version, Host: host, Remedy: remakeWords(environment)})
	}
	return OlderMacPorts(behind, true)
}

// remakeWords is the setup that makes a Tart environment's image again
// (RemakeWords); empty for another provider.
func remakeWords(environment model.Environment) string {
	darwin, err := strconv.Atoi(environment.Platform.Version)
	if err != nil || environment.Provider != buildenv.Tart {
		return ""
	}
	release, err := macos.ReleaseForDarwin(darwin)
	if err != nil {
		return ""
	}
	return RemakeWords(release, environment.DeveloperTools == model.DeveloperToolsXcode)
}

// tagMovedNotes say each target the check builds again because the GitHub
// tag its archive is fetched by names another commit now than when an
// earlier check of the branch built it there (D-N5), from the plans'
// Tags, asking nothing new: rc1's check --plan said nothing of it, which
// only the running check and the submission's concerns said.
func (e *Engine) tagMovedNotes(ctx context.Context, plan model.Plan) []string {
	if e.Store == nil || !slices.ContainsFunc(plan.Builds, func(build model.EnvironmentPlan) bool { return len(build.Tags) > 0 }) {
		return nil
	}
	var notes []string
	_ = e.Store.View(ctx, e.Repository, func(r store.Reader) error {
		revision, err := r.Revision(plan.Revision)
		if err != nil {
			return err
		}
		runs, err := r.Runs(store.RunFilter{Branch: revision.Branch})
		if err != nil {
			return err
		}
		said := map[model.TargetID]bool{}
		for _, build := range plan.Builds {
			for _, id := range slices.Sorted(maps.Keys(build.Tags)) {
				now := build.Tags[id]
				if said[id] || now.Commit == "" {
					continue
				}
				// The newest earlier check that fetched it by the same tag,
				// and of those that fetched that commit, the first, which
				// built it; later ones reused its build.
				var then model.GitSource
				built := ""
				for _, run := range runs {
					if !run.State.Finished() {
						continue
					}
					earlier, err := r.Plan(run.Plan)
					if err != nil {
						continue
					}
					planned, _ := earlier.In(build.Environment)
					tag, ok := planned.Tags[id]
					if !ok || tag.Ref != now.Ref || tag.Commit == "" || then.Commit != "" && tag.Commit != then.Commit {
						continue
					}
					then, built = tag, run.Name()
				}
				if then.Commit != "" && then.Commit != now.Commit {
					said[id] = true
					target, _ := plan.Target(id)
					notes = append(notes, fmt.Sprintf("%s builds again: %s", id, tagMovedWords(then, now, built, target.Target.Name)))
				}
			}
		}
		return nil
	})
	return notes
}
