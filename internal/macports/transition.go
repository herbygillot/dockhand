package macports

import (
	"path"
	"slices"

	"github.com/herbygillot/dockhand/internal/model"
)

// TransitionKind is how a change carries a port from the base to the
// revision.
type TransitionKind string

const (
	// TransitionKept is a port of the same name in the same directory on
	// both sides.
	TransitionKept TransitionKind = "kept"
	// TransitionAdded is a port the revision alone has.
	TransitionAdded TransitionKind = "added"
	// TransitionRemoved is a port the base alone has.
	TransitionRemoved TransitionKind = "removed"
	// TransitionMoved is a port of the same name in another directory.
	TransitionMoved TransitionKind = "moved"
	// TransitionRenamed is a port under a new name, carried on from the
	// base's port of the old.
	TransitionRenamed TransitionKind = "renamed"
)

// PortSide is one side of a port's transition: the source and directory
// it's read at, and its name there, empty where that side hasn't it.
type PortSide struct {
	Source    model.Source
	Directory string
	Name      string
}

// PortTransition is one port's before and after across a change, and
// how the one becomes the other.
type PortTransition struct {
	Before, After PortSide
	Kind          TransitionKind
}

// Transitions are the ports a change carries through one directory it
// changes: directory is the revision's, or the base's for one it removes;
// before are the names the base's directory, s.MovedFrom(directory),
// defines, and after those the revision's does, each read by the caller
// as MacPorts evaluates them, since the scope reads no Portfile. A port
// of both is kept, or moved where its directory moved. One the revision
// alone has is renamed from the base's port it carries on, found first by
// a rename's stub, which keeps the old name replaced_by the new, and else
// by the base's directory having one port, whose name the revision's no
// longer has, beside a name the base hadn't, the one named for the
// directory where there are several; any other is added. A port of the
// base alone is removed: two the revision no longer has can't be told
// apart, and jq renamed to jq2 with jq-devel dropped beside it stays an
// addition and two removals. sources are the base's and the revision's.
func (s Scope) Transitions(sources [2]model.Source, directory string, before, after []string) []PortTransition {
	from := s.MovedFrom(directory)
	same := TransitionKept
	if from != directory {
		same = TransitionMoved
	}
	var gone, fresh []string
	for _, name := range before {
		if !slices.Contains(after, name) {
			gone = append(gone, name)
		}
	}
	for _, name := range after {
		if !slices.Contains(before, name) {
			fresh = append(fresh, name)
		}
	}
	// renamed is the base's port each new name carries on.
	renamed := map[string]string{}
	stub := slices.IndexFunc(s.Moved, func(m Move) bool { return m.To == directory && m.Stub && m.Port != "" })
	switch named := path.Base(directory); {
	case stub >= 0 && slices.Contains(gone, s.Moved[stub].Port) && slices.Contains(fresh, named):
		renamed[named] = s.Moved[stub].Port
	case len(before) == 1 && len(gone) == 1 && len(fresh) == 1:
		renamed[fresh[0]] = gone[0]
	case len(before) == 1 && len(gone) == 1 && slices.Contains(fresh, named):
		renamed[named] = gone[0]
	}
	side := func(i int, directory, name string) PortSide {
		return PortSide{Source: sources[i], Directory: directory, Name: name}
	}
	var transitions []PortTransition
	carried := map[string]bool{}
	for _, name := range after {
		transition := PortTransition{Before: side(0, from, ""), After: side(1, directory, name), Kind: TransitionAdded}
		switch old, ok := renamed[name]; {
		case slices.Contains(before, name):
			transition.Before.Name, transition.Kind = name, same
		case ok:
			transition.Before.Name, transition.Kind = old, TransitionRenamed
			carried[old] = true
		}
		transitions = append(transitions, transition)
	}
	for _, name := range gone {
		if !carried[name] {
			transitions = append(transitions, PortTransition{Before: side(0, from, name), After: side(1, directory, ""), Kind: TransitionRemoved})
		}
	}
	return transitions
}

// TransitionOf is the transition of the port named so at the revision
// among transitions, and false where none carries it.
func TransitionOf(transitions []PortTransition, name string) (PortTransition, bool) {
	i := slices.IndexFunc(transitions, func(t PortTransition) bool { return t.After.Name == name })
	if i < 0 {
		return PortTransition{}, false
	}
	return transitions[i], true
}
