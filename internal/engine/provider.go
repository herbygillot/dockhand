package engine

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/herbygillot/dockhand/internal/macos"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/provider"
)

// Remedy is how to give an environment what an unmet target needs, when
// its provider can say.
func (e *Engine) Remedy(unmet model.Unmet) string {
	if remedier, ok := e.Providers[unmet.Environment.Provider].(provider.Remedier); ok {
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
		if _, ok := e.Providers["command"]; ok {
			return []model.Environment{{Provider: "command"}}, nil
		}
		if tart, ok := e.Providers["tart"].(provider.ReleaseProvider); ok {
			if environments, err := tart.Environments(ctx, ""); err == nil {
				return environments[:1], nil
			}
		}
		return nil, errors.New(`a check needs somewhere to build: --on tart builds in a Tart image of this Mac's macOS, --on github with MacPorts' own workflow in your fork, and --on command with your own script, set up as [providers.command] run = "..." in ~/.dockhand/config.toml; [check] on = ["tart"] makes one the default`)
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
			if _, isRelease := e.Providers["tart"]; isRelease && releases == "" && knownRelease(name) {
				name, releases = "tart", name
			}
		}
		named, ok := e.Providers[name]
		if !ok {
			switch name {
			case "tart":
				return nil, fmt.Errorf("--on %s: Tart isn't installed here; MacPorts' tart port installs it", value)
			case "prefix":
				return nil, fmt.Errorf("--on %s: the prefix provider is not in v3 yet; use Tart or your own script (--on command) meanwhile", value)
			}
			return nil, fmt.Errorf("--on %s: no provider %q is set up", value, name)
		}
		if releaser, ok := named.(provider.ReleaseProvider); ok {
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
		case releases != "" && name == "github":
			return nil, fmt.Errorf("--on %s: the github provider builds on the runners MacPorts' workflow names, so it takes no releases", value)
		case releases != "":
			return nil, fmt.Errorf("--on %s: the %s provider builds wherever its script does, so it takes no releases", value, name)
		}
		add(model.Environment{Provider: name})
	}
	return environments, nil
}

// knownRelease reports whether a name is a macOS release, by name or
// product version.
func knownRelease(name string) bool {
	_, err := macos.ParseRelease(name)
	return err == nil
}
