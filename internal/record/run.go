package record

import (
	"time"

	"github.com/herbygillot/dockhand/internal/artifact"
)

// RunState is where one subject's run on one platform stands. Its
// underlying type is string and it deliberately carries no
// MarshalJSON or UnmarshalJSON: a state marshals as the bare word, and
// unmarshals whatever a note holds without judging it, which keeps the
// codec's refusals to the ones the reader is meant to make.
type RunState string

const (
	// Queued means the run was asked for and no slot was free. The drain
	// starts it when one frees, so it is a waiting room and not an
	// outcome.
	Queued RunState = "queued"
	// Submitting means this checkout has claimed the platform and is
	// starting the environment. It is the window between the claim going
	// down and the provider handing back a lease — short, and the only
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
	// Canceled means a person stopped the run. The typed cause is on the
	// attempt's Interrupt; this is the verdict the one judge wrote from
	// it.
	Canceled RunState = "canceled"
	// Superseded means the branch moved out from under the run.
	Superseded RunState = "superseded"
	// Errored means the environment could not answer, which is a fact
	// about the machine and never a finding about the port.
	Errored RunState = "errored"
	// Faulted means the environment was FINE and dockhand's own
	// apparatus did not deliver an answer: the runner that did not
	// start, the cohort member the guest never announced.
	//
	// It is its own word for the reason Withheld is. Errored says the
	// machine could not answer, and the machine answered every question
	// it was asked — a guest that reported no state was RUNNING when it
	// was asked, and a member nobody announced sat inside a guest that
	// PASSED. Failed says the port does not build, and nothing here
	// looked at the port. Filing this under Errored told a person to go
	// and fix their machine over a defect in dockhand, and released the
	// one environment that could have proved it.
	//
	// So the state names dockhand's own machinery and the detail says
	// which part of it, which is the same rule Withheld follows for
	// dockhand's deliberate acts. This is the accidental one.
	Faulted RunState = "faulted"
	// Withheld means this build deliberately did not run the subject,
	// and nothing about the subject is the reason. A cohort member that
	// declares a conflict with a member already in the guest is the case
	// it was added for: MacPorts will not activate both, so one is
	// bumped by the change and simply not built here. It is owed nothing
	// further; the person is told, and may choose to force it.
	//
	// It is its own word because every neighbouring one would be a
	// false statement about the port. Blocked says something failed
	// before this subject was reached, and nothing failed. Unsupported
	// says the port declines the platform, and it does not — it would
	// build alone. Queued says the drain submits it when a slot frees,
	// which is a promise nothing here will keep. Errored is about the
	// machine. What happened is that dockhand held the subject back, so
	// the state names dockhand's own act and the detail says why.
	Withheld RunState = "withheld"
)

// Terminal reports whether the state will not change on its own.
//
// Submitting is not terminal, and that is the whole reason the state
// exists. A claimed run that read terminal would let a peer conclude
// the work was finished and start a second guest for it — the exact
// failure the claim is there to end. Queued is not terminal either: the
// drain submits it as soon as a slot frees.
//
// The three unfinished states are named and everything else is
// terminal, which is the opposite of enumerating the terminal ones and
// falling through to false. It is deliberate: the question this answers
// is "may work still move this", and only a state this build KNOWS is
// still moving can answer yes. A word from another shape has no drain
// behind it and no provider that will ever settle it, so reading it as
// unfinished would hold a lease open on nothing.
func (s RunState) Terminal() bool {
	//nolint:exhaustive // naming only the unfinished states is the point: a word from another shape has no drain behind it and must read terminal, where falling through to false would hold a lease open on nothing
	switch s {
	case Queued, Submitting, Running:
		return false
	}
	return true
}

// Ask is what was requested of a run, and it is a separate struct from
// what came back. Today these four live beside the outcome fields on
// one Run, which is why a deferred run — one with no environment yet —
// has to carry its caller's --keep-env in the outcome so that a later
// cycle can find it. An ask that outlives the process that made it is a
// durable request, and this is where it lives.
type Ask struct {
	// Test says the submission includes the port's test suite after the
	// install. A promotion's checklist vouches only for what a note
	// remembers, and the test is asked for per environment.
	Test bool `json:"test,omitempty"`
	// KeepEnv says the person who started this run asked for its
	// environment to stand after a pass — `--keep-env` on `verify` and
	// the bump family (D27). The failure path keeps by rule; this keeps
	// by request, and it is honoured wherever release is decided.
	KeepEnv bool `json:"keep_env,omitempty"`
	// FromSource says the port's binary archive is to be ignored and the
	// build run from source. A version bump does not need it: the new
	// version's archive does not exist yet. A re-derivation at an
	// unchanged version does, because the archive that matches predates
	// the change, and a pass earned against it verified nothing.
	FromSource bool `json:"from_source,omitempty"`
	// Forced names the member the environment must deactivate
	// immediately before this subject is built, and is empty for every
	// ordinary run. It is set on exactly one shape: a cohort member D24
	// would have withheld — MacPorts will not activate it beside a
	// sibling the cohort seats — that a person overrode and had built
	// anyway, last, with the sibling taken out of the active set first.
	// The sibling's own run is untouched by it: the sibling was built and
	// judged before the deactivation, and that verdict stands.
	Forced string `json:"forced,omitempty"`
}

// Run is one verification of one subject on one platform: what was
// asked, and what came back. Manifest and probe values come from the
// artifact package rather than from verify, for the same reason LeaseID
// is declared here.
type Run struct {
	Ask   Ask      `json:"ask,omitzero"`
	State RunState `json:"state"`
	// Content is the digest of the file set this verdict was earned
	// against, so a reader a week later can tell whether the evidence
	// describes the tip it is attached to.
	Content ContentID `json:"content"`

	// Detail explains the state in the words a person reads — why the
	// environment could not answer, which slot limit was hit. Nothing
	// decides from it: a cause a caller acts on is a typed field
	// somewhere, and Attempt.Interrupt is where the last one went.
	Detail string `json:"detail,omitempty"`
	// Blamed names the subject whose failure this one inherited. A
	// member whose prerequisite failed is skipped rather than built,
	// and it is blocked rather than disproven; naming the prerequisite
	// is the difference between "untested" and "untested because of
	// libwidget".
	Blamed string `json:"blamed,omitempty"`
	// Evidence is what a pass proves, in the provider's own words —
	// "built in a pristine VM", or something weaker from a backend whose
	// runners carry whatever the last job left. It is stamped from the
	// provider's capabilities as the run settles rather than looked up
	// at render time, because the claim belongs to the environment that
	// was actually used and providers get reconfigured.
	Evidence string `json:"evidence,omitempty"`
	// Lint is nil when no lint ran and non-nil when one did, which is
	// one field where today there are two — a Linted bool beside a Lint
	// string, so "linted, and it said nothing" and "not linted" are
	// spelled by a combination rather than by a value.
	Lint *string `json:"lint,omitempty"`
	// Manifest is what the install laid down, collected from inside the
	// environment that built it.
	Manifest *artifact.Manifest `json:"manifest,omitempty"`
	// Baseline is the same picture of what the change is measured
	// against. Both are pointers because both absences are real and mean
	// different things: a port never installed has no baseline, and a
	// build that did not get far enough to install produced nothing to
	// measure.
	Baseline *artifact.Manifest `json:"baseline,omitempty"`
	// BaselineSource says where the baseline came from — a binary
	// archive, a banked manifest, the machine's own install. The same
	// difference means different things depending on the answer, and a
	// reader that could not tell would report a stale baseline's age as
	// this change's doing.
	BaselineSource string `json:"baseline_source,omitempty"`
	// BaselineReason is WHY there is no baseline, in the environment's own
	// words, and empty when there is one.
	//
	// It rides beside BaselineSource because "none" alone is the shape of
	// a guess: a port that did not exist at the merge base, an archive
	// that was never published, a capture that was cut off and a
	// merge-base portdir that would not stage are four facts with four
	// remedies. run.Manifests.Reason says exactly that and has said it all
	// along; what was missing was anywhere durable to put it, so
	// judge.stamp wrote Source and dropped Reason on the floor.
	//
	// Measured: a real settlement recorded baseline_source "none" with
	// nothing to explain it, the ABI comparison declined for want of a
	// before, the cohort proposal declined on the ABI comparison, and no
	// sentence anywhere said why — for a port whose maintainer comment
	// asks in so many words for its dependents to be revbumped.
	BaselineReason string `json:"baseline_reason,omitempty"`
	// Links are the link-proof lines: which installed files bind to
	// which library, in the words a reader can check. The provider
	// gathers every install name mapped to its dependents, because the
	// whole installation is only present at once inside the environment;
	// what the note keeps is the conclusion drawn from it, already
	// attributed and already worded.
	Links []string `json:"links,omitempty"`
	// Probes are the port's own binaries run in the environment and
	// what they said: the cheapest evidence that a build which succeeded
	// also produced something that runs. Each line carries the argv
	// beside the output, because output with no visible provenance is
	// not evidence.
	Probes []artifact.Probe `json:"probes,omitempty"`
	At     time.Time        `json:"at,omitzero"`
}

// PublicationState is the note's readable copy of what became of the
// change on the forge. The authority is the Publication row in the
// state ref; this is the projection a person reads on the commit.
type PublicationState struct {
	Number      int       `json:"number,omitempty"`
	URL         string    `json:"url,omitempty"`
	PublishedBy Driver    `json:"published_by,omitempty"`
	PublishedAt time.Time `json:"published_at,omitzero"`
	// Unproven is how many members the change published without a pass
	// — dependents that failed, were blocked, or were withheld. Zero, and
	// omitted, for a change where everything built.
	Unproven int `json:"unproven,omitempty"`
}
