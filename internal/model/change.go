package model

import "time"

// ChangeRecord is what a revision changed in one port directory, against
// the revision's base, as MacPorts evaluates both (the evaluated-change
// model's step 1): per subport, the evaluated fields that moved, from
// what, to what. Like an assessment, it's the revision's, by its tree and
// base, whoever made the change: an update, a hand edit, or a rebase. It
// applies to the revision's files, its base, and the policy it was made
// under. A directory's text changing says it may have changed; its record
// says which of its subports did.
type ChangeRecord struct {
	Branch    BranchID
	Tree      ObjectID
	Base      ObjectID
	Directory string
	// Platform is the context both sides were evaluated in: this Mac's.
	Platform Platform
	// Ports are the directory's subports at the base and the revision,
	// main port first, each with what moved.
	Ports []SubportChange
	// Problem says what kept the record from being made, where something
	// did: a side that couldn't be evaluated. Its readers then take the
	// directory's text scope, as without a record.
	Problem string
	Policy  int
	At      time.Time
}

// SubportChange is one subport's part of a change record.
type SubportChange struct {
	Port string
	Kind SubportChangeKind
	// Fields are the evaluated fields that moved, normalized for where
	// each side was evaluated.
	Fields []FieldChange `json:",omitempty"`
	// Unstable are fields that moved where they're known to move without
	// an edit; they don't make the subport changed, and are said so as
	// not to be dropped silently.
	Unstable []FieldChange `json:",omitempty"`
}

// SubportChangeKind is how a subport stands against the base.
type SubportChangeKind string

const (
	SubportAdded     SubportChangeKind = "added"
	SubportRemoved   SubportChangeKind = "removed"
	SubportChanged   SubportChangeKind = "changed"
	SubportUnchanged SubportChangeKind = "unchanged"
)

// FieldChange is one evaluated field that moved: an option, the version,
// revision, or epoch, the dependencies, or the fetch's kind.
type FieldChange struct {
	Field    string
	From, To string
}

// Changed are the subports the revision changes: added, removed, or
// changed, in the record's order. A record with a Problem names none of
// its own; its readers take the directory's text scope.
func (r ChangeRecord) Changed() []string {
	var names []string
	for _, port := range r.Ports {
		if port.Kind != SubportUnchanged {
			names = append(names, port.Port)
		}
	}
	return names
}

// RevisionOnly says whether the record changes a subport by its revision
// alone, as a rebuild does.
func (r ChangeRecord) RevisionOnly(port string) bool {
	for _, p := range r.Ports {
		if p.Port == port {
			return p.Kind == SubportChanged && len(p.Fields) == 1 && p.Fields[0].Field == "revision"
		}
	}
	return false
}

// Validate checks the rules every recorded change keeps.
func (r ChangeRecord) Validate() error {
	switch {
	case r.Branch == "" || r.Tree == "" || r.Base == "":
		return invalid("a change record names its branch, its revision's tree, and its base")
	case r.Directory == "":
		return invalid("a change record names its directory")
	case r.Policy < 1:
		return invalid("change record of %s has no policy version", r.Directory)
	}
	for _, port := range r.Ports {
		switch port.Kind {
		case SubportAdded, SubportRemoved, SubportChanged, SubportUnchanged:
		default:
			return invalid("change record of %s: %s is %q", r.Directory, port.Port, port.Kind)
		}
	}
	return nil
}
