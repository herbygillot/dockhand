package record

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// ReferenceRelation is how a contribution relates to a ticket: it closes
// it, or it is worth reading beside it.
type ReferenceRelation string

const (
	ReferenceCloses ReferenceRelation = "closes"
	ReferenceSee    ReferenceRelation = "see"
)

// Key is the trailer key MacPorts commits use for the relation; empty for
// a relation dockhand does not write.
func (r ReferenceRelation) Key() string {
	switch r {
	case ReferenceCloses:
		return "Closes"
	case ReferenceSee:
		return "See"
	}
	return ""
}

// Reference is one ticket a contribution commit cites in its trailers, in
// the form MacPorts asks for: a full URL under a Closes: or See: key.
type Reference struct {
	Relation ReferenceRelation
	URL      string
}

// Trailer renders the commit trailer line for the reference.
func (r Reference) Trailer() string {
	return r.Relation.Key() + ": " + r.URL
}

// Valid reports whether the reference can be written as one trailer line.
func (r Reference) Valid() bool {
	return r.Relation.Key() != "" && r.URL != "" && utf8.ValidString(r.URL) && strings.IndexFunc(r.URL, func(c rune) bool { return unicode.IsSpace(c) || unicode.IsControl(c) }) < 0
}

// ParseReferenceTrailer reads a trailer line dockhand or a person wrote,
// reporting whether it is one dockhand would write itself.
func ParseReferenceTrailer(line string) (Reference, bool) {
	key, value, ok := strings.Cut(strings.TrimSpace(line), ":")
	if !ok {
		return Reference{}, false
	}
	value = strings.TrimSpace(value)
	for _, relation := range []ReferenceRelation{ReferenceCloses, ReferenceSee} {
		if key == relation.Key() {
			reference := Reference{Relation: relation, URL: value}
			return reference, reference.Valid()
		}
	}
	return Reference{}, false
}
