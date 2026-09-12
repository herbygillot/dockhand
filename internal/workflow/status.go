package workflow

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"time"

	"github.com/herbygillot/dockhand/v2/internal/ledger"
	"github.com/herbygillot/dockhand/v2/internal/record"
)

type JobStatus struct {
	Job          record.Job
	Attempts     []record.Attempt
	Publications []record.PublicationAction
}

type Status struct {
	LedgerVersion record.ObjectID
	ReadAt        time.Time
	Jobs          []JobStatus
	Changes       []record.Change
	Revisions     []record.Revision
	PullRequests  []record.PullRequest
	Resources     []record.Resource
}

func (e *Engine) Status(ctx context.Context, scope Scope) (Status, error) {
	if e == nil || e.Ledger == nil {
		return Status{}, ErrNoLedger
	}
	if (scope.All && len(scope.Jobs) != 0) || (!scope.All && len(scope.Jobs) == 0) {
		return Status{}, ErrInvalidScope
	}
	selected := make(map[record.JobID]*JobStatus)
	for _, id := range scope.Jobs {
		if id == "" {
			return Status{}, ErrInvalidScope
		}
		selected[id] = nil
	}
	snapshot, err := e.Ledger.Read(ctx)
	if errors.Is(err, ledger.ErrNoState) {
		snapshot = ledger.Snapshot{State: ledger.NewState()}
	} else if err != nil {
		return Status{}, err
	}
	state := snapshot.State
	result := Status{
		LedgerVersion: snapshot.Version, ReadAt: e.now(),
		Jobs: []JobStatus{}, Changes: []record.Change{}, Revisions: []record.Revision{},
		PullRequests: []record.PullRequest{}, Resources: []record.Resource{},
	}
	if scope.All {
		for id := range state.Jobs {
			selected[id] = nil
		}
	}
	changes := make(map[record.ChangeID]bool)
	revisions := make(map[record.RevisionID]bool)
	attempts := make(map[record.AttemptID]bool)
	for _, id := range slices.Sorted(maps.Keys(selected)) {
		job, exists := state.Jobs[id]
		if !exists {
			return Status{}, fmt.Errorf("%w: job %s", ErrNotFound, id)
		}
		selected[id] = &JobStatus{Job: job, Attempts: []record.Attempt{}, Publications: []record.PublicationAction{}}
		changes[job.ChangeID] = true
		revisions[job.Spec.InputRevision] = true
		revisions[job.ResultRevision] = true
	}
	for _, id := range slices.Sorted(maps.Keys(state.Attempts)) {
		attempt := state.Attempts[id]
		if job, exists := selected[attempt.JobID]; exists {
			job.Attempts = append(job.Attempts, attempt)
			attempts[id] = true
			revisions[attempt.Spec.RevisionID] = true
		}
	}
	for _, id := range slices.Sorted(maps.Keys(state.Publications)) {
		publication := state.Publications[id]
		if job, exists := selected[publication.JobID]; exists {
			job.Publications = append(job.Publications, publication)
			changes[publication.ChangeID] = true
			revisions[publication.RevisionID] = true
		}
	}
	for _, id := range slices.Sorted(maps.Keys(selected)) {
		result.Jobs = append(result.Jobs, *selected[id])
	}
	slices.SortStableFunc(result.Jobs, func(a, b JobStatus) int { return a.Job.AcceptedAt.Compare(b.Job.AcceptedAt) })
	for _, id := range slices.Sorted(maps.Keys(state.Changes)) {
		change := state.Changes[id]
		if scope.All || changes[id] {
			result.Changes = append(result.Changes, change)
			revisions[change.CurrentRevision] = true
			revisions[change.PublishedRevision] = true
		}
	}
	for _, id := range slices.Sorted(maps.Keys(state.Revisions)) {
		if scope.All || revisions[id] {
			result.Revisions = append(result.Revisions, state.Revisions[id])
		}
	}
	for _, id := range slices.Sorted(maps.Keys(state.PullRequests)) {
		pr := state.PullRequests[id]
		if scope.All || changes[pr.ChangeID] {
			result.PullRequests = append(result.PullRequests, pr)
		}
	}
	for _, id := range slices.Sorted(maps.Keys(state.Resources)) {
		resource := state.Resources[id]
		if scope.All || attempts[resource.AttemptID] {
			result.Resources = append(result.Resources, resource)
		}
	}
	return result, nil
}
