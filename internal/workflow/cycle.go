package workflow

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/herbygillot/dockhand/v2/internal/record"
	"github.com/herbygillot/dockhand/v2/internal/verify"
)

type JobProblem struct {
	JobID      record.JobID
	ResourceID record.ResourceID
	Detail     string
}

type CycleResult struct {
	Advanced       []record.JobID
	Problems       []JobProblem
	PendingCleanup []record.ResourceID
}

type cycle struct {
	engine                *Engine
	owner                 record.ProcessID
	lease, timeout, retry time.Duration
	capabilities          verify.Capabilities
	providerError         error
}

func (e *Engine) Cycle(ctx context.Context, scope Scope) (CycleResult, error) {
	result := CycleResult{Advanced: []record.JobID{}, Problems: []JobProblem{}, PendingCleanup: []record.ResourceID{}}
	status, err := e.Status(ctx, scope)
	if err != nil {
		return result, err
	}
	c, err := e.newCycle()
	if err != nil {
		return result, err
	}
	if len(status.Jobs) == 0 && len(status.Resources) == 0 {
		return result, nil
	}
	selected := make(map[record.JobID]bool, len(status.Jobs))
	for _, item := range status.Jobs {
		selected[item.Job.ID] = true
	}
	if err := e.applyControls(ctx, selected); err != nil {
		return result, err
	}
	if e.Provider == nil {
		c.providerError = fmt.Errorf("workflow: verification provider is required")
	} else {
		callCtx, cancel := context.WithTimeout(ctx, c.timeout)
		c.capabilities, c.providerError = e.Provider.Capabilities(callCtx)
		if c.providerError == nil {
			c.providerError = callCtx.Err()
		}
		cancel()
	}
	for _, item := range status.Jobs {
		changed, detail, err := c.advanceJob(ctx, item.Job.ID)
		if changed {
			result.Advanced = append(result.Advanced, item.Job.ID)
		}
		if errors.Is(err, ErrClaimLost) {
			detail, err = err.Error(), nil
		}
		if err != nil {
			return result, err
		}
		if detail != "" {
			result.Problems = append(result.Problems, JobProblem{JobID: item.Job.ID, Detail: detail})
		}
	}
	status, err = e.Status(ctx, scope)
	if err != nil {
		return result, err
	}
	for _, resource := range status.Resources {
		detail, err := c.cleanup(ctx, resource.ID)
		if errors.Is(err, ErrClaimLost) {
			detail, err = err.Error(), nil
		}
		if err != nil {
			return result, err
		}
		if detail != "" {
			result.Problems = append(result.Problems, JobProblem{ResourceID: resource.ID, Detail: detail})
		}
	}
	status, err = e.Status(ctx, scope)
	if err != nil {
		return result, err
	}
	for _, resource := range status.Resources {
		switch resource.State {
		case record.ResourceReleaseRequested, record.ResourceUncertain, record.ResourceRetained:
			result.PendingCleanup = append(result.PendingCleanup, resource.ID)
		}
	}
	return result, ctx.Err()
}

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

func jobTerminal(state record.JobState) bool {
	switch state {
	case record.JobCompleted, record.JobFailed, record.JobNeedsAttention, record.JobCanceled, record.JobSuperseded:
		return true
	default:
		return false
	}
}

func attemptTerminal(state record.AttemptState) bool {
	return state == record.AttemptFinished || state == record.AttemptCanceled
}
func live(claim *record.Claim, now time.Time) bool { return claim != nil && claim.ExpiresAt.After(now) }
func due(retry *time.Time, now time.Time) bool     { return retry == nil || !retry.After(now) }
func owns(current, expected *record.Claim, now time.Time) bool {
	return live(current, now) && expected != nil && current.Owner == expected.Owner && current.Generation == expected.Generation
}
func (c *cycle) claim(generation *uint64, now time.Time) (*record.Claim, error) {
	if *generation == ^uint64(0) {
		return nil, fmt.Errorf("workflow: claim generation exhausted")
	}
	(*generation)++
	return &record.Claim{Owner: c.owner, Generation: *generation, ExpiresAt: now.Add(c.lease)}, nil
}
