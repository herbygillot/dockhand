package record

import "time"

// Disposition is what has become of a finding. A finding proposes and
// never executes, so the proposal and the answer to it are two facts
// and this is the second one.
type Disposition string

const (
	// Proposed is a finding nobody has answered yet. It is the state a
	// finding is appended in, and an unanswered one is a question the
	// change is still carrying.
	Proposed Disposition = "proposed"
	// Accepted means the proposal was taken up — the cohort was built,
	// the revision bumped.
	Accepted Disposition = "accepted"
	// Dismissed means a person looked and said no. Dismissal is an
	// answer worth recording, not an absence: a finding that vanished
	// when declined would be proposed again on the next look.
	Dismissed Disposition = "dismissed"
)

// Candidate is one port a finding examined, whether or not the finding
// proposes doing anything to it.
//
// The ports the tool declined to touch are recorded beside the ones it
// proposes, because they are exactly what a reviewer must check by
// hand: a dependent excluded for being obsolete, replaced, or already
// in flight is a decision, and a decision no reader can see is a
// decision nobody can disagree with.
type Candidate struct {
	Port string `json:"port"`
	// Portdir is where it lives, when the finding knows.
	Portdir string `json:"portdir,omitempty"`
	// Proposed says the finding puts this port forward. A candidate
	// without it was looked at and left out.
	Proposed bool `json:"proposed,omitempty"`
	// Reason is why, either way.
	Reason string `json:"reason,omitempty"`
}

// Finding is something verification noticed that nobody asked about:
// a library whose ABI moved, the dependents that would need a revision
// bump, an instruction comment in the Portfile, an upstream statement
// about compatibility.
//
// Kind is a plain string rather than a typed enumeration, and it is
// deliberately not plan.FindingKind: record is a leaf, and importing
// the planning package to name a kind would put the whole planning
// graph under everything that reads a note. A string is exactly as
// extensible and adds no coupling. What has to be ruled is the value
// set, not the import — a kind is a wire enum, and changing what one
// means is what bumps the schema number.
type Finding struct {
	Kind string `json:"kind"`
	// Ports are the ports the finding is about.
	Ports []string `json:"ports,omitempty"`
	// Candidates are the ports it examined, with what it concluded
	// about each.
	Candidates []Candidate `json:"candidates,omitempty"`
	// Criterion is the measurement in words a reader can check: which
	// install name moved, which compatibility version changed, between
	// which two builds on which platform. The mechanical criterion is
	// necessary and never sufficient, so it is stated rather than
	// implied.
	Criterion string `json:"criterion,omitempty"`
	// Source and Quote are where a non-mechanical finding came from and
	// what it actually said — an upstream release note, a comment in the
	// Portfile. A finding that cannot be traced back to its words is an
	// assertion.
	Source string `json:"source,omitempty"`
	Quote  string `json:"quote,omitempty"`
	// Disposition and At carry no omitempty. A finding with no
	// disposition on the wire would read as one nobody had to answer,
	// and the zero value of the type is not one of the three words.
	Disposition Disposition `json:"disposition"`
	At          time.Time   `json:"at"`
}
