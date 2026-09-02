package portstyle

import (
	"fmt"

	"github.com/herbygillot/dockhand/internal/exitcode"
	"github.com/herbygillot/dockhand/internal/macports/info"
	"github.com/herbygillot/dockhand/internal/text"
)

// DeclineType classifies why a field could not be located.
type DeclineType int

const (
	// FieldUnsupported means no recognizer exists for the requested field.
	FieldUnsupported DeclineType = iota
	// UnknownStyle means no style we recognize for the field appears in
	// any scope that applies to the context — a statement about the style
	// table's knowledge, not the Portfile's structure.
	UnknownStyle
	// NotLiteral means styles were found but no candidate's text equals
	// the evaluated value: the value is computed, or lives somewhere the
	// known styles do not reach. Candidates carries what was inspected —
	// evidence for the diagnostic, and the input to a counterfactual
	// probe, which is how a computed value's carrier can still be proven
	// (write a target into a candidate, re-evaluate, watch the value
	// move).
	NotLiteral
)

// Candidate is one span a style proposed that corroboration could not
// prove. Style and Span say what and where; Literal says whether the
// span is a plain literal a probe could rewrite — a candidate that is
// itself a substitution cannot be.
type Candidate struct {
	Style   Type
	Span    text.Span
	Literal bool
}

func (t DeclineType) String() string {
	switch t {
	case FieldUnsupported:
		return "field unsupported"
	case UnknownStyle:
		return "style not recognized"
	case NotLiteral:
		return "no literal style matches the evaluated value"
	}
	return "unknown decline"
}

// Decline is a refusal to locate, stated precisely. It is an error so it
// travels normal plumbing, and typed so callers branch on it — a decline
// is a first-class outcome, not a failure.
type Decline struct {
	Type       DeclineType
	Field      info.Field
	Candidates []Candidate
}

// Error implements the error interface. The remedy rides on the end,
// in the shape every refusal in dockhand uses — the finding, then a
// dash, then what to do about it.
func (d *Decline) Error() string {
	msg := fmt.Sprintf("portstyle: %s: %s", d.Field, d.Type)
	if remedy := d.Remedy(); remedy != "" {
		msg += " — " + remedy
	}
	return msg
}

// Code names the decline for a machine: the twin's reason. Prefixed,
// because a location decline and a plan decline are different findings
// and a script filtering on the token must not confuse them.
func (d *Decline) Code() string {
	switch d.Type {
	case FieldUnsupported:
		return "style-field-unsupported"
	case UnknownStyle:
		return "style-unknown"
	case NotLiteral:
		return "style-not-literal"
	}
	return "style-unknown-decline"
}

// Remedy is what the user can do about it, in one clause.
//
// It hangs on the *Decline rather than on the type, because the useful
// sentence depends on the field: a Portfile with no revision line is a
// placement problem — dockhand has no convention for inserting one,
// and near a fifth of the tree carries none — while an unrecognized
// version carrier is a style problem. Told apart by the type alone,
// one of them gets the other's advice.
func (d *Decline) Remedy() string {
	switch d.Type {
	case FieldUnsupported:
		return "dockhand does not locate this field; edit the Portfile by hand"
	case UnknownStyle:
		if d.Field == info.FieldRevision {
			return "add a `revision` line to the Portfile yourself; dockhand will not guess where one belongs"
		}
		return "the field is not written in a style dockhand recognizes; edit the Portfile by hand"
	case NotLiteral:
		return "the value is computed rather than written; edit what computes it"
	}
	return ""
}

// DockhandExit: a decline is a successful judgment, and it exits in the
// declined band with the planner refusals it sits one level under —
// location failed, so no plan could be trusted.
func (d *Decline) DockhandExit() int { return exitcode.PlanDeclined }
