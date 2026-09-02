package verdict

import (
	"fmt"
	"strings"

	"github.com/herbygillot/dockhand/internal/exitcode"
	"github.com/herbygillot/dockhand/internal/record"
)

// DuplicatePRError is promote's refusal when an open upstream PR
// already claims the same change: a duplicate spends reviewer
// attention on the purest kind of waste. Refusal with a remedy (exit
// 5), not a failure — the other PR may be theirs to join, or
// --no-pr-check promotes past it deliberately.
type DuplicatePRError struct {
	Title string
	URL   string
}

func (e *DuplicatePRError) Error() string {
	return fmt.Sprintf("an open PR already proposes %q: %s — join it, retitle with --title, or --no-pr-check to promote anyway", e.Title, e.URL)
}

// ExitCode: a duplicate refusal is a decline with a remedy.
func (e *DuplicatePRError) ExitCode() int { return exitcode.Declined }

// PublishDecision is what promote's verification gate concluded.
type PublishDecision struct {
	// Refusal stops the promotion. It is a plain error on purpose: the
	// one thing that refuses here is a completed failed build, which is
	// the operation failing rather than a policy declining it, and it
	// exits in the failure band.
	Refusal error
	// Blocked is one advisory per blocked platform, in the record's
	// stable platform order, for stderr.
	Blocked []string
	// SayUnverified asks the caller to say so before it publishes.
	SayUnverified bool
}

// DecidePublish is promote's gate.
//
// Invoking promote is already the publication choice, so an unverified
// branch promotes with a complaint and never a demanded flag. The one
// refusal that remains is distinct NEGATIVE evidence: a completed failed
// build, which --no-verify alone overrides. Everything else — nothing
// verified, a run still queued, a platform that declined — passes, and
// the PR body says what the evidence was.
//
// A blocked run is the one unverified shape with a story worth telling:
// the change is untested because a neighbour is broken, and the
// maintainer deciding to promote anyway deserves the name of the
// neighbour in front of them.
//
// tipAbbrev is the tip's shortened sha, abbreviated by the caller.
func DecidePublish(r record.Record, promotable bool, branch, tipAbbrev string, noVerify bool) PublishDecision {
	if promotable {
		return PublishDecision{}
	}
	if r.AnyState(record.Failed) && !noVerify {
		return PublishDecision{Refusal: fmt.Errorf(
			"%s: tip %s has a failed verification — fix it, `dockhand discard` it, or --no-verify to promote anyway",
			branch, tipAbbrev)}
	}
	d := PublishDecision{SayUnverified: true}
	for _, plat := range r.Platforms() {
		if run := r.Runs[plat]; run.State == record.Blocked {
			d.Blocked = append(d.Blocked, fmt.Sprintf("verification blocked on %s: %s", plat, run.Detail))
		}
	}
	return d
}

// MergedDeadEnd refuses a promotion whose own PR already merged: there
// is nothing left to publish, and pushing to that branch would resurrect
// work the project has already taken. Nil when the branch's PR is
// anything else, including absent.
//
// A plain error, like the failed-verification refusal: the branch is in
// a state promote cannot act on, which is the operation failing.
func MergedDeadEnd(pr PRFact, branch string) error {
	if !pr.Found || !pr.Merged {
		return nil
	}
	return fmt.Errorf("PR #%d for %s already merged (%s) — `dockhand clean` retires the branch",
		pr.Number, branch, pr.URL)
}

// PortName is the port to search open pull requests by: the one the
// note names, or — for a branch with no note at all — the prefix of the
// title, leaning on the project convention that a title is
// `<port>: <description>`. Empty when neither answers, which is the
// caller's cue to skip the duplicate search rather than run an
// unbounded one.
func PortName(recorded, title string) string {
	port := recorded
	if before, _, found := strings.Cut(title, ":"); port == "" && found {
		port = strings.TrimSpace(before)
	}
	return port
}

// DuplicateCheck is what the same-port search concluded.
type DuplicateCheck struct {
	// Refusal is the duplicate, when one was found.
	Refusal error
	// Notes are the same-port-different-change advisories, for stderr.
	// They are returned even alongside a refusal, and the caller prints
	// them before returning it: the search reports each PR as it walks
	// past, so the advisories for everything ahead of the duplicate are
	// already said by the time it is found.
	Notes []string
}

// CheckDuplicates walks the open pull requests that claim the same port.
// A title matching this promotion's, case- and space-insensitively, is
// the duplicate; anything else on the same port is not a duplicate but
// is worth knowing now rather than at review, because a maintainer
// coordinating both changes will want to.
//
// own is this branch's own pull request, skipped by number: re-promoting
// updates that PR in place, and matching against it would refuse the
// branch for duplicating itself.
func CheckDuplicates(open []PRFact, own PRFact, title string) DuplicateCheck {
	var d DuplicateCheck
	for _, pr := range open {
		if own.Found && pr.Number == own.Number {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(pr.Title), strings.TrimSpace(title)) {
			d.Refusal = &DuplicatePRError{Title: pr.Title, URL: pr.URL}
			return d
		}
		d.Notes = append(d.Notes, fmt.Sprintf("note: an open PR already touches this port: #%d %q (%s)",
			pr.Number, pr.Title, pr.URL))
	}
	return d
}
