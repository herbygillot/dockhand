package engine

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/buildenv"
	"github.com/herbygillot/dockhand/internal/model"
)

// --on names providers that are set up, each once, with releases only
// where the provider can build on them; with none, the command provider
// when there is one.
func TestEnvironmentsAreTheProvidersOnNames(t *testing.T) {
	t.Parallel()
	e := &Engine{Providers: map[string]buildenv.Provider{"command": &scriptedProvider{}, "github": &scriptedProvider{}}}
	environments, err := e.Environments(t.Context(), nil)
	require.NoError(t, err)
	require.Equal(t, []model.Environment{{Provider: "command"}}, environments)
	environments, err = e.Environments(t.Context(), []string{"github", "command", "github"})
	require.NoError(t, err)
	require.Equal(t, []model.Environment{{Provider: "github"}, {Provider: "command"}}, environments)

	for on, refusal := range map[string]string{
		"tart:sonoma": "Tart isn't installed here",
		"prefix":      "the prefix provider is not in v3 yet",
		"nosuch":      `no provider "nosuch" is set up`,
		"github:15":   "the github provider builds on the runners MacPorts' workflow names",
		"command:15":  "the command provider builds wherever its script does",
	} {
		_, err := e.Environments(t.Context(), []string{on})
		require.ErrorContains(t, err, refusal, on)
	}
	_, err = (&Engine{}).Environments(t.Context(), nil)
	require.ErrorContains(t, err, "a check needs somewhere to build")
	// Tart's own refusal is said, not the general one that suggests the
	// same Tart (the rc3 full run, 2026-10-06).
	_, err = (&Engine{Providers: map[string]buildenv.Provider{"tart": &imageless{}}}).Environments(t.Context(), nil)
	require.ErrorContains(t, err, "no Tart image for macOS 27 (golden-gate): dockhand setup tart golden-gate makes")
	require.NotContains(t, err.Error(), "--on tart builds")
}

// imageless stands for Tart with no base image for this Mac's release.
type imageless struct{ scriptedProvider }

func (*imageless) Environments(context.Context, string) ([]model.Environment, error) {
	return nil, errors.New("no Tart image for macOS 27 (golden-gate): dockhand setup tart golden-gate makes dockhand-base-golden-gate")
}

// releasing stands for Tart: it builds on the releases named, the Mac's
// own by default, Tahoe with Xcode and Sonoma with the Command Line Tools.
type releasing struct{ scriptedProvider }

func (*releasing) Environments(ctx context.Context, releases string) ([]model.Environment, error) {
	if releases == "" {
		releases = "25"
	}
	var environments []model.Environment
	for _, release := range strings.Split(releases, ",") {
		version := map[string]string{"sonoma": "23", "tahoe": "25", "26": "25", "25": "25"}[release]
		tools := model.DeveloperToolsCommandLine
		if version == "25" {
			tools = model.DeveloperToolsXcode
		}
		environments = append(environments, model.Environment{Provider: "tart", Platform: model.Platform{OS: "darwin", Version: version, Architecture: "arm64"}, DeveloperTools: tools})
	}
	return environments, nil
}

// A provider that takes releases makes an environment of each, a bare
// release name means Tart, and Tart on the Mac's release is the default
// when no command provider is set up.
func TestEnvironmentsOfAProviderThatTakesReleases(t *testing.T) {
	t.Parallel()
	e := &Engine{Providers: map[string]buildenv.Provider{"tart": &releasing{}, "github": &scriptedProvider{}}}
	sonoma := model.Environment{Provider: "tart", Platform: model.Platform{OS: "darwin", Version: "23", Architecture: "arm64"}, DeveloperTools: model.DeveloperToolsCommandLine}
	tahoe := model.Environment{Provider: "tart", Platform: model.Platform{OS: "darwin", Version: "25", Architecture: "arm64"}, DeveloperTools: model.DeveloperToolsXcode}
	environments, err := e.Environments(t.Context(), []string{"tart:sonoma,tahoe", "github", "tahoe"})
	require.NoError(t, err)
	require.Equal(t, []model.Environment{sonoma, tahoe, {Provider: "github"}}, environments, "the bare release is the tart:tahoe already named")
	environments, err = e.Environments(t.Context(), nil)
	require.NoError(t, err)
	require.Equal(t, []model.Environment{tahoe}, environments)
	e.Providers["command"] = &scriptedProvider{}
	environments, err = e.Environments(t.Context(), nil)
	require.NoError(t, err)
	require.Equal(t, []model.Environment{{Provider: "command"}}, environments, "a command provider someone set up comes first")
}

// Environments are named as --on names them, Tart's releases together by
// their product versions; one whose release dockhand doesn't know can't
// be.
func TestEnvironmentsAreNamedAsOnNamesThem(t *testing.T) {
	t.Parallel()
	tart := func(darwin string, tools model.DeveloperTools) model.Environment {
		return model.Environment{Provider: "tart", Platform: model.Platform{OS: "darwin", Version: darwin, Architecture: "arm64"}, DeveloperTools: tools}
	}
	values, ok := OnValues([]model.Environment{{Provider: "command"}, tart("21", model.DeveloperToolsCommandLine), tart("25", model.DeveloperToolsXcode), tart("21", model.DeveloperToolsXcode), {Provider: "github"}})
	require.True(t, ok)
	require.Equal(t, []string{"command", "tart:12,26", "github"}, values)
	_, ok = OnValues([]model.Environment{tart("99", "")})
	require.False(t, ok)
}
