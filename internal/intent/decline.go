// Package intent holds the vocabulary every planner shares: the typed
// Decline refusals (a decline is a first-class outcome, mapped to its
// own exit code), the Shadow helper that materializes a portdir copy
// with edits applied, and the Fetcher seam planners pull checksums
// through. Each intent lives in its own package — bump is the first —
// and speaks this vocabulary at its boundaries.
package intent

import "fmt"

// DeclineType classifies why a planner refused to produce a plan. These
// extend portstyle's location declines one level up: location succeeded,
// but the plan the intent would produce cannot be trusted.
type DeclineType int

const (
	// AlreadyCurrent means the port is already at the requested version.
	AlreadyCurrent DeclineType = iota
	// TransformedStyle means the version carrier writes its literal in a
	// transformed form (perl5), so the new literal cannot be derived
	// from the requested version alone.
	TransformedStyle
	// FetchNotDriven means the version moved but nothing fetch-derived
	// moved with it: the version does not drive the fetch (a pinned
	// ref straddle), and bumping it alone would fetch the old source.
	FetchNotDriven
	// ChecksumsNotLocated means the recorded checksums could not all be
	// matched to editable literal spans.
	ChecksumsNotLocated
	// SubportsChanged means the shadow evaluation showed subports
	// appearing or disappearing — beyond what a bump may do.
	SubportsChanged
	// VersionNotReached means the shadow evaluation did not show the
	// version arriving at the requested value.
	VersionNotReached
	// UnexpectedChange means a field unrelated to the bump moved.
	UnexpectedChange
	// LatestUnresolved means "latest" could not be trusted: the upstream
	// resolvers disagreed, rotted, or produced no signal.
	LatestUnresolved
	// VendoredBlock means the port carries a vendored dependency block
	// (go.vendors, cargo.crates) that a version bump must regenerate —
	// the vendor intent's job, not yet built.
	VendoredBlock
)

func (t DeclineType) String() string {
	switch t {
	case AlreadyCurrent:
		return "already at the requested version"
	case TransformedStyle:
		return "carrier style transforms its literal"
	case FetchNotDriven:
		return "version does not drive the fetch"
	case ChecksumsNotLocated:
		return "checksums could not be located for editing"
	case SubportsChanged:
		return "subports would appear or disappear"
	case VersionNotReached:
		return "version would not reach the requested value"
	case UnexpectedChange:
		return "an unrelated field would change"
	case LatestUnresolved:
		return "latest could not be resolved"
	case VendoredBlock:
		return "vendored dependency block requires regeneration"
	}
	return "unknown decline"
}

// Decline is a planner's refusal, stated precisely. Like portstyle's,
// it is an error so it travels normal plumbing and typed so callers
// branch on it; a decline is a first-class outcome, not a failure.
type Decline struct {
	Type   DeclineType
	Detail string
}

// Error implements the error interface.
func (d *Decline) Error() string {
	if d.Detail == "" {
		return fmt.Sprintf("intent: declined: %s", d.Type)
	}
	return fmt.Sprintf("intent: declined: %s: %s", d.Type, d.Detail)
}
