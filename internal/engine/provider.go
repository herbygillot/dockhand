package engine

import (
	"context"
	"errors"

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
}
