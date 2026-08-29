package portstyle

import (
	"fmt"

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
	// known styles do not reach. Candidates carries the spans inspected,
	// as evidence for the diagnostic.
	NotLiteral
)

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
	Candidates []text.Span
}

// Error implements the error interface.
func (d *Decline) Error() string {
	return fmt.Sprintf("portstyle: %s: %s", d.Field, d.Type)
}
