// Package buildenv is the contract between the engine and the providers
// of build environments a check builds in (Design v3 §7): tart, github,
// and a person's own command, in internal/provider. A provider is given a
// Job, one guest execution's work, and reports what happened through a
// Build; what else it can do, it says by the interfaces it implements.
// Providers import this package and model, never the engine, which
// drives them; the command layer composes them.
package buildenv

import (
	"context"
	"errors"

	"github.com/herbygillot/dockhand/internal/model"
)

// Provider builds a plan's targets in one environment.
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
	// Targets are the plan's targets still without a complete verdict
	// here, in this environment's order.
	Targets []Target
	// Commit holds the revision's files: the commit itself, or for a
	// snapshot a commit made of its tree on the base, which only the
	// provider sees.
	Commit string
	// Directory is where the execution may keep files, such as logs.
	Directory string
}

// Target is one target of a job, with what it needs built first in the
// job's environment.
type Target struct {
	model.PlanTarget
	DependsOn []model.TargetID
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
	// Refer records the provider's own name for this run: a workflow
	// run's URL, a VM clone's name. dockhand logs finds the run by it.
	Refer(ref string) error
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

// An OwnTestsProvider runs a port's declared tests whatever a check's
// policy, as GitHub's workflow does: it is MacPorts' own, and dockhand
// doesn't change it. Under --tests skip, the tests still run there and
// don't count.
type OwnTestsProvider interface {
	Provider
	RunsOwnTests() bool
}

// A LeftoverProvider makes environments that can outlast the process that
// made them, as a Tart clone does when the process checking in it dies and
// no later attempt of its run comes to remove it. It lists them, each by
// the reference its execution recorded, and removes one the engine has
// found no process using.
type LeftoverProvider interface {
	Provider
	// Leftovers are the environments it made that are still there.
	Leftovers(ctx context.Context) ([]Leftover, error)
	// RemoveLeftover removes one, and refuses anything it didn't make for
	// a check.
	RemoveLeftover(ctx context.Context, ref string) error
}

// Leftover is an environment a provider made for a check that is still
// there, such as a Tart clone.
type Leftover struct {
	// Provider made it, and sets it; Ref is its name for it, as the
	// check's execution recorded it; What says it for a person, "Tart
	// clone dockhand-…".
	Provider string
	Ref      string
	What     string
}

// Fork is your fork of MacPorts' repository, and the Git remote that
// pushes to it: where the github provider pushes a check's commit, and
// submit a branch.
type Fork struct {
	// Repository is its GitHub name, such as ada/macports-ports.
	Repository string
	Remote     string
	PushURL    string
}

// CheckBranchPrefix names the branches a check pushes to your fork, as the
// github provider does; clean removes a merged branch's.
const CheckBranchPrefix = "dockhand-check/"
