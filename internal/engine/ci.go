package engine

import (
	"context"
	"fmt"
	"slices"
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
// testing, #35157). A check on the github provider covers each release its
// workflow's job ran on, which is its own environment.
func uncoveredCI(ci []string, environments []model.Environment) []string {
	covered := map[string]bool{}
	for _, environment := range environments {
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

// githubEnvironments are the environments a GitHub check builds in: one
// for each macOS release MacPorts' workflow at master runs a job on, its
// runners' arm64 (the M1's suggestion, 2026-10-29, after rc8's GitHub
// rows, where one release's job never ran and the matrix couldn't be
// recorded as passing the others). The workflow runs every job of its
// matrix, so each is planned and recorded, and a failure on any is seen:
// releases, as github:15, are refused, where the plan would have to say
// per environment which were asked (the architecture thread, 2026-10-29).
func (e *Engine) githubEnvironments(ctx context.Context, releases string) ([]model.Environment, error) {
	if releases != "" {
		return nil, fmt.Errorf("--on github:%s: the github provider builds every release MacPorts' workflow names; to check one release, use tart or local", releases)
	}
	master, ok := e.lastMaster(ctx)
	if !ok {
		return nil, fmt.Errorf("--on github: master hasn't been fetched, so the releases MacPorts' workflow builds on can't be read; dockhand outdated or update fetches it")
	}
	trees, err := e.Repo.CommitTrees(ctx, []string{string(master)})
	if err != nil {
		return nil, err
	}
	var environments []model.Environment
	for _, product := range e.ciReleases(ctx, trees[string(master)]) {
		release, err := macos.ParseRelease(product)
		if err != nil {
			return nil, fmt.Errorf("--on github: MacPorts' workflow builds on macOS %s, which dockhand doesn't know: %w", product, err)
		}
		environment := model.Environment{Provider: buildenv.GitHub, Platform: model.Platform{OS: "darwin", Version: strconv.Itoa(release.Darwin), Architecture: "arm64"}}
		if !slices.Contains(environments, environment) {
			environments = append(environments, environment)
		}
	}
	if len(environments) == 0 {
		return nil, fmt.Errorf("--on github: %s at master names no releases MacPorts' workflow builds on", macports.CIWorkflow)
	}
	return environments, nil
}

// GitHubWontBuild are the targets a plan's GitHub environments won't build:
// MacPorts' workflow builds only the ports a commit changes, so neither
// --also's unchanged ports nor a baseline's, which are the plan's Also
// targets. The plan says them before anything is pushed, where they were
// said only once the run was read.
func GitHubWontBuild(plan model.Plan) []string {
	if !slices.ContainsFunc(plan.Environments, func(environment model.Environment) bool { return environment.Provider == buildenv.GitHub }) {
		return nil
	}
	var unbuilt []string
	for _, target := range plan.Targets {
		if target.Role == model.Also {
			unbuilt = append(unbuilt, string(target.ID))
		}
	}
	return unbuilt
}

// ciSlugs are MacPorts' CI's releases as Tart names them, sonoma, sequoia,
// tahoe, read from the workflow at master as last fetched.
func (e *Engine) ciSlugs(ctx context.Context) ([]string, error) {
	master, ok := e.lastMaster(ctx)
	if !ok {
		return nil, fmt.Errorf("macports-ci: master hasn't been fetched, so MacPorts' CI releases can't be read; dockhand outdated or update fetches it")
	}
	trees, err := e.Repo.CommitTrees(ctx, []string{string(master)})
	if err != nil {
		return nil, err
	}
	releases := e.ciReleases(ctx, trees[string(master)])
	if len(releases) == 0 {
		return nil, fmt.Errorf("macports-ci: %s at master names no releases MacPorts' CI builds on", macports.CIWorkflow)
	}
	var slugs []string
	for _, product := range releases {
		release, err := macos.ParseRelease(product)
		if err != nil {
			return nil, fmt.Errorf("macports-ci: MacPorts' CI builds on macOS %s, which dockhand doesn't know: %w", product, err)
		}
		slugs = append(slugs, release.Slug)
	}
	return slugs, nil
}
