package engine

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/herbygillot/dockhand/internal/buildenv"
	"github.com/herbygillot/dockhand/internal/macos"
	"github.com/herbygillot/dockhand/internal/model"
)

// Remedy is how to give an environment what an unmet target needs, when
// its provider can say.
func (e *Engine) Remedy(unmet model.Unmet) string {
	if remedier, ok := e.Providers[unmet.Environment.Provider].(buildenv.Remedier); ok {
		return remedier.Remedy(unmet)
	}
	return ""
}

// Environments turns --on values, or check.on's, into the environments a
// check builds in: each a provider that is set up, with the releases it
// can take, and a bare release name meaning Tart. With none, the command
// provider when it is set up, and otherwise Tart on this Mac's release.
func (e *Engine) Environments(ctx context.Context, on []string) ([]model.Environment, error) {
	if len(on) == 0 {
		if _, ok := e.Providers[buildenv.Command]; ok {
			return []model.Environment{{Provider: buildenv.Command}}, nil
		}
		if tart, ok := e.Providers[buildenv.Tart].(buildenv.ReleaseProvider); ok {
			if environments, err := tart.Environments(ctx, ""); err == nil {
				return environments[:1], nil
			}
		}
		return nil, fmt.Errorf(`a check needs somewhere to build: --on tart builds in a Tart image of this Mac's macOS, --on github with MacPorts' own workflow in your fork, and --on command with your own script, set up as [providers.command] run = "..." in %s; [check] on = ["tart"] makes one the default`, e.configFile())
	}
	var environments []model.Environment
	add := func(environment model.Environment) {
		if !slices.Contains(environments, environment) {
			environments = append(environments, environment)
		}
	}
	for _, value := range on {
		name, releases, _ := strings.Cut(value, ":")
		if _, ok := e.Providers[name]; !ok {
			if _, isRelease := e.Providers[buildenv.Tart]; isRelease && releases == "" && knownRelease(name) {
				name, releases = buildenv.Tart, name
			}
		}
		provider, ok := e.Providers[name]
		if !ok {
			switch name {
			case buildenv.Tart:
				return nil, fmt.Errorf("--on %s: Tart isn't installed here; MacPorts' tart port installs it", value)
			case buildenv.Prefix:
				return nil, fmt.Errorf("--on %s: the prefix provider is not in v3 yet; use Tart or your own script (--on command) meanwhile", value)
			}
			return nil, fmt.Errorf("--on %s: no provider %q is set up", value, name)
		}
		if releaser, ok := provider.(buildenv.ReleaseProvider); ok {
			found, err := releaser.Environments(ctx, releases)
			if err != nil {
				return nil, err
			}
			for _, environment := range found {
				add(environment)
			}
			continue
		}
		switch {
		case releases != "" && name == buildenv.GitHub:
			return nil, fmt.Errorf("--on %s: the github provider builds on the runners MacPorts' workflow names, so it takes no releases", value)
		case releases != "":
			return nil, fmt.Errorf("--on %s: the %s provider builds wherever its script does, so it takes no releases", value, name)
		}
		add(model.Environment{Provider: name})
	}
	return environments, nil
}

// OnValues are environments as --on names them, as Environments reads
// them back: Tart's releases together by their product versions,
// tart:12,26, and another provider by its name, in the order the
// environments first name each. False where a release can't be named, as
// one this dockhand doesn't know can't.
func OnValues(environments []model.Environment) ([]string, bool) {
	var values []string
	releases := map[string][]string{}
	for _, environment := range environments {
		if environment.Provider != buildenv.Tart {
			if !slices.Contains(values, environment.Provider) {
				values = append(values, environment.Provider)
			}
			continue
		}
		darwin, err := strconv.Atoi(environment.Platform.Version)
		if err != nil {
			return nil, false
		}
		release, err := macos.ReleaseForDarwin(darwin)
		if err != nil {
			return nil, false
		}
		if _, ok := releases[buildenv.Tart]; !ok {
			values = append(values, buildenv.Tart)
		}
		if !slices.Contains(releases[buildenv.Tart], release.Product) {
			releases[buildenv.Tart] = append(releases[buildenv.Tart], release.Product)
		}
	}
	for i, value := range values {
		if names, ok := releases[value]; ok {
			values[i] = value + ":" + strings.Join(names, ",")
		}
	}
	return values, true
}

// knownRelease reports whether a name is a macOS release, by name or
// product version.
func knownRelease(name string) bool {
	_, err := macos.ParseRelease(name)
	return err == nil
}
