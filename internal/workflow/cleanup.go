package workflow

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"

	"github.com/herbygillot/dockhand/v2/internal/record"
	"github.com/herbygillot/dockhand/v2/internal/state"
)

// recordResources records valid provider handles as uncertain ownership within
// the caller's transaction. Identity combines provider namespace and resource
// lifetime; existing ownership and released records are never overwritten.
// Valid handles are still recorded when other handles produce a returned error.
func recordResources(work *execution, attempt record.Attempt, handles []record.ResourceHandle) error {
	var problems []error
	for _, handle := range handles {
		if handle.Provider != attempt.Spec.Config.Provider || !validToken(handle.ID) {
			problems = append(problems, fmt.Errorf("workflow: provider returned an invalid or foreign resource handle"))
			continue
		}
		id := record.ResourceID(fmt.Sprintf("resource_%x", sha256.Sum256([]byte(handle.Provider+"\x00"+handle.ID))))
		if previous, exists := work.Resources[id]; exists {
			if previous.AttemptID != attempt.ID || previous.Handle != handle {
				problems = append(problems, fmt.Errorf("workflow: resource %s is already owned by another attempt", id))
			}
			if previous.State == record.ResourceReleased {
				problems = append(problems, fmt.Errorf("workflow: provider returned a previously released resource %s", id))
			}
			continue
		}
		work.Resources[id] = record.Resource{ID: id, AttemptID: attempt.ID, SubmissionID: attempt.SubmissionID, Handle: handle, State: record.ResourceUncertain}
	}
	return errors.Join(problems...)
}

// hasResources reports any ownership record for the attempt, including released resources.
func hasResources(work *execution, id record.AttemptID) bool {
	for _, resource := range work.Resources {
		if resource.AttemptID == id {
			return true
		}
	}
	return false
}

// dispositionResources updates an attempt's unreleased resources and clears
// their retry delays within the caller's transaction. Released records retain
// their confirmed state; this helper performs no external cleanup.
func dispositionResources(work *execution, id record.AttemptID, disposition record.ResourceState) {
	for key, resource := range work.Resources {
		if resource.AttemptID != id || resource.State == record.ResourceReleased {
			continue
		}
		resource.State, resource.RetryAt = disposition, nil
		work.Resources[key] = resource
	}
}

// cleanup claims and performs at most one eligible resource release. It requires
// a terminal owning attempt, a matching provider handle, and an eligible retention
// state. Missing ownership and other per-resource problems are returned as detail;
// transaction failures and lost claims are returned as errors.
//
// The provider call runs outside the write transaction. An unconfirmed release remains
// uncertain with a retry time, and a stale result cannot replace newer confirmation.
// The job's recorded outcome is unaffected by cleanup progress.
func (c *cycle) cleanup(ctx context.Context, id record.ResourceID) (string, error) {
	e := c.engine
	var resource record.Resource
	var claimed bool
	var detail string
	err := e.State.Update(ctx, e.Repository, func(ctx context.Context, tx state.Tx) error {
		var err error
		resource, err = tx.Resource(ctx, id)
		if err != nil {
			return err
		}
		attempt, err := tx.Attempt(ctx, resource.AttemptID)
		if err != nil {
			return err
		}
		now := e.now()
		if !cleanupEligible(resource, attempt, now) {
			return nil
		}
		switch resource.State {
		case record.ResourceRetained, record.ResourceReleaseRequested, record.ResourceUncertain:
		default:
			detail = "workflow: resource has an invalid cleanup state"
			return nil
		}

		if !attemptTerminal(attempt.State) {
			return nil
		}
		if resource.Handle.Provider != attempt.Spec.Config.Provider || !validToken(resource.Handle.ID) {
			detail = "workflow: resource ownership does not match its attempt"
			return nil
		}
		if err := c.providerReady(attempt.Spec.Config, false); err != nil {
			detail = err.Error()
		} else {
			claim, err := c.claim(&resource.ClaimGeneration, now)
			if err != nil {
				return err
			}
			resource.State, resource.Claim = record.ResourceReleaseRequested, claim
			claimed = true
		}
		if detail != "" {
			retry := now.Add(c.retry)
			resource.LastError, resource.RetryAt = detail, &retry
		}
		return tx.PutResource(ctx, resource)
	})
	if err != nil || !claimed {
		return detail, err
	}
	// Release has its own durable claim and must be idempotent: another cycle
	// may retry it if this process dies before recording confirmation.
	callCtx, cancel := context.WithTimeout(ctx, c.timeout)
	result, callErr := e.Provider.Release(callCtx, resource.Handle)
	if callErr == nil {
		callErr = callCtx.Err()
	}
	cancel()
	err = e.State.Update(ctx, e.Repository, func(ctx context.Context, tx state.Tx) error {
		current, err := tx.Resource(ctx, id)
		if err != nil {
			return err
		}
		now := e.now()
		if !owns(current.Claim, resource.Claim, now) || current.State != record.ResourceReleaseRequested {
			return ErrClaimLost
		}
		current.Claim, current.LastError, current.RetryAt = nil, "", nil
		if callErr == nil && result.Confirmed {
			current.State, current.ReleasedAt = record.ResourceReleased, &now
		} else {
			detail = "workflow: resource release is unconfirmed: " + result.Detail
			if callErr != nil {
				detail = callErr.Error()
			}
			retry := now.Add(c.retry)
			current.State, current.LastError, current.RetryAt = record.ResourceUncertain, detail, &retry
		}
		return tx.PutResource(ctx, current)
	})
	return detail, err
}
