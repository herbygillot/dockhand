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
	// Observe records what the environment reported about itself, for
	// the pull request's Tested on.
	Observe(observed model.Observed) error
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
	// Environments are the environments the releases select: the
	// provider's default with none, as Tart builds on the Mac's own
	// release. Each states its developer tools where the provider knows
	// them.
	Environments(ctx context.Context, releases string) ([]model.Environment, error)
}

// A Remedier is a provider that can say how to give an environment what an
// unmet target needs: Tart, the command that makes a release's Xcode image.
type Remedier interface {
	Provider
	Remedy(unmet model.Unmet) string
}

// Remedy is how to give an environment what an unmet target needs, when
// its provider can say.
func (e *Engine) Remedy(unmet model.Unmet) string {
	if remedier, ok := e.Providers[unmet.Environment.Provider].(Remedier); ok {
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
		if tart, ok := e.Providers["tart"].(ReleaseProvider); ok {
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
