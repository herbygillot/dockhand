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

// JobStatus groups a job with its recorded attempts and publication actions.
// Resources remain separate in Status so their lifetime can outlast the job.
type JobStatus struct {
	Job          record.Job
	Attempts     []record.Attempt
	Publications []record.PublicationAction
}

// Status is a caller-owned projection of one immutable ledger snapshot.
// Its timestamps distinguish snapshot reads from provider and forge observations.
// Records may share maps or slices within the result; modifying them does not
// modify the ledger or a later Status result.
type Status struct {
	// LedgerVersion identifies the state commit, or is empty before the ledger
	// has any persisted state.
	LedgerVersion record.ObjectID
	// ReadAt records this snapshot read, without refreshing external observations.
	ReadAt time.Time
	// Jobs are ordered by acceptance time, then job ID. Associated collections
	// and the remaining top-level collections are ordered by their record IDs.
	Jobs         []JobStatus
	Changes      []record.Change
	Revisions    []record.Revision
	PullRequests []record.PullRequest
	// Resources includes associated cleanup obligations, including after jobs
	// finish. An all-jobs scope also includes orphan resource records.
	Resources []record.Resource
}

// Status reads one ledger snapshot without acquiring the writer lock, contacting
// providers, or advancing work. A selected-job scope includes associated changes,
// revisions, pull requests, and resources. An all-jobs scope exposes all of those
// records, including changes with no jobs and resources with no owning attempt.
//
// A missing state ref yields an empty all-jobs result; corrupt or unsupported
// state remains an error. Explicit unknown job IDs return ErrNotFound. All result
// collections are initialized on success, including when empty.
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
