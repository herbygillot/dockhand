package record

import (
	"errors"
	"fmt"
)

// RunState is where one subject's run on one platform stands. Its
// underlying type is string and it deliberately carries no
// MarshalJSON or UnmarshalJSON: a state marshals as the bare word, and
// unmarshals whatever a note holds without judging it, which keeps the
// codec's refusals to the ones the reader is meant to make.
//
// These words are not verify.State's, which spell four of them the
// same way by coincidence. verify.State is a provider's answer about a
// job; a RunState is what the note remembers about a subject, and it
// records outcomes — declined, blocked, superseded — that no provider
// ever reports.
type RunState string

const (
	// Queued means the run was asked for and no slot was free. Status's
	// pump submits it when one frees, so it is a waiting room and not an
	// outcome. Schema 2 spelled this "deferred"; the word changed with
	// the schema because the note now has a state for the window between
	// claiming a slot and having a job, and "deferred" beside
	// "submitting" reads as a decision rather than a queue.
	Queued RunState = "queued"
	// Submitting means this checkout has claimed the platform and is
	// starting the environment. It is the window between the claim going
	// down and the provider handing back a job — short, and the only
	// thing standing between two sessions and two guests for the same
	// work.
	Submitting RunState = "submitting"
	// Running means a worker is building.
	Running RunState = "running"
	// Passed means the port built on that platform.
	Passed RunState = "passed"
	// Failed means it did not, which is a finding about the port.
	Failed RunState = "failed"
	// Unsupported means the port declines the platform — known_fail.
	// That refusal is often the change working, so it is not a failure.
	Unsupported RunState = "unsupported"
	// Blocked means something failed before this subject was reached: a
	// dependency, or an earlier member of the cohort. Untested, not
	// disproven.
	Blocked RunState = "blocked"
	// Canceled means a person stopped the run.
	Canceled RunState = "canceled"
	// Superseded means the branch moved out from under the run.
	Superseded RunState = "superseded"
	// Errored means the environment could not answer, which is a fact
	// about the machine and never a finding about the port.
	Errored RunState = "errored"
	// Withheld means this build deliberately did not run the subject,
	// and nothing about the subject is the reason. A cohort member that
	// declares a conflict with a member already in the guest is the case
	// it was added for: MacPorts will not activate both, so one is
	// bumped by the change and built by a verification of its own.
	//
	// It is its own word because every neighbouring one would be a
	// false statement about the port. Blocked says something failed
	// before this subject was reached, and nothing failed. Unsupported
	// says the port declines the platform, and it does not — it would
	// build alone. Queued says the pump submits it when a slot frees,
	// which is a promise nothing here will keep. Errored is about the
	// machine. What happened is that dockhand held the subject back, so
	// the state names dockhand's own act and the detail says why.
	Withheld RunState = "withheld"
)

// ErrUnknownRunState reports a state word this build does not know.
var ErrUnknownRunState = errors.New("record: unknown run state")

// ParseRunState converts a bare wire word into a RunState, refusing
// one this build does not know.
//
// Decode does not call it, and that is deliberate: a note carrying a
// word this build cannot read is still a note that governs worker
// release, and refusing to read it would strand the environment it
// names. Use this where a state arrives from outside a note — a flag,
// an operator, another tool.
func ParseRunState(s string) (RunState, error) {
	switch rs := RunState(s); rs {
	case Queued, Submitting, Running, Passed, Failed,
		Unsupported, Blocked, Canceled, Superseded, Errored, Withheld:
		return rs, nil
	}
	return "", fmt.Errorf("%w: %q", ErrUnknownRunState, s)
}

// String returns the wire word, so a state renders as the byte
// sequence the notes and the goldens carry.
func (s RunState) String() string { return string(s) }

// Weight is what a run contributes to a verdict set. The set is a
// tally, not a vote: one Positive is enough to say the change works
// somewhere, and one Negative is enough to stop it.
type Weight int

const (
	// Neutral is a run that says nothing about the change — it has not
	// finished, or it never tested the change at all.
	Neutral Weight = iota
	// Positive is a run that says the change works.
	Positive
	// Negative is a run that says it does not.
	Negative
)

// Weight reports what the state contributes to a verdict set. Only a
// pass argues for the change and only a failure argues against it:
// unsupported and blocked are refusals to test, and the rest are
// states of the run rather than findings about the port. An unknown
// state weighs Neutral, because a word this build cannot read is not
// evidence.
func (s RunState) Weight() Weight {
	switch s {
	case Passed:
		return Positive
	case Failed:
		return Negative
	case Queued, Submitting, Running, Unsupported, Blocked, Canceled, Superseded, Errored, Withheld:
		return Neutral
	}
	return Neutral
}

// Terminal reports whether the state will not change on its own.
//
// Submitting is not terminal, and that is the whole reason the state
// exists. A claimed run that read terminal would let a peer conclude
// the work was finished and start a second guest for it — the exact
// failure the claim is there to end. Queued is not terminal either:
// status's pump submits it as soon as a slot frees. An unknown state
// is treated as unfinished, which is the reading that leaves it alone.
func (s RunState) Terminal() bool {
	switch s {
	case Passed, Failed, Unsupported, Blocked, Canceled, Superseded, Errored, Withheld:
		return true
	case Queued, Submitting, Running:
		return false
	}
	return false
}

// Outcome reports whether a terminal state says something about the
// subject itself, as opposed to about the machine, a person, or the
// branch. Passed, failed, unsupported, blocked and withheld are all
// answers to "what happened to this port here"; errored, canceled and
// superseded are answers to a different question and settle nothing.
//
// This is what best-effort dependents are measured against: a dependent
// must have an outcome, not merely a terminal run. The distinction is
// the ruling's own — its argument is that a dependent's build is a fact
// about the dependent, and these three are not.
func (s RunState) Outcome() bool {
	switch s {
	case Passed, Failed, Unsupported, Blocked, Withheld:
		return true
	case Queued, Submitting, Running, Canceled, Superseded, Errored:
		return false
	}
	// An unknown state is not an outcome: a word this build cannot read
	// says nothing about the port, and the reading that waits is the
	// one that publishes nothing on it.
	return false
}

// Destination is how far one change's contract reaches, recorded when
// the change is minted.
//
// It is a string on the wire and not an iota. The engine held this as
// an int, which is fine inside a process and wrong the moment it is
// written down: a number's meaning lives in the order of a const block,
// and inserting a member renumbers every note already on disk.
type Destination string

const (
	// ToBranch is --no-verify: mint the branch and stop. A change bound
	// here is never drained — nobody asked for a verdict, so the pump
	// must not invent one.
	ToBranch Destination = "branch"
	// ToVerdict is the default: mint the branch and verify it.
	ToVerdict Destination = "verdict"
	// ToPublished carries it through to a pull request.
	ToPublished Destination = "published"
)
