package view

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/herbygillot/dockhand/internal/macos"
	"github.com/herbygillot/dockhand/internal/record"
)

// The phrasebook: the words status, its JSON, the table, and the action
// summaries share for a change, a job, a state, and what to do next. The
// projection above decides what a row is; this decides what it says.

// changeWords says what a job changes: the version move for a bump once its
// release is resolved, otherwise the kind of change. A publication or
// correction has no words of its own; its contribution's row keeps the
// words of the update it carries.
func changeWords(job record.Job) string {
	switch job.Spec.Action {
	case record.Bump:
		if job.ResolvedRelease == nil {
			return "version bump"
		}
		return VersionMove(job.ResolvedRelease)
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

// ChangeWords is what a job does, for a line about the job itself: the
// version move or kind of update, else the kind of job.
func ChangeWords(job record.Job) string {
	if words := changeWords(job); words != "" {
		return words
	}
	switch job.Spec.Action {
	case record.Publish:
		return "publication"
	case record.Amend:
		return "amendment"
	case record.Rebase:
		return "rebase"
	}
	return string(job.Spec.Action)
}

// VersionMove words a resolved release as the change a person sees:
// "1.7 -> 1.8.1", or that the port is already current.
func VersionMove(release *record.Release) string {
	if release.NoUpdate {
		words := "already current at " + release.CurrentVersion
		if release.Version != "" && release.Version != release.CurrentVersion {
			words += "; latest eligible version is " + release.Version
		}
		return words
	}
	if release.CurrentVersion == "" {
		return "-> " + release.Version
	}
	return release.CurrentVersion + " -> " + release.Version
}

// PortLabel names a job by its first target and explicit variants, the way
// a summary line reads ("jq +docs"); a job with no target is a standalone
// job, never a UUID standing where a port name should be.
func PortLabel(job record.Job) string {
	if len(job.Spec.Targets) == 0 {
		return "standalone job"
	}
	target := job.Spec.Targets[0]
	name := target.Name
	for _, variant := range slices.Sorted(maps.Keys(target.Variants)) {
		prefix := "-"
		if target.Variants[variant] {
			prefix = "+"
		}
		name += " " + prefix + variant
	}
	return name
}

// PortSelector names a job the way a command selects it: the first target's
// name, or "--job <id>" when it has none, so it can be pasted after a verb.
func PortSelector(job record.Job) string {
	if len(job.Spec.Targets) == 0 {
		return "--job " + string(job.ID)
	}
	return job.Spec.Targets[0].Name
}

// JobState is the state word for one job, the same one its contribution's
// row shows when this is the current job: queued, preparing, building on
// macOS 26, waiting for capacity, verified, published, needs attention.
func JobState(entry JobStatus, pr *record.PullRequest) string {
	_, state, _ := jobWords(entry, pr)
	return state
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
			next = mergedNext(change.Cleanup)
		case change.Disposition == record.ChangeClosed && change.Branch == "" && pr == nil:
			state = "retired"
			// Nothing was built, so this starts over from fresh master rather
			// than adopting anything; the action is still the one to run.
			next = "stopped before a branch; fix it, then start over"
			if current != nil {
				next = "stopped before a branch; fix it, then " + retryCommand(current.Job) + " again"
			}
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
	return jobWords(*current, pr)
}

// jobWords derives the phase, state, and next columns from a job alone.
func jobWords(current JobStatus, pr *record.PullRequest) (phase, state, next string) {
	job := current.Job
	phase = string(job.Phase)
	switch job.State {
	case record.JobQueued:
		return phase, "queued", "waiting for a driver; run dockhand start or dockhand wait"
	case record.JobActive:
		return phase, activeState(current), activeNext(job)
	case record.JobCompleted:
		return completedWords(current, pr)
	case record.JobFailed:
		return phase, "failed", failedNext(current)
	case record.JobNeedsAttention:
		next = job.Detail
		switch {
		case job.Prepared != nil && len(job.Prepared.PatchProblems) > 0:
			next = fmt.Sprintf("patches no longer apply (%d); refresh them and amend", len(job.Prepared.PatchProblems))
		case job.Phase == record.PhasePreparation && job.Prepared == nil:
			next = "fix it, then " + retryCommand(job) + " again, or abandon: " + job.Detail
		case job.Phase == record.PhaseVerification:
			// Nothing here resumes on its own: this attempt is over, and the
			// command below starts a new one rather than continuing it.
			next = "fix it, then " + retryCommand(job) + " again, which starts a new attempt, or abandon: " + job.Detail
			if beyond := beyondTheChange(current); beyond != "" {
				next = beyond
			}
		}
		return phase, "needs attention", next
	case record.JobCanceled:
		return phase, "canceled", retryCommand(job) + " again, or abandon"
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
					platform = macos.Describe(record.Platform{OS: attempt.Spec.Config.Platform.OS, Version: attempt.Spec.Config.Platform.Version})
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
		if job.Spec.Verification == record.VerificationSkipped {
			return "publication pending; verification skipped"
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
		return "done", "no update needed", "bump again when upstream releases"
	}
	switch job.Spec.Destination {
	case record.BranchReady:
		return "preparation", "branch ready", "verify when ready: dockhand verify " + PortSelector(job)
	case record.Published:
		state := "published"
		if job.Spec.Verification == record.VerificationSkipped {
			state = "published unverified"
		}
		if pr != nil {
			return "publication", state, pullRequestNext(pr)
		}
		return "publication", state, "PR recorded; refresh to observe it"
	}
	if job.Spec.Action == record.Verify && job.ChangeID == "" {
		return "verification", "verified", "standalone verification; no update was prepared"
	}
	return "verification", "verified", "publish when ready: dockhand publish " + PortSelector(job)
}

func failedNext(entry JobStatus) string {
	if beyond := beyondTheChange(entry); beyond != "" {
		return beyond
	}
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
		return "preparation failed; " + retryCommand(entry.Job) + " again"
	}
	return "fix and amend, or abandon"
}

// beyondTheChange words a verification that did not fail on the candidate
// itself, and says to run it again rather than to change something. A build
// the machinery never completed, or one whose failing package is a dependency
// rather than the target, says nothing about the contribution: the same
// source run again is the useful next step, and amending it is not.
func beyondTheChange(entry JobStatus) string {
	retry := retryCommand(entry.Job)
	for _, attempt := range entry.Attempts {
		evidence := attempt.Evidence
		if evidence == nil {
			continue
		}
		failure := evidence.Failure
		switch {
		case failure != nil && failure.Kind == record.InfrastructureFailure:
			return "verification could not run (" + oneLine(failure.Detail) + "); nothing to fix in the change, retry it: " + retry
		case failure != nil && failure.Kind == record.DockhandFailure:
			return "dockhand failed to run the verification (" + oneLine(failure.Detail) + "); retry it, and report it if it repeats: " + retry
		case failure != nil && failure.Kind == record.DependencyFailure && failure.Package != "":
			return "dependency " + failure.Package + " failed to build, not the change itself; retry it, or fix that port first: " + retry
		case evidence.Verdict == record.VerdictErrored:
			return "verification errored before reaching a verdict; nothing to fix in the change, retry it: " + retry
		case evidence.Verdict == record.VerdictBlocked:
			return "verification was blocked before it could judge the change; retry it once the blocker is cleared: " + retry
		}
	}
	return ""
}

// retryCommand names what re-runs a stopped job without redoing its work.
// Submitting the same preparation again adopts the branch it already built,
// so it starts at verification and keeps the publication it was going to
// make; that is the command to offer, not the weaker verify, which stops at
// a verified contribution. Anything else is retried by verifying again.
func retryCommand(job record.Job) string {
	command := "dockhand " + retryVerb(job) + " " + PortSelector(job)
	if job.Spec.Action == record.Bump && job.ResolvedRelease != nil && job.ResolvedRelease.Requested != "" {
		command += " " + job.ResolvedRelease.Requested
	}
	return command
}

// retryVerb is the verb of that command. A contribution dockhand prepared is
// retried by its own preparing action, which is the only one that adopts the
// prepared branch and carries the publication forward. A branch someone made
// themselves was never prepared, so there is nothing to bump: verifying it
// again is the retry.
func retryVerb(job record.Job) string {
	switch job.Spec.Action {
	case record.Bump, record.BumpRevision, record.RefreshChecksums:
		return string(job.Spec.Action)
	case record.Publish:
		return "publish"
	}
	return "verify"
}

// oneLine keeps a recorded detail on the row it is printed in.
func oneLine(text string) string { return strings.Join(strings.Fields(text), " ") }

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

// mergedNext words a merged contribution's housekeeping from its recorded
// cleanup: cleaned once both branches are settled, otherwise what is still
// owed or was kept and why. A contribution retired before cleanup was
// recorded is only merged.
func mergedNext(cleanup *record.BranchCleanup) string {
	if cleanup == nil {
		return "merged"
	}
	var parts []string
	for _, side := range []struct {
		label   string
		outcome record.CleanupOutcome
	}{{"local branch", cleanup.Local}, {"fork branch", cleanup.Fork}} {
		switch side.outcome.State {
		case record.CleanupPending:
			part := side.label + " " + side.outcome.Name + " cleanup pending"
			if side.outcome.Detail != "" {
				part += ": " + side.outcome.Detail
			}
			parts = append(parts, part)
		case record.CleanupKept:
			parts = append(parts, side.label+" "+side.outcome.Name+" "+side.outcome.Detail)
		}
	}
	if len(parts) == 0 {
		return "merged; branches cleaned"
	}
	return "merged; " + strings.Join(parts, "; ")
}
