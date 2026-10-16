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
	require.ErrorContains(t, err, "build profile host builds on Tart, which isn't installed here")
	_, err = e.Environments(t.Context(), []string{"ci"})
	require.EqualError(t, err, "--on ci is gone: --build macports-ci builds on the releases MacPorts' CI does")
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
	switch releases {
	case "":
		releases = "25"
	case "all":
		releases = "sonoma,tahoe"
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

// A check builds where a build profile says: build.default's, host unless
// set, build.unattended's for a check nobody looks over, minmax unless
// set, which spans each port's oldest and newest; a profile of the
// person's own; and --on's explicit environments over any of them (the
// person's design, 2026-10-13).
func TestWhereABuildProfileBuilds(t *testing.T) {
	t.Parallel()
	e := &Engine{Providers: map[string]buildenv.Provider{"tart": &releasing{}, "github": &scriptedProvider{}}}
	sonoma := model.Environment{Provider: "tart", Platform: model.Platform{OS: "darwin", Version: "23", Architecture: "arm64"}, DeveloperTools: model.DeveloperToolsCommandLine}
	tahoe := model.Environment{Provider: "tart", Platform: model.Platform{OS: "darwin", Version: "25", Architecture: "arm64"}, DeveloperTools: model.DeveloperToolsXcode}

	where, err := e.Where(t.Context(), WhereRequest{})
	require.NoError(t, err)
	require.Equal(t, Where{Profile: "host", Environments: []model.Environment{tahoe}}, where)
	where, err = e.Where(t.Context(), WhereRequest{Unattended: true})
	require.NoError(t, err)
	require.Equal(t, Where{Profile: "minmax", Environments: []model.Environment{sonoma, tahoe}, Span: true}, where)
	where, err = e.Where(t.Context(), WhereRequest{Build: "all"})
	require.NoError(t, err)
	require.Equal(t, Where{Profile: "all", Environments: []model.Environment{sonoma, tahoe}}, where)

	e.Builds = Builds{Default: "mine", Unattended: "fork", Profiles: map[string]Profile{"mine": {Releases: []string{"sonoma"}}, "fork": {Provider: "github"}}}
	where, err = e.Where(t.Context(), WhereRequest{})
	require.NoError(t, err)
	require.Equal(t, Where{Profile: "mine", Environments: []model.Environment{sonoma}}, where)
	where, err = e.Where(t.Context(), WhereRequest{Unattended: true})
	require.NoError(t, err)
	require.Equal(t, Where{Profile: "fork", Environments: []model.Environment{{Provider: "github"}}}, where)
	where, err = e.Where(t.Context(), WhereRequest{On: []string{"tahoe"}, Build: "mine"})
	require.NoError(t, err)
	require.Equal(t, Where{Environments: []model.Environment{tahoe}}, where, "--on's explicit environments")
	_, err = e.Where(t.Context(), WhereRequest{Build: "nosuch"})
	require.ErrorContains(t, err, "--build nosuch: no profile of that name")
	_, err = e.Where(t.Context(), WhereRequest{Build: "command"})
	require.ErrorContains(t, err, "build profile command builds with your own script, which isn't set up")
}
