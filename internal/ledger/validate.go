package ledger

import (
	"fmt"

	"github.com/herbygillot/dockhand/v2/internal/model"
)

func validateState(state State) error {
	checks := []error{
		validateRecords("changes", state.Changes, func(v model.Change) model.ChangeID { return v.ID }),
		validateRecords("revisions", state.Revisions, func(v model.Revision) model.RevisionID { return v.ID }),
		validateRecords("jobs", state.Jobs, func(v model.Job) model.JobID { return v.ID }),
		validateRecords("controls", state.Controls, func(v model.ControlRequest) model.RequestID { return v.ID }),
		validateRecords("plans", state.Plans, func(v model.VerificationPlan) model.JobID { return v.JobID }),
		validateRecords("attempts", state.Attempts, func(v model.Attempt) model.AttemptID { return v.ID }),
		validateRecords("resources", state.Resources, func(v model.Resource) model.ResourceID { return v.ID }),
		validateRecords("publications", state.Publications, func(v model.PublicationAction) model.PublicationID { return v.ID }),
		validateRecords("pull requests", state.PullRequests, func(v model.PullRequest) model.PullRequestID { return v.ID }),
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
