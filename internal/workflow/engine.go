package workflow

import (
	"context"
	"errors"
	"fmt"
	"github.com/herbygillot/dockhand/internal/macports/workspace"
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
	// errNotImplemented identifies work whose executor is not implemented.
	// Cycle reports it as a job problem without discarding the accepted request.
	errNotImplemented = errors.New("workflow: job execution is not implemented")
	// errNoState means the engine or its state dependency is missing.
	errNoState = errors.New("workflow: state store and repository are required")
	// errUnsupportedAction means intake does not yet support the requested action or control.
	errUnsupportedAction = errors.New("workflow: action intake is not implemented")
	// ErrRequestConflict means a request ID already identifies different intent.
	// Job and control requests share the workflow request-ID namespace.
	ErrRequestConflict = state.ErrConflict
	// ErrStaleRevision means a modifying or publication request selected
	// a revision that is no longer the change's current revision.
	ErrStaleRevision = errors.New("workflow: selected revision is stale")
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
	// ErrContinuation means a port's open contribution met a master or a PR
	// that changed what continuing it would mean, and a person decides.
	ErrContinuation = errors.New("workflow: the open contribution needs a decision")
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
	// Workspaces hands out one projection per source to the bindings that
	// evaluate a selection; nil opens one per binding.
	Workspaces *workspace.Registry
	// Preparer produces immutable candidate trees without adopting branches.
	Preparer   SourcePreparer
	Dependents DependentDiscoverer
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
	// RetryDelay is the initial failure backoff and cancellation retry delay.
	RetryDelay time.Duration
	// PullRequestInterval bounds how often a whole-repository cycle re-observes
	// an open contribution's pull request; zero means defaultPullRequestInterval.
	PullRequestInterval time.Duration
	// WaitInterval controls expected capacity, discovery and publication waiting.
	WaitInterval time.Duration
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

// verificationProvider resolves persisted work independently of CLI defaults.
// A configured registry is authoritative; Provider supports single-provider engines.
func (e *Engine) verificationProvider(name string) verify.Provider {
	if e.Providers != nil {
		return e.Providers[name]
	}
	return e.Provider
}

const (
	// defaultPullRequestInterval is how long a cycle leaves an open PR unobserved.
	defaultPullRequestInterval = 5 * time.Minute
	// observationsPerCycle bounds forge calls in one cycle.
	observationsPerCycle = 4
)

// requireRepository checks that the Git repository the engine drives is the
// one its state is scoped to; scope words the refusal for the caller.
func (e *Engine) requireRepository(ctx context.Context, scope string) error {
	registered, err := e.State.FindRepository(ctx, e.Repo.CommonDir)
	if err != nil {
		return err
	}
	if registered.ID != e.Repository {
		if scope == "" {
			return ErrInvalidRequest
		}
		return fmt.Errorf("%w: %s", ErrInvalidRequest, scope)
	}
	return nil
}

// publicationDestinationFor resolves where a publication goes: the attached
// pull request's fork and base when there is one, the checkout's remotes
// otherwise.
func (e *Engine) publicationDestinationFor(ctx context.Context, attached *record.PullRequest, options publish.Options) (record.PublicationDestination, error) {
	if e.Publisher == nil {
		return record.PublicationDestination{}, fmt.Errorf("workflow: publisher required")
	}
	timeouts, err := e.Timeouts.defaults()
	if err != nil {
		return record.PublicationDestination{}, err
	}
	call, cancel := context.WithTimeout(ctx, timeouts.Publish)
	defer cancel()
	if err := e.Publisher.Preflight(call); err != nil {
		return record.PublicationDestination{}, fmt.Errorf("%w: %w", ErrPublicationIntake, err)
	}
	var destination record.PublicationDestination
	if attached != nil {
		destination, err = e.Publisher.DestinationFor(call, *attached, options)
	} else {
		destination, err = e.Publisher.Destination(call, options)
	}
	if err != nil {
		return record.PublicationDestination{}, fmt.Errorf("%w: %w", ErrPublicationIntake, err)
	}
	return destination, nil
}
