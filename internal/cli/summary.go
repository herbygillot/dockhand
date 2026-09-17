package cli

import (
	"bytes"
	"fmt"
	"io"
	"maps"
	"slices"
	"strings"

	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/workflow"
)

// renderSummary prints the info-level result of an action: for each job its
// port, what changed, the branch, the verdict per platform, and the pull
// request. Identifiers and the full record stay behind -v in renderStatus.
func renderSummary(out io.Writer, status workflow.Status) error {
	var buffer bytes.Buffer
	line := func(format string, values ...any) {
		for i, value := range values {
			values[i] = plain(fmt.Sprint(value))
		}
		fmt.Fprintf(&buffer, format+"\n", values...)
	}
	if len(status.Jobs) == 0 {
		line("No recorded jobs.")
	}
	for i, entry := range status.Jobs {
		if i > 0 {
			line("")
		}
		job := entry.Job
		line("%s: %s", jobPort(job), jobHeadline(entry))
		if prepared := job.Prepared; prepared != nil {
			if job.ResultRevision != "" {
				line("  branch: %s", prepared.Branch)
			} else {
				line("  candidate branch: %s (integration unconfirmed)", prepared.Branch)
			}
			for _, problem := range prepared.PatchProblems {
				line("  patch: %s", problem)
			}
		}
		if release := job.ResolvedRelease; release != nil && release.LeavesStable {
			line("  warning: %s is a prerelease; this takes the port out of stable", release.Version)
		}
		if reused := entry.Reused; reused != nil && reused.Evidence != nil && reused.Evidence.Verdict == record.VerdictPassed {
			line("  passed on %s (reused from an earlier build)", platformLabel(reused.Spec.Config.Platform))
		}
		for _, attempt := range entry.Attempts {
			line("  %s", attemptLine(job, attempt))
		}
		if pr := pullRequestLine(entry, status.PullRequests); pr != "" {
			line("  %s", pr)
		}
		if job.Detail != "" && (job.State == record.JobFailed || job.State == record.JobNeedsAttention || job.State == record.JobCanceled || job.State == record.JobSuperseded) {
			line("  %s", job.Detail)
		}
		if pending := pendingGuidance(job); pending != "" {
			line("")
			line("%s", pending)
		}
	}
	_, err := io.Copy(out, &buffer)
	return err
}

// jobPort names a job by its first target and explicit variants, never by ID.
func jobPort(job record.Job) string {
	if len(job.Spec.Targets) == 0 {
		return string(job.ID)
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

// jobHeadline says what the job does and where it stands: "1.7 -> 1.8.1; verified".
func jobHeadline(entry workflow.JobStatus) string {
	job := entry.Job
	var what string
	switch job.Spec.Action {
	case record.Bump:
		if release := job.ResolvedRelease; release != nil {
			what = versionMove(release)
		} else {
			what = "version bump"
		}
	case record.BumpRevision:
		what = "revision bump"
	case record.RefreshChecksums:
		what = "checksum refresh"
	case record.Verify:
		what = "verification"
	case record.Publish:
		what = "publication"
	default:
		what = string(job.Spec.Action)
	}
	return what + "; " + jobState(entry)
}

// versionMove words a resolved release as the change a person sees.
func versionMove(release *record.Release) string {
	if release.NoUpdate {
		return fmt.Sprintf("already current at %s; latest eligible version is %s", release.CurrentVersion, release.Version)
	}
	if release.CurrentVersion == "" {
		return "-> " + release.Version
	}
	return release.CurrentVersion + " -> " + release.Version
}

func jobState(entry workflow.JobStatus) string {
	job := entry.Job
	switch job.State {
	case record.JobQueued:
		return "queued"
	case record.JobActive:
		switch job.Phase {
		case record.PhasePreparation:
			return "preparing"
		case record.PhaseVerification:
			return "verifying"
		case record.PhasePublication:
			return "publishing"
		}
		return "in progress"
	case record.JobCompleted:
		if job.ResolvedRelease != nil && job.ResolvedRelease.NoUpdate {
			return "no update needed"
		}
		if job.Spec.Destination == record.Published && len(entry.Publications) > 0 && entry.Publications[0].State == record.PublicationConfirmed {
			return "published"
		}
		if job.Spec.Destination == record.BranchReady && job.ResultRevision != "" {
			return "branch ready"
		}
		if strings.HasPrefix(completedOutcome(entry), "verification passed") {
			return "verified"
		}
		return "completed"
	case record.JobNeedsAttention:
		return "needs attention"
	}
	return string(job.State)
}

func platformLabel(platform record.Platform) string {
	return strings.TrimSpace(strings.Join([]string{platform.OS, platform.Version, platform.Architecture}, " "))
}

// attemptLine is one build's verdict or progress on its platform, with the
// failing phase and the log location when it failed.
func attemptLine(job record.Job, attempt record.Attempt) string {
	platform := platformLabel(attempt.Spec.Config.Platform)
	var prefix string
	if name := attempt.Spec.Target.Name; name != "" && (len(job.Spec.Targets) == 0 || name != job.Spec.Targets[0].Name) {
		prefix = name + ": "
	}
	if evidence := attempt.Evidence; evidence != nil && evidence.Verdict != "" {
		text := prefix + string(evidence.Verdict) + " on " + platform
		if failure := evidence.Failure; failure != nil {
			if failure.Phase != "" {
				text += "; " + failure.Phase + " phase"
			}
			if failure.Package != "" && failure.Package != attempt.Spec.Target.Name {
				text += " of " + failure.Package
			}
			if failure.Detail != "" {
				text += ": " + failure.Detail
			}
		}
		for _, log := range evidence.Logs {
			if log.Location != "" {
				text += "; log: " + log.Location
				break
			}
		}
		return text
	}
	var text string
	switch attempt.State {
	case record.AttemptQueued:
		text = "waiting for a build slot on " + platform
	case record.AttemptSubmitting, record.AttemptRunning:
		text = "building on " + platform
	case record.AttemptUncertain:
		text = "outcome uncertain on " + platform
	default:
		text = string(attempt.State) + " on " + platform
	}
	if attempt.LastError != "" {
		text += "; " + attempt.LastError
	}
	return prefix + text
}

// pullRequestLine names the PR a publication confirmed, or the recorded PR of
// the job's contribution, with its observed state.
func pullRequestLine(entry workflow.JobStatus, pulls []record.PullRequest) string {
	for _, publication := range entry.Publications {
		if publication.State != record.PublicationConfirmed {
			continue
		}
		verb := "created"
		if publication.Spec.ExpectedPR != nil {
			verb = "updated"
		}
		for _, pr := range pulls {
			if pr.ChangeID == entry.Job.ChangeID && pr.Ref.URL != "" {
				return fmt.Sprintf("PR %s (%s)", pr.Ref.URL, verb)
			}
		}
		return "PR " + verb
	}
	if entry.Job.ChangeID == "" {
		return ""
	}
	for _, pr := range pulls {
		if pr.ChangeID != entry.Job.ChangeID || pr.Ref.URL == "" {
			continue
		}
		text := fmt.Sprintf("PR %s: %s", pr.Ref.URL, pr.State)
		if pr.Status != nil {
			text += "; " + pr.Status.Summary()
		}
		return text
	}
	return ""
}

// pendingGuidance tells a detached person how to resume work still running.
func pendingGuidance(job record.Job) string {
	if job.State != record.JobActive && job.State != record.JobQueued {
		return ""
	}
	pending := "Work remains pending."
	if job.Spec.Destination == record.Published {
		pending = "PR publication remains pending."
	}
	resume := "dockhand wait --job " + string(job.ID)
	if len(job.Spec.Targets) > 0 {
		resume = "dockhand wait " + job.Spec.Targets[0].Name
	}
	return fmt.Sprintf("%s A running driver must settle the result and perform cleanup. Resume with %s or run dockhand start for this repository.", pending, resume)
}
