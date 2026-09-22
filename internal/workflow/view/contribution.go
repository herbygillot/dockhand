package view

import (
	"slices"
	"time"

	"github.com/herbygillot/dockhand/internal/record"
)

// JobStatus is one job with the attempts and publication actions recorded
// for it.
type JobStatus struct {
	Plan         *record.VerificationPlan `json:",omitempty"`
	Job          record.Job
	Attempts     []record.Attempt
	Publications []record.PublicationAction
	// Reused is the original execution cited by a completed job, not a new attempt.
	Reused *record.Attempt `json:",omitempty"`
}

// Snapshot is what the view projects: the jobs, changes, revisions, and
// pull requests read from the store at one moment.
type Snapshot struct {
	Jobs         []JobStatus
	Changes      []record.Change
	Revisions    []record.Revision
	PullRequests []record.PullRequest
	Resources    []record.Resource
}

// EmptySnapshot is a snapshot with every list present and empty, so its
// JSON says so rather than omitting them.
func EmptySnapshot() Snapshot {
	return Snapshot{Jobs: []JobStatus{}, Changes: []record.Change{}, Revisions: []record.Revision{}, PullRequests: []record.PullRequest{}, Resources: []record.Resource{}}
}

// Contribution is one row of the contribution-centric view of a repository:
// a tracked change, or a standalone verification, with the words a person
// reads for where it stands and what comes next. The plain status output,
// its JSON result, and the live table all read this projection, so all
// three say the same thing.
type Contribution struct {
	// Port is the initiating target; Targets lists every port the change touches.
	Port    string
	Targets []string `json:",omitempty"`
	Branch  string   `json:",omitempty"`
	// Change words the version move ("1.7 -> 1.8.1") or the kind of change.
	Change string
	// Shared words each file under _resources the current revision changes
	// beside the port and what loads it, since only the port was built.
	Shared []string `json:",omitempty"`
	// Phase is preparation, verification, publication, or done.
	Phase string
	// State is the phase's current word: preparing, building on macOS 26,
	// waiting for capacity, verified, published, merged, needs attention.
	State string
	// Next says what happens or is needed next.
	Next        string
	PullRequest string `json:",omitempty"`
	// Log is the location of the latest failed build's log, when one is recorded.
	Log string `json:",omitempty"`
	// Retry is the verb that re-runs stopped work without redoing it: the
	// action that prepared the contribution, which adopts the branch already
	// built and keeps the publication it was going to make, or verify for one
	// dockhand did not prepare. Empty when nothing is stopped.
	Retry string `json:",omitempty"`
	// Active is the one job still doing work, with its last recorded detail.
	Active *ActiveJob `json:",omitempty"`
	// Detail is the current job's last recorded detail when nothing is active.
	Detail   string          `json:",omitempty"`
	ChangeID record.ChangeID `json:",omitempty"`
	// History lists the jobs behind this row, oldest first.
	History   []ContributionJob
	UpdatedAt time.Time
	// Retired is set once the change is merged, closed, or abandoned, or a
	// standalone verification has finished.
	Retired bool `json:",omitempty"`
	// Earlier holds the port's other contributions and standalone
	// verifications, newest first, folded under this row.
	Earlier []Contribution `json:",omitempty"`
}

type ActiveJob struct {
	JobID  record.JobID
	Action record.Action
	Detail string `json:",omitempty"`
}

type ContributionJob struct {
	JobID      record.JobID
	Action     record.Action
	State      record.JobState
	Phase      record.JobPhase
	Detail     string `json:",omitempty"`
	AcceptedAt time.Time
	FinishedAt *time.Time `json:",omitempty"`
}

// Project derives the rows of a snapshot: one per port, carrying the port's
// current open contribution, with its earlier contributions and standalone
// verifications folded underneath. Rows are ordered by the time of their
// latest activity, newest first.
func Project(status Snapshot) []Contribution {
	return foldByPort(contributions(status))
}

// foldByPort keeps one row per port: the newest open tracked contribution,
// else the newest standalone verification, else the newest retired one,
// with the rest as Earlier in their original order.
func foldByPort(rows []Contribution) []Contribution {
	rank := func(row Contribution) int {
		switch {
		case row.Retired:
			return 0
		case row.ChangeID == "":
			return 1
		}
		return 2
	}
	index := map[string]int{}
	var folded []Contribution
	for _, row := range rows {
		i, seen := index[row.Port]
		if !seen {
			index[row.Port] = len(folded)
			folded = append(folded, row)
			continue
		}
		current := &folded[i]
		if rank(row) > rank(*current) {
			earlier := append([]Contribution{*current}, current.Earlier...)
			current.Earlier = nil
			row.Earlier = earlier
			*current = row
			continue
		}
		current.Earlier = append(current.Earlier, row)
	}
	return folded
}

// Current keeps the rows that still have something going on: every row
// whose leading contribution is not retired. Retired rows are what --all
// and the table's history toggle bring back.
func Current(rows []Contribution) []Contribution {
	current := make([]Contribution, 0, len(rows))
	for _, row := range rows {
		if !row.Retired {
			current = append(current, row)
		}
	}
	return current
}

// contributions derives one row per contribution or standalone verification.
func contributions(status Snapshot) []Contribution {
	changes := make(map[record.ChangeID]record.Change, len(status.Changes))
	for _, change := range status.Changes {
		changes[change.ID] = change
	}
	pulls := make(map[record.ChangeID]record.PullRequest, len(status.PullRequests))
	for _, pr := range status.PullRequests {
		pulls[pr.ChangeID] = pr
	}
	grouped := map[record.ChangeID][]JobStatus{}
	var order []record.ChangeID
	var standalone [][]JobStatus
	for _, entry := range status.Jobs {
		id := entry.Job.ChangeID
		if id == "" {
			standalone = append(standalone, []JobStatus{entry})
			continue
		}
		if _, seen := grouped[id]; !seen {
			order = append(order, id)
		}
		grouped[id] = append(grouped[id], entry)
	}
	for _, change := range status.Changes {
		if _, seen := grouped[change.ID]; !seen {
			order = append(order, change.ID)
			grouped[change.ID] = nil
		}
	}
	var rows []Contribution
	for _, id := range order {
		change, known := changes[id]
		if !known {
			change = record.Change{ID: id}
		}
		var pr *record.PullRequest
		if found, ok := pulls[id]; ok {
			pr = &found
		}
		rows = append(rows, project(change, known, grouped[id], pr, status.Revisions))
	}
	for _, entries := range standalone {
		rows = append(rows, project(record.Change{}, false, entries, nil, nil))
	}
	slices.SortStableFunc(rows, func(a, b Contribution) int { return b.UpdatedAt.Compare(a.UpdatedAt) })
	return rows
}

func project(change record.Change, known bool, entries []JobStatus, pr *record.PullRequest, revisions []record.Revision) Contribution {
	row := Contribution{ChangeID: change.ID, Branch: change.Branch, Port: change.InitiatingTarget, History: []ContributionJob{}, UpdatedAt: change.CreatedAt}
	for _, target := range change.Targets {
		row.Targets = append(row.Targets, target.Name)
	}
	for _, revision := range revisions {
		if revision.ID == change.CurrentRevision {
			row.Shared = SharedWords(revision.Shared, row.Port)
		}
	}
	var current *JobStatus
	for i := range entries {
		entry := &entries[i]
		job := entry.Job
		row.History = append(row.History, ContributionJob{JobID: job.ID, Action: job.Spec.Action, State: job.State, Phase: job.Phase, Detail: job.Detail, AcceptedAt: job.AcceptedAt, FinishedAt: job.FinishedAt})
		row.UpdatedAt = latest(row.UpdatedAt, job.AcceptedAt, job.FinishedAt)
		if row.Port == "" && len(job.Spec.Targets) > 0 {
			row.Port = job.Spec.Targets[0].Name
		}
		if row.Branch == "" && job.Prepared != nil {
			row.Branch = job.Prepared.Branch
		}
		if row.Change == "" {
			row.Change = changeWords(job)
		}
		for _, attempt := range entry.Attempts {
			if attempt.Evidence == nil || attempt.Evidence.Verdict != record.VerdictFailed {
				continue
			}
			for _, log := range attempt.Evidence.Logs {
				if log.Location != "" {
					row.Log = log.Location
					break
				}
			}
		}
		if job.State == record.JobSuperseded {
			continue
		}
		if current == nil || job.State == record.JobActive || job.State == record.JobQueued || current.Job.State.Terminal() {
			current = entry
		}
	}
	if row.Port == "" {
		row.Port = string(change.ID)
	}
	if row.Change == "" {
		row.Change = revisionWords(change, revisions)
	}
	if pr != nil {
		row.PullRequest = pr.Ref.URL
		row.UpdatedAt = latest(row.UpdatedAt, pr.ObservedAt, nil)
	}
	if current != nil {
		job := current.Job
		if job.State == record.JobActive || job.State == record.JobQueued {
			row.Active = &ActiveJob{JobID: job.ID, Action: job.Spec.Action, Detail: job.Detail}
		} else {
			row.Detail = job.Detail
		}
	}
	row.Phase, row.State, row.Next = words(change, known, current, pr)
	if current != nil && current.Job.State.Terminal() && current.Job.State != record.JobCompleted {
		row.Retry = retryVerb(current.Job)
	}
	// A retired change is history. So is a standalone verification once its
	// job has finished: it belongs to no contribution, so there is nothing
	// to abandon and nothing further to do with it.
	row.Retired = known && change.Disposition != record.ChangeOpen || !known && current != nil && current.Job.State.Terminal()
	return row
}

func latest(current, accepted time.Time, finished *time.Time) time.Time {
	if accepted.After(current) {
		current = accepted
	}
	if finished != nil && finished.After(current) {
		current = *finished
	}
	return current
}
