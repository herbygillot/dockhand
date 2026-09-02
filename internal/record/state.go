package record

import (
	"errors"
	"fmt"
)

// RunState is where one platform's run stands. Its underlying type is
// string and it deliberately carries no MarshalJSON or UnmarshalJSON:
// a state marshals as the bare word schema 2 has always written, and
// unmarshals whatever a note holds without judging it, which is what
// keeps the codec's refusals to the three the reader has always made.
//
// These words are not verify.State's, which spell four of them the
// same way by coincidence. verify.State is a provider's answer about a
// job; a RunState is what the note remembers about a platform, and it
// records outcomes — declined, blocked, superseded — that no provider
// ever reports.
type RunState string

const (
	// Running means a worker is still building.
	Running RunState = "running"
	// Passed means the port built on that platform.
	Passed RunState = "passed"
	// Failed means it did not, which is a finding about the port.
	Failed RunState = "failed"
	// Unsupported means the port declines the platform — known_fail.
	// That refusal is often the change working, so it is not a failure.
	Unsupported RunState = "unsupported"
	// Blocked means a dependency failed before the change was reached:
	// untested, not disproven.
	Blocked RunState = "blocked"
	// Canceled means a person stopped the run.
	Canceled RunState = "canceled"
	// Superseded means the branch moved out from under the run.
	Superseded RunState = "superseded"
	// Deferred means there was no slot when the run was asked for.
	// Status's pump starts it when one frees, so it is not an outcome.
	Deferred RunState = "deferred"
	// Errored means the environment could not answer, which is a fact
	// about the machine and never a finding about the port.
	Errored RunState = "errored"
)

// ErrUnknownRunState reports a state word this build does not know.
var ErrUnknownRunState = errors.New("record: unknown run state")

// ParseRunState converts a bare wire word into a RunState, refusing
// one this build does not know.
//
// Decode does not call it, and that is deliberate: the reader has
// always copied a note's state through unexamined, and the schema-1
// lift can produce an empty one. Refusing inside the codec would turn
// old notes into errors. Use this where a state arrives from outside a
// note — a flag, an operator, another tool.
func ParseRunState(s string) (RunState, error) {
	switch rs := RunState(s); rs {
	case Running, Passed, Failed, Unsupported, Blocked, Canceled, Superseded, Deferred, Errored:
		return rs, nil
	}
	return "", fmt.Errorf("%w: %q", ErrUnknownRunState, s)
}

// String returns the wire word, so a state renders as the byte
// sequence the notes and the goldens already carry.
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
	case Running, Unsupported, Blocked, Canceled, Superseded, Deferred, Errored:
		return Neutral
	}
	return Neutral
}

// Terminal reports whether the state will not change on its own.
// Running will not stay; deferred will not either, because status's
// pump submits it as soon as a slot frees — a deferred run is a queued
// one, not a finished one. An unknown state is treated as unfinished,
// which is the reading that leaves it alone.
func (s RunState) Terminal() bool {
	switch s {
	case Passed, Failed, Unsupported, Blocked, Canceled, Superseded, Errored:
		return true
	case Running, Deferred:
		return false
	}
	return false
}
