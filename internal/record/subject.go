package record

// Subject is one member of a change: a port, where it lives, and what
// was done to it.
//
// A cohort's members differ in every one of these, which is why they
// are a struct per member rather than parallel slices on the record. A
// dependent revision-bumped because its library's ABI moved carries a
// different intent, a different target and a different reason from the
// headline that caused it, and a record that flattened them would make
// the pull request body guess.
type Subject struct {
	// Port is the port's own name, as `port` would be given it.
	Port string `json:"port"`
	// Names is the port and its subports — every name a build log can
	// blame that belongs to this member.
	//
	// It exists for the subport-vs-parent blame guard: a cohort log that
	// fails on py312-foo must map to the member that owns it, and a
	// reader matching on Port alone would find no member and blame a
	// stranger, or blame nobody.
	//
	// It is written as [Port] even when the port has no subports at all,
	// because the empty slice already means something else: a reader
	// cannot otherwise tell "this port has no subports" from "nobody
	// ever asked".
	Names []string `json:"names,omitempty"`
	// Portdir is the <category>/<port> directory the change touched, on
	// the host. It is what gets staged ahead of the environment's own
	// ports tree.
	Portdir string `json:"portdir,omitempty"`
	// Intent is what was done — bump, refresh, bump-revision. It is per
	// member and not per change: a cohort's headline is a version bump
	// and its members are revision bumps, in one commit series.
	Intent string `json:"intent,omitempty"`
	// Target is what this member moved to: a version ("1.9"), a
	// re-derivation ("checksums"), a revision ("rev2").
	//
	// It is recorded so that deciding whether one change supersedes
	// another is a comparison of two values rather than a parse of two
	// branch names. Today the version survives only in the slug, and
	// reading it back out of a name is the guess this field ends.
	Target string `json:"target,omitempty"`
	// Reason is why this member is in the change, in the words that
	// reach the commit body — for a cohort member, the criterion the
	// finding measured.
	Reason string `json:"reason,omitempty"`
}
