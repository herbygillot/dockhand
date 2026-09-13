package workflow

import (
	"errors"
	"time"

	"github.com/herbygillot/dockhand/v2/internal/git"
	"github.com/herbygillot/dockhand/v2/internal/macports"
	"github.com/herbygillot/dockhand/v2/internal/prepare"
	"github.com/herbygillot/dockhand/v2/internal/publish"
	"github.com/herbygillot/dockhand/v2/internal/record"
	"github.com/herbygillot/dockhand/v2/internal/state"
	"github.com/herbygillot/dockhand/v2/internal/verify"
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
	// Preparer is reserved for the source-preparation execution path.
	Preparer *prepare.Service
	// Planner is reserved for broader coverage planning. The current cycle
	// uses verify.PlanSingle for its single-target plan.
	Planner *verify.Planner
	// Provider executes verification and resource operations during Cycle.
	// Submit, Control, and Status do not call it.
	Provider verify.Provider
	// Publisher is reserved for the publication execution path.
	Publisher *publish.Service
	// Now supplies timestamps and lease comparisons, converted to UTC. Nil uses
	// the system clock. Keep this callback nonblocking; transactions call it.
	Now func() time.Time
	// Owner identifies the claim owner. Empty generates an identity per cycle.
	// Sharing an owner identity does not permit replacing a live claim.
	Owner record.ProcessID
	// LeaseDuration defaults to two minutes and must exceed the effective
	// CallTimeout. Expiry permits recovery; it cannot stop an external call.
	LeaseDuration time.Duration
	// CallTimeout defaults to thirty seconds for each provider call. Providers
	// must honor the context deadline for it to bound their execution time.
	CallTimeout time.Duration
	// RetryDelay defaults to one second before another attempt or cleanup action.
	// Cycle records eligibility times and leaves waiting to its caller.
	RetryDelay time.Duration
}

// Scope selects all jobs or a nonempty list of explicit job IDs. Those forms
// are mutually exclusive. Status and Cycle collapse duplicate IDs and reject
// unknown jobs. All is limited to the engine's registered repository.
type Scope struct {
	All  bool
	Jobs []record.JobID
}

// now supplies a UTC timestamp without modifying the configured clock.
func (e *Engine) now() time.Time {
	if e.Now != nil {
		return e.Now().UTC().Truncate(time.Millisecond)
	}
	return time.Now().UTC().Truncate(time.Millisecond)
}
