package engine

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/herbygillot/dockhand/internal/buildenv"
	"github.com/herbygillot/dockhand/internal/model"
)

// Profile is a build profile: a provider and the releases it builds on,
// a list or a rule (the person's design, 2026-10-13).
type Profile struct {
	Name     string
	Provider string
	// Releases are an explicit list, as "15" or "tahoe"; Rule a rule's
	// name, host, minmax, macports-ci, or all, where there's no list.
	Releases []string
	Rule     string
}

// The release rules a profile can name.
const (
	RuleHost       = "host"
	RuleMinMax     = "minmax"
	RuleMacPortsCI = "macports-ci"
	RuleAll        = "all"
)

// Builds are where checks build, as the configuration sets it: the
// profile a person's check takes, the one a check nobody looks over
// takes, and the person's own profiles, by name.
type Builds struct {
	Default, Unattended string
	Profiles            map[string]Profile
}

// builtinProfile is one of dockhand's own profiles, all on Tart, each
// named for its rule, local being host's other name; and command, the
// person's own script, where it's set up.
func builtinProfile(name string) (Profile, bool) {
	switch name {
	case RuleHost, "local":
		return Profile{Name: name, Provider: buildenv.Tart, Rule: RuleHost}, true
	case RuleMinMax, RuleMacPortsCI, RuleAll:
		return Profile{Name: name, Provider: buildenv.Tart, Rule: name}, true
	case buildenv.Command:
		return Profile{Name: name, Provider: buildenv.Command}, true
	}
	return Profile{}, false
}

// Profile is the profile a name means: the person's own, else dockhand's.
func (e *Engine) Profile(name string) (Profile, error) {
	if profile, ok := e.Builds.Profiles[name]; ok {
		profile.Name = name
		if profile.Provider == "" {
			profile.Provider = buildenv.Tart
		}
		return profile, nil
	}
	if profile, ok := builtinProfile(name); ok {
		return profile, nil
	}
	return Profile{}, fmt.Errorf("--build %s: no profile of that name; dockhand's are host, minmax, macports-ci, and all, and yours go under [build.profiles] in %s", name, e.configFile())
}

// Where is where a check builds: the environments, the profile that
// named them, empty where --on did, and whether each port builds only on
// its oldest and newest of them (minmax).
type Where struct {
	Profile      string
	Environments []model.Environment
	Span         bool
}

// WhereRequest is how a check was told where: --on's explicit
// environments, else --build's profile, else build.unattended for a check
// nobody looks over before it's submitted, and build.default for the rest
// (the command package review's CheckDefaults, which build profiles are).
type WhereRequest struct {
	On         []string
	Build      string
	Unattended bool
}

// Explicit reports whether the request names where, --on or --build,
// rather than taking a default.
func (r WhereRequest) Explicit() bool { return len(r.On) > 0 || r.Build != "" }

// Where resolves where a check builds.
func (e *Engine) Where(ctx context.Context, request WhereRequest) (Where, error) {
	if len(request.On) > 0 {
		environments, err := e.Environments(ctx, request.On)
		return Where{Environments: environments}, err
	}
	name := request.Build
	if name == "" && request.Unattended {
		name = e.unattendedProfile()
	}
	if name == "" {
		name = e.defaultProfile()
	}
	profile, err := e.Profile(name)
	if err != nil {
		return Where{}, err
	}
	where := Where{Profile: profile.Name, Span: profile.Rule == RuleMinMax}
	where.Environments, err = e.profileEnvironments(ctx, profile)
	return where, err
}

// defaultProfile is build.default: host, or command where it's set up.
func (e *Engine) defaultProfile() string {
	if e.Builds.Default != "" {
		return e.Builds.Default
	}
	if _, ok := e.Providers[buildenv.Command]; ok {
		return buildenv.Command
	}
	return RuleHost
}

// unattendedProfile is build.unattended: minmax where Tart is set up, its
// rule's provider, and else build.default's, as on an Intel Mac, or a
// person's own script alone.
func (e *Engine) unattendedProfile() string {
	if e.Builds.Unattended != "" {
		return e.Builds.Unattended
	}
	if _, ok := e.Providers[buildenv.Tart]; ok {
		return RuleMinMax
	}
	return e.defaultProfile()
}

// minMaxFloor is the oldest release minmax builds on, macOS 12, Darwin
// 21: the person's design.
const minMaxFloor = 21

// profileEnvironments are a profile's environments: its provider's, on the
// releases its list or rule names.
func (e *Engine) profileEnvironments(ctx context.Context, profile Profile) ([]model.Environment, error) {
	provider, ok := e.Providers[profile.Provider]
	if !ok {
		return nil, e.notSetUp(ctx, profile)
	}
	releaser, ok := provider.(buildenv.ReleaseProvider)
	if !ok {
		// github builds on what MacPorts' workflow names, and a script
		// wherever it does: one environment each.
		return []model.Environment{{Provider: profile.Provider}}, nil
	}
	var releases string
	switch profile.Rule {
	case "":
		releases = strings.Join(profile.Releases, ",")
	case RuleHost:
	case RuleAll, RuleMinMax:
		releases = "all"
	case RuleMacPortsCI:
		slugs, err := e.ciSlugs(ctx)
		if err != nil {
			return nil, err
		}
		releases = strings.Join(slugs, ",")
	}
	environments, err := releaser.Environments(ctx, releases)
	if err != nil {
		return nil, fmt.Errorf("build profile %s: %w", profile.Name, err)
	}
	switch profile.Rule {
	case RuleHost:
		environments = environments[:min(1, len(environments))]
	case RuleMinMax:
		environments = slices.DeleteFunc(environments, func(environment model.Environment) bool {
			darwin, err := strconv.Atoi(environment.Platform.Version)
			return err == nil && darwin < minMaxFloor
		})
	}
	if len(environments) == 0 {
		return nil, fmt.Errorf("build profile %s: no release to build on", profile.Name)
	}
	return environments, nil
}

// notSetUp says a profile's provider isn't set up, with what sets it up:
// Tart's own words where it can say, as "dockhand setup tart golden-gate
// makes …" (the rc3 full run, 2026-10-06).
func (e *Engine) notSetUp(_ context.Context, profile Profile) error {
	switch profile.Provider {
	case buildenv.Tart:
		return fmt.Errorf(`build profile %s builds on Tart, which isn't installed here: MacPorts' tart port installs it, on Apple silicon; or --on github builds with MacPorts' own workflow in your fork, and --on command with your own script, set up as [providers.command]; a profile of your own under [build.profiles] in %s makes either the default, as {provider = "github"}`, profile.Name, e.configFile())
	case buildenv.Command:
		return fmt.Errorf(`build profile %s builds with your own script, which isn't set up: [providers.command] run = "..." in %s`, profile.Name, e.configFile())
	}
	return fmt.Errorf("build profile %s: no provider %q is set up", profile.Name, profile.Provider)
}
