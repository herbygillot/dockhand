package workflow

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/state"
	"github.com/herbygillot/dockhand/internal/verify"
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
	engine                      *Engine
	owner                       record.ProcessID
	grace, retry, observe, wait time.Duration
	timeouts                    Timeouts
	capabilities                verify.Capabilities
	providerError               error
	providerChecked             bool
	providerName                string
	provider                    verify.Provider
	providerResults             map[string]providerCheck
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
		var changed bool
		var detail string
		var err error
		switch job.Phase {
		case record.PhasePreparation:
			changed, detail, err = c.advancePreparation(ctx, job.ID)
		case record.PhaseVerification:
			if job.Spec.IncludeDependents {
				changed, detail, err = c.planDependents(ctx, job.ID)
			} else {
				changed, detail, err = c.advanceJob(ctx, job.ID)
			}
		case record.PhasePublication:
			changed, detail, err = c.advancePublication(ctx, job.ID)
		default:
			detail = fmt.Sprintf("workflow: job %s has invalid phase %q", job.ID, job.Phase)
		}
		if changed {
			result.Advanced = append(result.Advanced, job.ID)
		}
		if errors.Is(err, ErrClaimLost) || errors.Is(err, state.ErrConflict) || errors.Is(err, errNotImplemented) {
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
		c.checkProvider(ctx, resource.Handle.Provider)
		detail, err := c.cleanup(ctx, resource.ID)
		if errors.Is(err, ErrClaimLost) || errors.Is(err, state.ErrConflict) || errors.Is(err, errNotImplemented) {
			detail, err = err.Error(), nil
		}
		if err != nil {
			return result, err
		}
		if detail != "" {
			result.Problems = append(result.Problems, JobProblem{ResourceID: resource.ID, Detail: detail})
		}
	}
	if err = c.pruneDiagnostics(ctx, &result); err != nil {
		return result, err
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

type providerCheck struct {
	provider     verify.Provider
	capabilities verify.Capabilities
	err          error
}

// checkProvider observes each provider once per pass, outside write transactions.
func (c *cycle) checkProvider(ctx context.Context, name string) {
	if c.providerChecked && c.providerName == name {
		return
	}
	if value, ok := c.providerResults[name]; ok {
		c.provider, c.capabilities, c.providerError = value.provider, value.capabilities, value.err
		c.providerName, c.providerChecked = name, true
		return
	}
	if c.providerResults == nil {
		c.providerResults = map[string]providerCheck{}
	}
	defer func() { c.providerResults[name] = providerCheck{c.provider, c.capabilities, c.providerError} }()
	c.providerChecked = true
	c.providerName = name
	c.providerError = nil
	c.provider = c.engine.verificationProvider(name)
	if c.provider == nil {
		c.providerError = fmt.Errorf("workflow: verification provider %q is unavailable in this driver", name)
		return
	}
	callCtx, cancel := context.WithTimeout(ctx, c.timeouts.Observe)
	defer cancel()
	c.capabilities, c.providerError = c.provider.Capabilities(callCtx)
	if c.providerError == nil {
		c.providerError = callCtx.Err()
	}
}

// newCycle resolves zero-value defaults and checks timing before any progression.
// A generated owner belongs only to this pass and is not written back to Engine.
func (e *Engine) newCycle() (*cycle, error) {
	timeouts, err := e.Timeouts.defaults()
	if err != nil {
		return nil, err
	}
	c := &cycle{engine: e, owner: e.Owner, grace: e.LeaseGrace, timeouts: timeouts, retry: e.RetryDelay, observe: e.ObserveInterval, wait: e.WaitInterval}
	if c.grace == 0 {
		c.grace = 30 * time.Second
	}
	if c.retry == 0 {
		c.retry = time.Second
	}
	if c.wait == 0 {
		c.wait = 10 * time.Second
	}
	if c.observe == 0 {
		c.observe = 10 * time.Second
	}
	if c.grace < 0 || c.retry < 0 || c.observe < 0 || c.wait < 0 || e.now().IsZero() {
		return nil, fmt.Errorf("workflow: positive lease grace, retry, and observation intervals are required")
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
	if submitting && len(c.capabilities.Platforms) > 0 && !slices.Contains(c.capabilities.Platforms, config.Platform) {
		return fmt.Errorf("workflow: provider does not support requested platform %+v", config.Platform)
	}
	return nil
}

// due treats a missing retry time as immediately eligible.
func due(retry *time.Time, now time.Time) bool { return retry == nil || !retry.After(now) }

// claim advances a record's generation and creates a lease within the caller's
// transaction. The record must retain that generation after its claim is cleared
// so that a later claim by the same owner cannot accept an older result.
func (c *cycle) claim(generation *uint64, now time.Time, timeout time.Duration) (*record.Claim, error) {
	if *generation == ^uint64(0) {
		return nil, fmt.Errorf("workflow: claim generation exhausted")
	}
	(*generation)++
	return &record.Claim{Owner: c.owner, Generation: *generation, ExpiresAt: now.Add(timeout).Add(c.grace)}, nil
}
