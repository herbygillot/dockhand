package github

import (
	"slices"
	"strings"
	"time"

	"github.com/herbygillot/dockhand/internal/forge"
)

// The one reading of a pull request's reviews and checks, which the REST
// inspection and the GraphQL reading each decided apart (R3-24, the
// architecture review of 5897a82): what a review's state counts as, which
// review sets the time, and what a check run's or a status's outcome
// counts as. CI's classification by cause reads what these give.

// opinion is a reviewer's latest review that holds an opinion, as REST
// lists them once each reviewer's latest is kept and GraphQL's
// latestOpinionatedReviews gives them: approved, changes requested, or
// dismissed, which counts for nothing.
type opinion struct {
	Login, State string
	At           time.Time
}

// review is the reviews' standing: approvals, the reviewers asking for
// changes, which of the two holds, and when the newest review that sets
// it was made, which status says, rather than when dockhand read it (a
// refresh read a nine-hour-old request for changes as "just now", field
// testing, 2026-10-02).
func review(status *forge.PullRequestStatus, opinions []opinion) {
	for _, o := range opinions {
		switch strings.ToUpper(o.State) {
		case "APPROVED":
			status.Approvals++
		case "CHANGES_REQUESTED":
			status.ChangesRequested++
			status.ChangesRequestedBy = append(status.ChangesRequestedBy, o.Login)
		}
	}
	slices.Sort(status.ChangesRequestedBy)
	switch {
	case status.ChangesRequested > 0:
		status.Review = "changes-requested"
	case status.Approvals > 0:
		status.Review = "approved"
	}
	for _, o := range opinions {
		state := strings.ToUpper(o.State)
		if (state == "APPROVED" || state == "CHANGES_REQUESTED") && (state == "CHANGES_REQUESTED") == (status.Review == "changes-requested") && o.At.After(status.ReviewedAt) {
			status.ReviewedAt = o.At.UTC()
		}
	}
}

// check counts one check of the head: a check run by its status and
// conclusion, or a commit status by its state, each named. A run that
// waits on a maintainer's approval, as a first-time contributor's workflow
// does, is pending, not failed (the rc6 full stage's F1).
func check(status *forge.PullRequestStatus, run bool, name, state, conclusion string) {
	status.Checks.Total++
	state, conclusion = strings.ToUpper(state), strings.ToUpper(conclusion)
	switch {
	case run && (state != "COMPLETED" || conclusion == "ACTION_REQUIRED"):
		status.Checks.Pending++
	case run && (conclusion == "SUCCESS" || conclusion == "NEUTRAL" || conclusion == "SKIPPED"):
		status.Checks.Passed++
	case !run && state == "SUCCESS":
		status.Checks.Passed++
	case !run && (state == "PENDING" || state == "EXPECTED"):
		status.Checks.Pending++
	default:
		status.Checks.Failed++
		status.Checks.Failing = append(status.Checks.Failing, name)
	}
}
