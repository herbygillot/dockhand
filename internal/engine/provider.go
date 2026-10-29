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

// Environments turns --on values into the environments a check builds
// in: each a provider that is set up, with the releases it can take, and
// a bare release name meaning Tart. Where nothing names them, a build
// profile does (Where).
func (e *Engine) Environments(ctx context.Context, on []string) ([]model.Environment, error) {
	if len(on) == 0 {
		where, err := e.Where(ctx, WhereRequest{})
		return where.Environments, err
	}
	var environments []model.Environment
	add := func(environment model.Environment) {
		if !slices.Contains(environments, environment) {
			environments = append(environments, environment)
		}
	}
	values, err := e.onItems(on)
	if err != nil {
		return nil, err
	}
	for _, value := range values {
		if value == "ci" {
			// ci went with build profiles, with no alias (the person,
			// 2026-10-13).
			return nil, fmt.Errorf("--on ci is gone: --build macports-ci builds on the releases MacPorts' CI does")
		}
		name, releases, _ := strings.Cut(value, ":")
		if _, ok := e.Providers[name]; !ok {
			if _, isRelease := e.Providers[buildenv.Tart]; isRelease && releases == "" && knownRelease(name) {
				name, releases = buildenv.Tart, name
			}
		}
		provider, ok := e.Providers[name]
		if ok && name == buildenv.GitHub {
			// A release MacPorts' workflow builds on is an environment of
			// its own (githubEnvironments).
			found, err := e.githubEnvironments(ctx, releases)
			if err != nil {
				return nil, err
			}
			for _, environment := range found {
				add(environment)
			}
			continue
		}
		if !ok {
			switch name {
			case buildenv.Tart:
				return nil, fmt.Errorf("--on %s: Tart isn't installed here; MacPorts' tart port installs it", value)
			case buildenv.Local:
				return nil, fmt.Errorf("--on %s: the local provider builds in a MacPorts prefix on a Mac, and this isn't one", value)
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
		if releases != "" {
			return nil, fmt.Errorf("--on %s: the %s provider builds wherever its script does, so it takes no releases", value, name)
		}
		add(model.Environment{Provider: name})
	}
	return environments, nil
}

// onForms are the forms --on takes, said where a value is none of them.
const onForms = "--on takes a provider, tart, github, command, or local; a provider with its releases or prefix, tart:12,26 or local:/opt/macports-test; or a release, tahoe or 26, which is Tart's"

// onItems are --on's values with each comma-separated item that names a
// provider starting a value of its own: tart:12,tart:26 handed tart
// "12,tart:26", refused as an unknown release, where tart:12,26 worked
// (the abook session, 2026-10-28). An item after a comma that names
// neither a provider nor a release, where the value's provider takes
// releases, is refused with the forms --on takes.
func (e *Engine) onItems(on []string) ([]string, error) {
	provider := func(item string) bool {
		name, _, _ := strings.Cut(item, ":")
		_, set := e.Providers[name]
		return set || slices.Contains([]string{buildenv.Tart, buildenv.GitHub, buildenv.Command, buildenv.Local}, name)
	}
	var values []string
	for _, value := range on {
		items := strings.Split(value, ",")
		current := items[0]
		for _, item := range items[1:] {
			switch {
			case provider(item):
				values = append(values, current)
				current = item
			case item == "all" || knownRelease(item):
				current += "," + item
			default:
				return nil, fmt.Errorf("--on %s: %q is neither a provider nor a release; %s", value, item, onForms)
			}
		}
		values = append(values, current)
	}
	return values, nil
}

// OnValues are environments as --on names them, as Environments reads
// them back: Tart's and GitHub's releases together by their product
// versions, tart:12,26 and github:15,26, a local check's prefix,
// local:/opt/local, and another provider by its name, in the order the
// environments first name each. False where a release can't be named, as
// one this dockhand doesn't know can't.
func OnValues(environments []model.Environment) ([]string, bool) {
	var values []string
	releases := map[string][]string{}
	for _, environment := range environments {
		// Tart's and GitHub's are named by their releases, tart:12,26 and
		// github:15; a GitHub check's from before it had them by its name.
		named := environment.Provider == buildenv.Tart || environment.Provider == buildenv.GitHub && environment.Platform.Version != ""
		if !named {
			// A local check's prefix is named as --on names it.
			value := providerPlace(environment)
			if !slices.Contains(values, value) {
				values = append(values, value)
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
		if _, ok := releases[environment.Provider]; !ok {
			values = append(values, environment.Provider)
		}
		if !slices.Contains(releases[environment.Provider], release.Product) {
			releases[environment.Provider] = append(releases[environment.Provider], release.Product)
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
