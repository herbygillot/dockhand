package engine

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/provider"
)

// --on names providers that are set up, each once, with releases only
// where the provider can build on them; with none, the command provider
// when there is one.
func TestEnvironmentsAreTheProvidersOnNames(t *testing.T) {
	e := &Engine{Providers: map[string]provider.Provider{"command": &scriptedProvider{}, "github": &scriptedProvider{}}}
	environments, err := e.Environments(t.Context(), nil)
	require.NoError(t, err)
	require.Equal(t, []model.Environment{{Provider: "command"}}, environments)
	environments, err = e.Environments(t.Context(), []string{"github", "command", "github"})
	require.NoError(t, err)
	require.Equal(t, []model.Environment{{Provider: "github"}, {Provider: "command"}}, environments)

	for on, refusal := range map[string]string{
		"tart:sonoma": "Tart isn't installed here",
		"nosuch":      `no provider "nosuch" is set up`,
		"github:15":   "the github provider builds on the runners MacPorts' workflow names",
		"command:15":  "the command provider builds wherever its script does",
	} {
		_, err := e.Environments(t.Context(), []string{on})
		require.ErrorContains(t, err, refusal, on)
	}
	_, err = (&Engine{}).Environments(t.Context(), nil)
	require.ErrorContains(t, err, "a check needs somewhere to build")
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
	e := &Engine{Providers: map[string]provider.Provider{"tart": &releasing{}, "github": &scriptedProvider{}}}
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
