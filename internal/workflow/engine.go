package workflow

import (
	"context"
	"errors"
	"time"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/publish"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/state"
	"github.com/herbygillot/dockhand/internal/verify"
	"github.com/herbygillot/dockhand/internal/workflow/preparation"
)

var (
	// ErrNotImplemented identifies work whose executor is not implemented.
	// Cycle reports it as a job problem without discarding the accepted request.
	ErrNotImplemented = errors.New("workflow: job execution is not implemented")
	// ErrNoState means the engine or its state dependency is missing.
	ErrNoState = errors.New("workflow: state store and repository are required")
	// ErrInvalidRequest means a request violates the intake contract.
	ErrInvalidRequest = errors.New("workflow: invalid request")
	// ErrUnsupportedAction means intake does not yet support the requested action or control.
	ErrUnsupportedAction = errors.New("workflow: action intake is not implemented")
	// ErrRequestConflict means a request ID already identifies different intent.
	// Job and control requests share the workflow request-ID namespace.
	ErrRequestConflict = state.ErrConflict
	// ErrStaleRevision means a modifying or publication request selected
	// a revision that is no longer the change's current revision.
	ErrStaleRevision = errors.New("workflow: selected revision is stale")
	// ErrNotFound means a requested job or referenced record does not exist.
	ErrNotFound = state.ErrNotFound
	// ErrClaimLost means an action can no longer adopt its result because
	// its claim expired, was replaced, or no longer matches the current state.
	// Cycle reports this as a problem and continues with independent work.
	ErrClaimLost = errors.New("workflow: action claim expired or was replaced")
	// ErrInvalidScope means a scope selects both all jobs and explicit IDs,
	// selects neither, or includes an empty job ID.
	ErrInvalidScope = errors.New("workflow: select all jobs or explicit job IDs")
	// ErrNoPendingJobs means a tracked contribution has no queued or active work
	// to attach to or cancel.
	ErrNoPendingJobs = errors.New("workflow: no pending jobs")
)

// Engine owns request intake, workflow advancement, and state projections.
// Configure it before use. Concurrent callers must leave its fields unchanged
// and provide dependencies and a clock that support concurrent calls.
type Engine struct {
	// State is required by every public operation.
	State      state.Store
	Repository record.RepositoryID
	// Repo and Ports support explicit branch binding before submission.
	Repo  *git.Repository
	Ports macports.Reader
	// Preparer produces immutable candidate trees without adopting branches.
	Preparer SourcePreparer
	// Releases resolves version-bump input for a durable checkpoint before preparation.
	Releases ReleaseResolver
	// Provider executes verification and resource operations during Cycle.
	// Submit, Control, and Status do not call it.
	Provider verify.Provider
	// Providers routes persisted names; Provider remains the single-provider fallback.
	Providers map[string]verify.Provider
	// Publisher resolves and executes Git/forge operations outside state transactions.
	Publisher *publish.Service
	// Now supplies timestamps and lease comparisons, converted to UTC. Nil uses
	// the system clock. Keep this callback nonblocking; transactions call it.
	Now func() time.Time
	// Owner identifies the claim owner. Empty generates an identity per cycle.
	// Sharing an owner identity does not permit replacing a live claim.
	Owner record.ProcessID
	// Timeouts bounds external operations independently of build execution time.
	Timeouts Timeouts
	// LeaseGrace leaves time to record a result after its operation deadline.
	LeaseGrace time.Duration
	// RetryDelay applies to admission, uncertain actions, errors, and cancellation.
	RetryDelay time.Duration
	// ObserveInterval schedules successful observations of running builds.
	ObserveInterval time.Duration
}

// now supplies a UTC timestamp without modifying the configured clock.
func (e *Engine) now() time.Time {
	if e.Now != nil {
		return e.Now().UTC().Truncate(time.Millisecond)
	}
	return time.Now().UTC().Truncate(time.Millisecond)
}

type ReleaseResolver interface {
	ResolveRelease(context.Context, preparation.Request) (record.Release, error)
}

type SourcePreparer interface {
	Prepare(context.Context, preparation.Request) (preparation.Result, error)
}

// VerificationProvider resolves persisted work independently of CLI defaults.
func (e *Engine) VerificationProvider(name string) verify.Provider {
	if p := e.Providers[name]; p != nil {
		return p
	}
	return e.Provider
}
