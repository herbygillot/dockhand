package workflow

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"

	"github.com/herbygillot/dockhand/v2/internal/ledger"
	"github.com/herbygillot/dockhand/v2/internal/record"
)

func recordResources(state *ledger.State, attempt record.Attempt, handles []record.ResourceHandle) error {
	var problems []error
	for _, handle := range handles {
		if handle.Provider != attempt.Spec.Config.Provider || !validToken(handle.ID) {
			problems = append(problems, fmt.Errorf("workflow: provider returned an invalid or foreign resource handle"))
			continue
		}
		id := record.ResourceID(fmt.Sprintf("resource_%x", sha256.Sum256([]byte(handle.Provider+"\x00"+handle.ID))))
		if previous, exists := state.Resources[id]; exists {
			if previous.AttemptID != attempt.ID || previous.Handle != handle {
				problems = append(problems, fmt.Errorf("workflow: resource %s is already owned by another attempt", id))
			}
			if previous.State == record.ResourceReleased {
				problems = append(problems, fmt.Errorf("workflow: provider returned a previously released resource %s", id))
			}
			continue
		}
		state.Resources[id] = record.Resource{ID: id, AttemptID: attempt.ID, Handle: handle, State: record.ResourceUncertain}
	}
	return errors.Join(problems...)
}

func hasResources(state ledger.State, id record.AttemptID) bool {
	for _, resource := range state.Resources {
		if resource.AttemptID == id {
			return true
		}
	}
	return false
}

func dispositionResources(state *ledger.State, id record.AttemptID, disposition record.ResourceState) {
	for key, resource := range state.Resources {
		if resource.AttemptID != id || resource.State == record.ResourceReleased {
			continue
		}
		resource.State, resource.RetryAt = disposition, nil
		state.Resources[key] = resource
	}
}

func (c *cycle) cleanup(ctx context.Context, id record.ResourceID) (string, error) {
	e := c.engine
	var resource record.Resource
	var claimed bool
	var detail string
	err := e.Ledger.Update(ctx, func(_ context.Context, tx *ledger.Transaction) error {
		var exists bool
		resource, exists = tx.State.Resources[id]
		if !exists {
			return fmt.Errorf("%w: resource %s", ErrNotFound, id)
		}
		now := e.now()
		if resource.State == record.ResourceReleased || live(resource.Claim, now) || !due(resource.RetryAt, now) {
			return nil
		}
		switch resource.State {
		case record.ResourceActive:
			return nil
		case record.ResourceRetained:
			if resource.RetainUntil == nil || resource.RetainUntil.After(now) {
				return nil
			}
		case record.ResourceReleaseRequested, record.ResourceUncertain:
		default:
			detail = "workflow: resource has an invalid cleanup state"
			return nil
		}
		attempt, exists := tx.State.Attempts[resource.AttemptID]
		if !exists {
			detail = "workflow: resource has no owning attempt; cleanup requires reconciliation"
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
		tx.State.Resources[id] = resource
		return nil
	})
	if err != nil || !claimed {
		return detail, err
	}
	callCtx, cancel := context.WithTimeout(ctx, c.timeout)
	result, callErr := e.Provider.Release(callCtx, resource.Handle)
	if callErr == nil {
		callErr = callCtx.Err()
	}
	cancel()
	err = e.Ledger.Update(ctx, func(_ context.Context, tx *ledger.Transaction) error {
		current, exists := tx.State.Resources[id]
		now := e.now()
		if !exists || !owns(current.Claim, resource.Claim, now) || current.State != record.ResourceReleaseRequested {
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
		tx.State.Resources[id] = current
		return nil
	})
	return detail, err
}
