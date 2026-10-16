package engine

import (
	"context"
	"fmt"
	"slices"
	"strconv"

	"github.com/herbygillot/dockhand/internal/buildenv"
	"github.com/herbygillot/dockhand/internal/macos"
	"github.com/herbygillot/dockhand/internal/model"
)

// PlanNotes say what a plan's preview and its check's heading should
// add of where it builds: an environment whose MacPorts is older than this
// Mac's, which reads the ports it builds, and where the plan's test policy
// can't be carried out as asked.
func (e *Engine) PlanNotes(ctx context.Context, plan model.Plan) []string {
	notes := e.macPortsNotes(ctx, plan)
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

// macPortsNotes say each environment that builds with an older MacPorts
// than this Mac's, with how to make its image again.
func (e *Engine) macPortsNotes(ctx context.Context, plan model.Plan) []string {
	var notes []string
	for _, environment := range plan.Environments {
		provider, ok := e.Providers[environment.Provider].(buildenv.MacPortsProvider)
		if !ok {
			continue
		}
		version, host := provider.MacPortsBehind(ctx, environment)
		if version == "" {
			continue
		}
		remedy := ""
		if darwin, err := strconv.Atoi(environment.Platform.Version); err == nil && environment.Provider == buildenv.Tart {
			if release, err := macos.ReleaseForDarwin(darwin); err == nil {
				remedy = "; dockhand setup tart " + release.Slug + " --rebuild makes it again"
				if environment.DeveloperTools == model.DeveloperToolsXcode {
					remedy = "; dockhand setup tart " + release.Slug + " --rebuild --xcode <Xcode> makes it again"
				}
			}
		}
		notes = append(notes, fmt.Sprintf("%s builds with MacPorts %s, older than this Mac's %s, which reads its ports%s", DescribeEnvironment(environment), version, host, remedy))
	}
	return notes
}
