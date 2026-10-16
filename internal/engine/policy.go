package engine

import (
	"context"
	"fmt"
	"github.com/herbygillot/dockhand/internal/prose"
	"slices"
	"strconv"
	"strings"

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

// macPortsNotes say the environments the check builds on whose MacPorts
// is older than this Mac's, in one line for each pair of versions, with
// the setup that makes each image again: six lines, one per image, four of
// them images the check built nothing on, read as noise (field testing,
// 2026-10-16). An Xcode image's names its Xcode, where it said <Xcode>.
func (e *Engine) macPortsNotes(ctx context.Context, plan model.Plan) []string {
	type versions struct{ image, host string }
	var order []versions
	behind := map[versions][]model.Environment{}
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
		key := versions{version, host}
		if _, seen := behind[key]; !seen {
			order = append(order, key)
		}
		behind[key] = append(behind[key], environment)
	}
	var notes []string
	for _, key := range order {
		var names, remedies []string
		for _, environment := range behind[key] {
			names = append(names, DescribeEnvironment(environment))
			if remedy := remakeWords(environment); remedy != "" {
				remedies = append(remedies, remedy)
			}
		}
		one := len(names) == 1
		line := fmt.Sprintf("%s %s with MacPorts %s, older than this Mac's %s, which reads %s ports", prose.And(names), map[bool]string{true: "builds", false: "build"}[one], key.image, key.host, map[bool]string{true: "its", false: "their"}[one])
		if len(remedies) > 0 {
			line += "; " + map[bool]string{true: "to make it again", false: "to make them again"}[len(remedies) == 1] + ": " + strings.Join(remedies, "; ")
		}
		notes = append(notes, line)
	}
	return notes
}

// remakeWords is the setup that makes a Tart environment's image again,
// with its Xcode named for an Xcode image; empty for another provider.
func remakeWords(environment model.Environment) string {
	darwin, err := strconv.Atoi(environment.Platform.Version)
	if err != nil || environment.Provider != buildenv.Tart {
		return ""
	}
	release, err := macos.ReleaseForDarwin(darwin)
	if err != nil {
		return ""
	}
	remedy := "dockhand setup tart " + release.Slug + " --rebuild"
	if environment.DeveloperTools == model.DeveloperToolsXcode {
		remedy += " --xcode <folder> (Xcode " + release.Xcode + "'s .xip, or a folder xcodes downloads it into)"
	}
	return remedy
}
