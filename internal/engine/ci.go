package engine

import (
	"context"
	"fmt"
	"strconv"

	"github.com/herbygillot/dockhand/internal/buildenv"
	"github.com/herbygillot/dockhand/internal/macos"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/model"
)

// ciReleases are the macOS releases MacPorts' CI builds on, as its
// workflow in a tree names them; none where it can't be read.
func (e *Engine) ciReleases(ctx context.Context, tree string) []string {
	file, data, err := e.Repo.File(ctx, tree, macports.CIWorkflow)
	if err != nil || !file.Exists {
		return nil
	}
	return macports.CIReleases(string(data))
}

// uncoveredCI are the releases of MacPorts' CI that no environment of a
// check built on: tart 2.40.1 passed its check on macOS 26 alone, and
// failed MacPorts CI on 15, using an API only the macOS 26 SDK has (field
// testing, #35157). A check on the github provider runs MacPorts' own
// workflow, so it covers them all.
func uncoveredCI(ci []string, environments []model.Environment) []string {
	covered := map[string]bool{}
	for _, environment := range environments {
		if environment.Provider == buildenv.GitHub {
			return nil
		}
		darwin, err := strconv.Atoi(environment.Platform.Version)
		if err != nil {
			continue
		}
		if product, err := macos.ProductForDarwin(darwin); err == nil {
			covered[product] = true
		}
	}
	var uncovered []string
	for _, release := range ci {
		if !covered[release] {
			uncovered = append(uncovered, release)
		}
	}
	return uncovered
}

// ciEnvironments is the name check.on and --on give MacPorts' CI's
// releases, on Tart.
const ciEnvironments = "ci"

// ciSlugs are MacPorts' CI's releases as Tart names them, sonoma, sequoia,
// tahoe, read from the workflow at master as last fetched.
func (e *Engine) ciSlugs(ctx context.Context) ([]string, error) {
	master, ok := e.lastMaster(ctx)
	if !ok {
		return nil, fmt.Errorf("--on ci: master hasn't been fetched, so MacPorts' CI releases can't be read; dockhand outdated or update fetches it")
	}
	trees, err := e.Repo.CommitTrees(ctx, []string{string(master)})
	if err != nil {
		return nil, err
	}
	releases := e.ciReleases(ctx, trees[string(master)])
	if len(releases) == 0 {
		return nil, fmt.Errorf("--on ci: %s at master names no releases MacPorts' CI builds on", macports.CIWorkflow)
	}
	var slugs []string
	for _, product := range releases {
		release, err := macos.ParseRelease(product)
		if err != nil {
			return nil, fmt.Errorf("--on ci: MacPorts' CI builds on macOS %s, which dockhand doesn't know: %w", product, err)
		}
		slugs = append(slugs, release.Slug)
	}
	return slugs, nil
}
