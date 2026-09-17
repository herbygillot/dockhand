package workflow

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/herbygillot/dockhand/internal/record"
)

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
	// Active is the one job still doing work, with its last recorded detail.
	Active *ActiveJob `json:",omitempty"`
	// Detail is the current job's last recorded detail when nothing is active.
	Detail   string          `json:",omitempty"`
	ChangeID record.ChangeID `json:",omitempty"`
	// History lists the jobs behind this row, oldest first.
	History   []ContributionJob
	UpdatedAt time.Time
	// Retired is set once the change is merged, closed, or abandoned.
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

// Overview is a status snapshot with its contribution projection.
type Overview struct {
	Status
	Contributions []Contribution
}

// Project derives the rows of a snapshot: one per port, carrying the port's
// current open contribution, with its earlier contributions and standalone
// verifications folded underneath. Rows are ordered by the time of their
// latest activity, newest first.
func Project(status Status) []Contribution {
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
func contributions(status Status) []Contribution {
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
	row.Retired = known && change.Disposition != record.ChangeOpen
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

// changeWords says what a job changes: the version move for a bump once its
// release is resolved, otherwise the kind of change.
func changeWords(job record.Job) string {
	switch job.Spec.Action {
	case record.Bump:
		release := job.ResolvedRelease
		if release == nil {
			return "version bump"
		}
		if release.NoUpdate {
			return "current at " + release.CurrentVersion
		}
		if release.CurrentVersion == "" {
			return "-> " + release.Version
		}
		return release.CurrentVersion + " -> " + release.Version
	case record.BumpRevision:
		return "revision bump"
	case record.RefreshChecksums:
		return "checksum refresh"
	case record.Verify:
		return "verification"
	case record.Publish, record.Amend, record.Rebase:
		return ""
	}
	return string(job.Spec.Action)
}

// revisionWords recovers the version move of a change whose bump job is not
// in the snapshot from its current revision's release scope.
func revisionWords(change record.Change, revisions []record.Revision) string {
	for _, revision := range revisions {
		if revision.ID != change.CurrentRevision || revision.Scope == nil {
			continue
		}
		for _, member := range revision.Scope.Affected {
			if member.Target.Name == change.InitiatingTarget || change.InitiatingTarget == "" {
				return member.Before.Version + " -> " + member.After.Version
			}
		}
	}
	return "update"
}

// words derives the phase, state, and next columns from the change, its
// current job, and its pull request.
func words(change record.Change, known bool, current *JobStatus, pr *record.PullRequest) (phase, state, next string) {
	if known && change.Disposition != record.ChangeOpen {
		state = string(change.Disposition)
		switch {
		case change.Disposition == record.ChangeMerged:
			next = "merged; branches cleaned"
		case change.Disposition == record.ChangeClosed && change.Branch == "" && pr == nil:
			state = "retired"
			next = "stopped before a branch; bump again once fixed"
			if current != nil && current.Job.Detail != "" {
				next += ": " + current.Job.Detail
			}
		case change.Disposition == record.ChangeClosed:
			next = "PR closed without merging"
		case change.Disposition == record.ChangeAbandoned:
			next = "abandoned; branch and evidence preserved"
		}
		return "done", state, next
	}
	if current == nil {
		if pr != nil {
			return "publication", "published", pullRequestNext(pr)
		}
		return "preparation", "recorded", "no jobs recorded"
	}
	job := current.Job
	phase = string(job.Phase)
	switch job.State {
	case record.JobQueued:
		return phase, "queued", "waiting for a driver; run dockhand start or dockhand wait"
	case record.JobActive:
		return phase, activeState(*current), activeNext(job)
	case record.JobCompleted:
		return completedWords(*current, pr)
	case record.JobFailed:
		return phase, "failed", failedNext(*current)
	case record.JobNeedsAttention:
		next = job.Detail
		switch {
		case job.Prepared != nil && len(job.Prepared.PatchProblems) > 0:
			next = fmt.Sprintf("patches no longer apply (%d); refresh them and amend", len(job.Prepared.PatchProblems))
		case job.Phase == record.PhasePreparation && job.Prepared == nil:
			next = "bump again once fixed, or abandon: " + job.Detail
		case job.Phase == record.PhaseVerification:
			next = "verify again once fixed, or abandon: " + job.Detail
		}
		return phase, "needs attention", next
	case record.JobCanceled:
		return phase, "canceled", "verify or publish again, or abandon"
	}
	return phase, string(job.State), job.Detail
}

func activeState(entry JobStatus) string {
	job := entry.Job
	switch job.Phase {
	case record.PhasePreparation:
		if job.Prepared != nil {
			return "integrating branch"
		}
		return "preparing"
	case record.PhaseVerification:
		queued, running := 0, 0
		var platform string
		for _, attempt := range entry.Attempts {
			switch attempt.State {
			case record.AttemptQueued:
				queued++
			case record.AttemptSubmitting, record.AttemptRunning:
				running++
				if platform == "" {
					platform = strings.TrimSpace(attempt.Spec.Config.Platform.OS + " " + attempt.Spec.Config.Platform.Version)
				}
			}
		}
		switch {
		case running > 0 && platform != "":
			return "building on " + platform
		case running > 0:
			return "building"
		case queued > 0:
			return "waiting for capacity"
		}
		return "verifying"
	case record.PhasePublication:
		return "publishing"
	}
	return "in progress"
}

func activeNext(job record.Job) string {
	switch job.Phase {
	case record.PhasePreparation:
		if job.Spec.Destination == record.BranchReady {
			return "branch ready when preparation completes"
		}
		return "verification pending"
	case record.PhaseVerification:
		if job.Spec.Destination == record.Published {
			return "publication pending"
		}
		return "verification pending"
	case record.PhasePublication:
		return "PR confirmation pending"
	}
	return ""
}

func completedWords(entry JobStatus, pr *record.PullRequest) (phase, state, next string) {
	job := entry.Job
	if job.ResolvedRelease != nil && job.ResolvedRelease.NoUpdate {
		return "done", "current", "no update needed"
	}
	switch job.Spec.Destination {
	case record.BranchReady:
		return "preparation", "branch ready", "verify when ready: dockhand verify " + jobPort(job)
	case record.Published:
		if pr != nil {
			return "publication", "published", pullRequestNext(pr)
		}
		return "publication", "published", "PR recorded; refresh to observe it"
	}
	if job.Spec.Action == record.Verify && job.ChangeID == "" {
		return "verification", "verified", "standalone verification; no update was prepared"
	}
	return "verification", "verified", "publish when ready: dockhand publish " + jobPort(job)
}

func failedNext(entry JobStatus) string {
	for _, attempt := range entry.Attempts {
		if attempt.Evidence == nil || attempt.Evidence.Verdict != record.VerdictFailed {
			continue
		}
		if failure := attempt.Evidence.Failure; failure != nil && failure.Phase != "" {
			return failure.Phase + " failed; fix and amend, or abandon"
		}
		return "build failed; fix and amend, or abandon"
	}
	if entry.Job.Phase == record.PhasePreparation {
		return "preparation failed; retry the bump"
	}
	return "fix and amend, or abandon"
}

// pullRequestNext words an observed PR: its state, then the mergeability,
// review, and check counts when they were inspected.
func pullRequestNext(pr *record.PullRequest) string {
	switch pr.State {
	case record.PullRequestMerged:
		return "merged; run dockhand refresh to retire the contribution"
	case record.PullRequestClosed:
		return "PR closed; run dockhand refresh to retire the contribution"
	case record.PullRequestUnknown:
		return "PR state unknown; run dockhand refresh"
	}
	if pr.Status == nil {
		return "PR open; run dockhand refresh for checks and review"
	}
	parts := []string{"PR open"}
	if pr.Status.Draft {
		parts = append(parts, "draft")
	}
	if pr.Status.Checks.Total == 0 {
		parts = append(parts, "no checks reported")
	} else if pr.Status.Checks.Failed > 0 {
		parts = append(parts, fmt.Sprintf("%d checks failed", pr.Status.Checks.Failed))
	} else if pr.Status.Checks.Pending > 0 {
		parts = append(parts, fmt.Sprintf("%d checks pending", pr.Status.Checks.Pending))
	} else {
		parts = append(parts, "checks passed")
	}
	switch pr.Status.Review {
	case "approved":
		parts = append(parts, "approved")
	case "changes-requested":
		parts = append(parts, "changes requested")
	}
	if pr.Status.Mergeable == "no" {
		detail := "not mergeable"
		if pr.Status.MergeableDetail != "" {
			detail += " (" + pr.Status.MergeableDetail + ")"
		}
		parts = append(parts, detail)
	}
	return strings.Join(parts, ", ")
}

func jobPort(job record.Job) string {
	if len(job.Spec.Targets) == 0 {
		return "--job " + string(job.ID)
	}
	return job.Spec.Targets[0].Name
}
