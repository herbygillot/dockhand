package engine

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/buildenv"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/testsupport"
)

// A check on macOS 26 alone leaves MacPorts' CI's 14 and 15 uncovered;
// one on the github provider runs MacPorts' own workflow, and leaves none.
func TestUncoveredCIIsWhatNoEnvironmentBuiltOn(t *testing.T) {
	tahoe := model.Environment{Provider: buildenv.Tart, Platform: model.Platform{OS: "darwin", Version: "25", Architecture: "arm64"}}
	sequoia := model.Environment{Provider: buildenv.Tart, Platform: model.Platform{OS: "darwin", Version: "24", Architecture: "arm64"}}
	ci := []string{"14", "15", "26"}
	require.Equal(t, []string{"14", "15"}, uncoveredCI(ci, []model.Environment{tahoe}))
	require.Equal(t, []string{"14"}, uncoveredCI(ci, []model.Environment{tahoe, sequoia}))
	// A GitHub check covers each release whose job it recorded, its own
	// environment.
	github := func(darwin string) model.Environment {
		return model.Environment{Provider: buildenv.GitHub, Platform: model.Platform{OS: "darwin", Version: darwin, Architecture: "arm64"}}
	}
	require.Equal(t, []string{"14"}, uncoveredCI(ci, []model.Environment{github("24"), github("25")}))
	require.Empty(t, uncoveredCI(nil, []model.Environment{tahoe}), "a tree whose workflow names none")
}

// ci reads MacPorts' CI's releases from the workflow at master as last
// fetched, as Tart names them.
func TestCIIsTheReleasesMastersWorkflowNames(t *testing.T) {
	t.Parallel()
	f := setup(t)
	write(t, f.upstream, map[string]string{".github/workflows/main.yml": "jobs:\n  build:\n    strategy:\n      matrix:\n        os: [macos-14, macos-15, macos-26]\n"})
	testsupport.Git(t, f.upstream, "add", ".")
	testsupport.Git(t, f.upstream, "commit", "-q", "-m", "CI")
	e := f.open(t)
	_, err := e.ciSlugs(t.Context())
	require.ErrorContains(t, err, "master hasn't been fetched")
	_, err = e.Start(t.Context(), StartRequest{Name: "fetches-master"})
	require.NoError(t, err)
	slugs, err := e.ciSlugs(t.Context())
	require.NoError(t, err)
	require.Equal(t, []string{"sonoma", "sequoia", "tahoe"}, slugs)

	// A GitHub check builds on each release the workflow names, each its
	// own environment, named back as github:14,15,26; one release alone is
	// refused, since the workflow runs them all (the architecture thread,
	// 2026-10-29).
	e.Providers = map[string]buildenv.Provider{buildenv.GitHub: &scriptedProvider{}}
	environments, err := e.Environments(t.Context(), []string{"github"})
	require.NoError(t, err)
	var darwins []string
	for _, environment := range environments {
		require.Equal(t, buildenv.GitHub, environment.Provider)
		require.Equal(t, "arm64", environment.Platform.Architecture)
		darwins = append(darwins, environment.Platform.Version)
	}
	require.Equal(t, []string{"23", "24", "25"}, darwins)
	values, ok := OnValues(environments)
	require.True(t, ok)
	require.Equal(t, []string{"github:14,15,26"}, values)
	_, err = e.Environments(t.Context(), []string{"github:15"})
	require.EqualError(t, err, "--on github:15: the github provider builds every release MacPorts' workflow names; to check one release, use tart or local")
	// A github build profile is every release too; one listing releases
	// is refused.
	profiled, err := e.profileEnvironments(t.Context(), Profile{Name: "fork", Provider: buildenv.GitHub})
	require.NoError(t, err)
	require.Equal(t, environments, profiled)
	_, err = e.profileEnvironments(t.Context(), Profile{Name: "fork", Provider: buildenv.GitHub, Releases: []string{"15"}})
	require.ErrorContains(t, err, "build profile fork: --on github:15: the github provider builds every release")
}

// A plan says before anything is pushed what its GitHub environments won't
// build: MacPorts' workflow builds only the ports a commit changes, so
// neither --also's unchanged ports nor a baseline's.
func TestAPlanSaysWhatGitHubWontBuild(t *testing.T) {
	t.Parallel()
	github := model.Environment{Provider: buildenv.GitHub, Platform: model.Platform{OS: "darwin", Version: "24", Architecture: "arm64"}}
	tart := model.Environment{Provider: buildenv.Tart, Platform: github.Platform}
	targets := []model.PlanTarget{{ID: "jq", Role: model.Changed}, {ID: "gojq", Role: model.Also}, {ID: "oniguruma", Role: model.Prerequisite}}
	require.Equal(t, []string{"gojq"}, GitHubWontBuild(model.Plan{Environments: []model.Environment{tart, github}, Targets: targets}))
	require.Empty(t, GitHubWontBuild(model.Plan{Environments: []model.Environment{tart}, Targets: targets}), "Tart builds what it's asked")
}
