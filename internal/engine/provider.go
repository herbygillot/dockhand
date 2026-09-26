package engine

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/herbygillot/dockhand/internal/macos"
	"github.com/herbygillot/dockhand/internal/model"
)

// Provider builds a plan's targets in one environment (Design v3 §7):
// tart, prefix, github, or a person's own command.
type Provider interface {
	// Name is how people select it with --on.
	Name() string
	// Execute builds the job's targets in order, recording each target's
	// result through the build as it finishes. An error is trouble with
	// the environment itself; a target that fails to build is a result,
	// not an error.
	Execute(ctx context.Context, job Job, build Build) error
}

// ErrInfrastructure marks trouble with a provider's environment rather
// than with what it built: a VM that would not start, a script that wrote
// no result. The runner tries such an execution again, up to
// model.MaxAttempts; it never repeats a verdict.
var ErrInfrastructure = errors.New("provider infrastructure failed")

// Job is one guest execution's work.
type Job struct {
	Run         model.Run
	Execution   model.GuestExecution
	Revision    model.Revision
	Plan        model.Plan
	Environment model.Environment
	// Targets are the plan's targets still without a complete verdict,
	// in order.
	Targets []model.PlanTarget
	// Commit holds the revision's files: the commit itself, or for a
	// snapshot a commit made of its tree on the base, which only the
	// provider sees.
	Commit string
	// Directory is where the execution may keep files, such as logs.
	Directory string
}

// Build is how a provider learns what to skip and records what happened.
type Build interface {
	// Blocked names a changed dependency of the target that did not pass,
	// when there is one; the provider records the target as blocked
	// rather than building it.
	Blocked(target model.TargetID) (model.TargetID, bool)
	// Record checkpoints one target's result. A complete verdict is final.
	Record(result model.TargetResult) error
	// Progress reports a step to whoever is watching.
	Progress(message string)
	// Canceled reports, once the context is done, that the run was
	// canceled or interrupted for good, rather than its driver stopping
	// with the run left for the next one: only then does a provider stop
	// work it started elsewhere.
	Canceled() bool
}

// A ReleaseProvider builds on releases a person names, as Tart does: each
// release --on <provider>:<releases> selects is an environment of its own.
type ReleaseProvider interface {
	Provider
	// Platforms are the platforms the releases select: the provider's
	// default with none, as Tart builds on the Mac's own release.
	Platforms(ctx context.Context, releases string) ([]model.Platform, error)
}

// A Skipper is a provider that won't build some targets in an environment,
// and can say so before a check starts, as Tart won't build a target that
// needs Xcode on a release with no Xcode image. It records each as not run,
// with the Skip's reason as the result's detail, and so what depends on it
// (SkipDependents).
type Skipper interface {
	Provider
	Skips(ctx context.Context, plan model.Plan, environment model.Environment) ([]Skip, error)
}

// Skip is a target a provider won't build in an environment.
type Skip struct {
	Target      model.TargetID
	Environment model.Environment
	// Reason is why, in a few words: "needs Xcode". Remedy, when there is
	// one, is what would let it be built.
	Reason, Remedy string
	// Because names the target a dependent needs, which isn't built; the
	// provider's own skips have none.
	Because model.TargetID
}

// Skips are the targets a plan's providers won't build, and what depends
// on them in each environment, for the plan to show before the check
// starts. The provider decides again when it builds.
func (e *Engine) Skips(ctx context.Context, plan model.Plan) ([]Skip, error) {
	var skips []Skip
	for _, environment := range plan.Environments {
		skipper, ok := e.Providers[environment.Provider].(Skipper)
		if !ok {
			continue
		}
		own, err := skipper.Skips(ctx, plan, environment)
		if err != nil {
			return nil, err
		}
		skips = append(skips, SkipDependents(plan, environment, plan.Targets, own)...)
	}
	return skips, nil
}

// SkipDependents adds to a provider's skips in an environment the targets
// that depend on them, directly or not, each not run for the one it needs:
// it is neither built against an old build of that target nor counted as
// failed.
func SkipDependents(plan model.Plan, environment model.Environment, targets []model.PlanTarget, own []Skip) []Skip {
	skips := slices.Clone(own)
	skipped := map[model.TargetID]bool{}
	for _, skip := range own {
		skipped[skip.Target] = true
	}
	// Targets are in dependency order, so a dependency is settled before
	// its dependents.
	for _, target := range targets {
		if skipped[target.ID] || Excluded(plan, target, environment.Platform) {
			continue
		}
		for _, dependency := range plan.DependsOnIn(environment, target.ID) {
			if skipped[dependency] {
				skipped[target.ID] = true
				skips = append(skips, Skip{Target: target.ID, Environment: environment, Reason: "needs " + string(dependency) + ", which isn't built", Because: dependency})
				break
			}
		}
	}
	return skips
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
		if tart, ok := e.Providers["tart"].(ReleaseProvider); ok {
			if platforms, err := tart.Platforms(ctx, ""); err == nil {
				return []model.Environment{{Provider: "tart", Platform: platforms[0]}}, nil
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
		provider, ok := e.Providers[name]
		if !ok {
			switch name {
			case "tart":
				return nil, fmt.Errorf("--on %s: Tart isn't installed here; MacPorts' tart port installs it", value)
			case "prefix":
				return nil, fmt.Errorf("--on %s: the prefix provider is not in v3 yet; use Tart or your own script (--on command) meanwhile", value)
			}
			return nil, fmt.Errorf("--on %s: no provider %q is set up", value, name)
		}
		if releaser, ok := provider.(ReleaseProvider); ok {
			platforms, err := releaser.Platforms(ctx, releases)
			if err != nil {
				return nil, err
			}
			for _, platform := range platforms {
				add(model.Environment{Provider: name, Platform: platform})
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
