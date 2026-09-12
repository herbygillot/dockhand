package ledger

import (
	"fmt"

	"github.com/herbygillot/dockhand/v2/internal/record"
)

func validateState(state State) error {
	checks := []error{
		validateRecords("changes", state.Changes, func(v record.Change) record.ChangeID { return v.ID }),
		validateRecords("revisions", state.Revisions, func(v record.Revision) record.RevisionID { return v.ID }),
		validateRecords("jobs", state.Jobs, func(v record.Job) record.JobID { return v.ID }),
		validateRecords("controls", state.Controls, func(v record.ControlRequest) record.RequestID { return v.ID }),
		validateRecords("plans", state.Plans, func(v record.VerificationPlan) record.JobID { return v.JobID }),
		validateRecords("attempts", state.Attempts, func(v record.Attempt) record.AttemptID { return v.ID }),
		validateRecords("resources", state.Resources, func(v record.Resource) record.ResourceID { return v.ID }),
		validateRecords("publications", state.Publications, func(v record.PublicationAction) record.PublicationID { return v.ID }),
		validateRecords("pull requests", state.PullRequests, func(v record.PullRequest) record.PullRequestID { return v.ID }),
	}
	for _, err := range checks {
		if err != nil {
			return err
		}
	}
	if state.Requests == nil {
		return fmt.Errorf("%w: missing requests collection", ErrInvalidState)
	}
	for request, job := range state.Requests {
		if request == "" || job == "" {
			return fmt.Errorf("%w: empty request or job ID", ErrInvalidState)
		}
		if record, exists := state.Jobs[job]; !exists || record.RequestID != request {
			return fmt.Errorf("%w: request %s does not identify its job", ErrInvalidState, request)
		}
	}
	for id, job := range state.Jobs {
		if job.RequestID == "" || state.Requests[job.RequestID] != id {
			return fmt.Errorf("%w: job %s has no matching request index", ErrInvalidState, id)
		}
	}
	return nil
}

func validateRecords[K ~string, V any](name string, records map[K]V, id func(V) K) error {
	if records == nil {
		return fmt.Errorf("%w: missing %s collection", ErrInvalidState, name)
	}
	for key, record := range records {
		if key == "" || key != id(record) {
			return fmt.Errorf("%w: %s key %q does not match its record ID", ErrInvalidState, name, key)
		}
	}
	return nil
}
