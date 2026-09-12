package workflow

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/herbygillot/dockhand/v2/internal/record"
	"github.com/herbygillot/dockhand/v2/internal/state"
	"github.com/herbygillot/dockhand/v2/internal/verify"
)

// JobProblem reports an issue with one job or resource during a cycle.
// JobID is set for job advancement; ResourceID is set for cleanup. These are
// per-item reports, including lost claims, rather than errors that abort the pass.
type JobProblem struct {
	JobID      record.JobID
	ResourceID record.ResourceID
	Detail     string
}

// CycleResult summarizes a reconciliation pass. It spans several transactions
// and reads; use [Engine.Status] for a consistent view of durable progress.
// If [Engine.Cycle] returns an error, the result can describe only part of the pass.
type CycleResult struct {
	// Advanced lists jobs whose advancement handler recorded changes. It does
	// not imply success or completion and excludes standalone control and cleanup writes.
	Advanced []record.JobID
	Problems []JobProblem
	// PendingCleanup lists retained, uncertain, or release-requested resources
	// in the last state view. Some may not yet be eligible for release.
	PendingCleanup []record.ResourceID
}

// cycle holds one pass's effective timing, claim owner, and provider capability
// observation. It carries no durable state between calls to Engine.Cycle.
type cycle struct {
	engine                *Engine
	owner                 record.ProcessID
	lease, timeout, retry time.Duration
	capabilities          verify.Capabilities
	providerError         error
	providerChecked       bool
}

func (e *Engine) Cycle(ctx context.Context, scope Scope) (CycleResult, error) {
	result := CycleResult{Advanced: []record.JobID{}, Problems: []JobProblem{}, PendingCleanup: []record.ResourceID{}}
	if err := e.checkScope(scope); err != nil {
		return result, err
	}
	c, err := e.newCycle()
	if err != nil {
		return result, err
	}
	selection := scope.Jobs
	if scope.All {
		selection = nil
	}
	var controls []record.ControlRequest
	err = e.State.View(ctx, e.Repository, func(ctx context.Context, r state.Reader) error {
		if err := checkJobs(ctx, r, scope); err != nil {
			return err
		}
		var err error
		controls, err = r.Controls(ctx, state.Query{Jobs: selection, Pending: true, Limit: 64})
		return err
	})
	if err != nil {
		return result, err
	}
	if err = e.applyControls(ctx, scope, controls); err != nil {
		return result, err
	}
	var jobs []record.Job
	now := e.now()
	err = e.State.View(ctx, e.Repository, func(ctx context.Context, r state.Reader) error {
		var err error
		jobs, err = r.Jobs(ctx, state.Query{Jobs: selection, DueBefore: &now, Limit: 64})
		return err
	})
	if err != nil {
		return result, err
	}
	for _, job := range jobs {
		if job.Spec.Action == record.Verify {
			c.checkProvider(ctx)
		}
		changed, detail, err := c.advanceJob(ctx, job.ID)
		if changed {
			result.Advanced = append(result.Advanced, job.ID)
		}
		if errors.Is(err, ErrClaimLost) || errors.Is(err, state.ErrConflict) || errors.Is(err, ErrNotImplemented) {
			detail, err = err.Error(), nil
		}
		if err != nil {
			return result, err
		}
		if detail != "" {
			result.Problems = append(result.Problems, JobProblem{JobID: job.ID, Detail: detail})
		}
	}
	var resources []record.Resource
	now = e.now()
	err = e.State.View(ctx, e.Repository, func(ctx context.Context, r state.Reader) error {
		var err error
		resources, err = r.Resources(ctx, state.Query{Jobs: selection, DueBefore: &now, Limit: 64})
		return err
	})
	if err != nil {
		return result, err
	}
	for _, resource := range resources {
		c.checkProvider(ctx)
		detail, err := c.cleanup(ctx, resource.ID)
		if errors.Is(err, ErrClaimLost) || errors.Is(err, state.ErrConflict) || errors.Is(err, ErrNotImplemented) {
			detail, err = err.Error(), nil
		}
		if err != nil {
			return result, err
		}
		if detail != "" {
			result.Problems = append(result.Problems, JobProblem{ResourceID: resource.ID, Detail: detail})
		}
	}
	err = e.State.View(ctx, e.Repository, func(ctx context.Context, r state.Reader) error {
		q := state.Query{Jobs: selection, Pending: true, Limit: 64}
		for {
			rs, err := r.Resources(ctx, q)
			if err != nil {
				return err
			}
			for _, v := range rs {
				result.PendingCleanup = append(result.PendingCleanup, v.ID)
			}
			if len(rs) < q.Limit {
				break
			}
			q.After = string(rs[len(rs)-1].ID)
		}
		return nil
	})
	return result, err
}

// checkProvider observes capabilities once, only when this pass has a candidate
// action. It runs before the handler's transaction, outside the write transaction.
func (c *cycle) checkProvider(ctx context.Context) {
	if c.providerChecked {
		return
	}
	c.providerChecked = true
	if c.engine.Provider == nil {
		c.providerError = fmt.Errorf("workflow: verification provider is required")
		return
	}
	callCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	c.capabilities, c.providerError = c.engine.Provider.Capabilities(callCtx)
	if c.providerError == nil {
		c.providerError = callCtx.Err()
	}
}

// newCycle resolves zero-value defaults and checks timing before any progression.
// A generated owner belongs only to this pass and is not written back to Engine.
func (e *Engine) newCycle() (*cycle, error) {
	c := &cycle{engine: e, owner: e.Owner, lease: e.LeaseDuration, timeout: e.CallTimeout, retry: e.RetryDelay}
	if c.lease == 0 {
		c.lease = 2 * time.Minute
	}
	if c.timeout == 0 {
		c.timeout = 30 * time.Second
	}
	if c.retry == 0 {
		c.retry = time.Second
	}
	if c.timeout < 0 || c.retry < 0 || c.lease <= c.timeout || e.now().IsZero() {
		return nil, fmt.Errorf("workflow: positive retry/timeout and a lease longer than the provider timeout are required")
	}
	if c.owner == "" {
		c.owner = record.ProcessID("cycle_" + rand.Text())
	}
	return c, nil
}

// providerReady checks the recorded provider binding and, for new submissions,
// platform compatibility using this pass's capability observation. The provider
// must decide capacity atomically during submission; this check is advisory.
func (c *cycle) providerReady(config record.BuildConfig, submitting bool) error {
	if c.providerError != nil {
		return c.providerError
	}
	if config.Provider != c.capabilities.Name {
		return fmt.Errorf("workflow: request requires provider %q, configured provider is %q", config.Provider, c.capabilities.Name)
	}
	if submitting && !slices.Contains(c.capabilities.Platforms, config.Platform) {
		return fmt.Errorf("workflow: provider does not support requested platform %+v", config.Platform)
	}
	return nil
}

// jobTerminal identifies states that the current driver will not advance further.
func jobTerminal(state record.JobState) bool {
	switch state {
	case record.JobCompleted, record.JobFailed, record.JobNeedsAttention, record.JobCanceled, record.JobSuperseded:
		return true
	default:
		return false
	}
}

// attemptTerminal identifies settled attempts whose resource disposition can be considered for cleanup.
func attemptTerminal(state record.AttemptState) bool {
	return state == record.AttemptFinished || state == record.AttemptCanceled
}

// live requires a nonnil claim whose expiry is strictly later than now.
func live(claim *record.Claim, now time.Time) bool { return claim != nil && claim.ExpiresAt.After(now) }

// due treats a missing retry time as immediately eligible.
func due(retry *time.Time, now time.Time) bool { return retry == nil || !retry.After(now) }

// owns checks the live claim's owner and generation. Callers separately check
// the record's lifecycle state before adopting an external result.
func owns(current, expected *record.Claim, now time.Time) bool {
	return live(current, now) && expected != nil && current.Owner == expected.Owner && current.Generation == expected.Generation
}

// claim advances a record's generation and creates a lease within the caller's
// transaction. The record must retain that generation after its claim is cleared
// so that a later claim by the same owner cannot accept an older result.
func (c *cycle) claim(generation *uint64, now time.Time) (*record.Claim, error) {
	if *generation == ^uint64(0) {
		return nil, fmt.Errorf("workflow: claim generation exhausted")
	}
	(*generation)++
	return &record.Claim{Owner: c.owner, Generation: *generation, ExpiresAt: now.Add(c.lease)}, nil
}
