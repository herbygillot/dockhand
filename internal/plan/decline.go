package plan

import (
	"fmt"
	"strings"

	"github.com/herbygillot/dockhand/internal/exitcode"
)

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
	// TargetNotReached means the shadow evaluation did not show the
	// edited field arriving at the requested value — a bump's version,
	// a revbump's revision.
	TargetNotReached
	// UnexpectedChange means a field unrelated to the bump moved.
	UnexpectedChange
	// LatestUnresolved means "latest" could not be trusted: the upstream
	// resolvers disagreed, rotted, or produced no signal.
	LatestUnresolved
	// VendoredBlock means the port carries a vendored dependency block
	// this intent cannot regenerate. bump regenerates cargo.crates and
	// declines the rest; refresh subtracts a cargo block's records and
	// declines go.vendors.
	VendoredBlock
	// RevisionShapeAmbiguous means the Portfile carries no revision line
	// and its own shape does not say where one belongs. A revision line
	// sits under the line that carries the version, and dockhand writes
	// it there when the Portfile admits exactly one such line; every
	// other shape — subports that would all move together, a version
	// carried by a set variable, a carrier inside a conditional — is
	// named here rather than guessed at. The Detail says which.
	RevisionShapeAmbiguous
	// PatchWontRelocate means a patch the port applies does not carry
	// over to the new source by the one move dockhand will make for it:
	// every hunk's before-block found once, verbatim, somewhere in the
	// new file, with only its line numbers rewritten. A hunk whose lines
	// are not there, or are there twice, or a file the new source does
	// not carry at all, is this decline — the whole patch, not the hunk,
	// because half a refreshed patch is a patch nobody wrote. No fuzz
	// and no whitespace tolerance, by ruling: anything past a verbatim
	// relocation is a person's judgment about what the patch was for.
	// The Detail names the patch, the file, the hunk and why.
	PatchWontRelocate
)

func (t DeclineType) String() string {
	switch t {
	case AlreadyCurrent:
		// Worded for every intent that can find nothing to do — a bump
		// at the version asked for, a refresh whose sums already match.
		// "at the requested version" was the first intent's phrasing
		// leaking into shared vocabulary, and it read as nonsense the
		// moment a second intent declined this way.
		return "already in the desired state"
	case TransformedStyle:
		return "carrier style transforms its literal"
	case FetchNotDriven:
		return "version does not drive the fetch"
	case ChecksumsNotLocated:
		return "checksums could not be located for editing"
	case SubportsChanged:
		return "subports would appear or disappear"
	case TargetNotReached:
		return "the edit would not reach its target value"
	case UnexpectedChange:
		return "an unrelated field would change"
	case LatestUnresolved:
		return "latest could not be resolved"
	case VendoredBlock:
		return "vendored dependency block requires regeneration"
	case RevisionShapeAmbiguous:
		return "the Portfile's shape does not say where a revision line belongs"
	case PatchWontRelocate:
		return "a patch does not relocate onto the new source"
	}
	return "unknown decline"
}

// Code is the type's stable machine name: the token a document's exit
// twin carries and a script branches on when the band is too coarse.
// It is not String with the spaces taken out — String is prose and may
// be reworded, these bytes are a contract and may not.
func (t DeclineType) Code() string {
	switch t {
	case AlreadyCurrent:
		return "already-current"
	case TransformedStyle:
		return "transformed-style"
	case FetchNotDriven:
		return "fetch-not-driven"
	case ChecksumsNotLocated:
		return "checksums-not-located"
	case SubportsChanged:
		return "subports-changed"
	case TargetNotReached:
		return "target-not-reached"
	case UnexpectedChange:
		return "unexpected-change"
	case LatestUnresolved:
		return "latest-unresolved"
	case VendoredBlock:
		return "vendored-block"
	case RevisionShapeAmbiguous:
		return "revision-shape-ambiguous"
	case PatchWontRelocate:
		return "patch-wont-relocate"
	}
	return "unknown-decline"
}

// Remedy is what the user can do about the decline, in one clause.
//
// It hangs on the TYPE, so every producer of a type has to be able to
// stand behind the same sentence — which is a real constraint, not a
// formality: AlreadyCurrent is raised by a bump at the version it was
// given and by a refresh whose sums already match, so a remedy naming
// --to would be false the moment refresh raised it. Where the
// producers differ, the remedy names the SHAPE of the fix rather than
// a command, and the Detail the producer wrote says which case this
// is. The same vocabulary leak was found once already, in String; the
// note there records it.
//
// A type with nothing useful to say returns empty, and Error says
// nothing rather than padding the sentence.
func (t DeclineType) Remedy() string {
	switch t {
	case AlreadyCurrent:
		return "nothing needs doing here; ask for a different state if this is not the one you meant"
	case TransformedStyle:
		return "edit the carrier in the Portfile by hand; dockhand will not invent the transform back"
	case FetchNotDriven:
		return "find what actually drives the fetch and move that first"
	case ChecksumsNotLocated:
		return "dockhand rewrites only the checksums a Portfile writes plainly; write them there, or regenerate the block that supplies them"
	case SubportsChanged:
		return "land the subport change on its own first, then run this again"
	case TargetNotReached:
		return "`--debug` prints the carrier and the shadow evaluation; edit what actually drives the field"
	case UnexpectedChange:
		return "land the unrelated change on its own first; `--debug` names what moved"
	case LatestUnresolved:
		return "name the version with `--to`, or fix the port's livecheck so it finds what upstream publishes"
	case VendoredBlock:
		return "regenerate the vendored block and commit that first; dockhand will not edit around it"
	case RevisionShapeAmbiguous:
		return "add the `revision` line yourself; dockhand writes one only under a version line whose placement leaves nothing to guess"
	case PatchWontRelocate:
		return "refresh the patch by hand against the new source; dockhand moves a hunk only where its lines recur verbatim, and rewrites nothing inside one"
	}
	return ""
}

// Decline is a planner's refusal, stated precisely. Like portstyle's,
// it is an error so it travels normal plumbing and typed so callers
// branch on it; a decline is a first-class outcome, not a failure.
type Decline struct {
	Type   DeclineType
	Detail string
	// Withheld names what the decline held back with it: the riders a
	// sweep would have carried on the change it is not making. It moves
	// the exit code, because "nothing to do" and "nothing to do, and
	// these went undone with it" are different answers to a caller
	// deciding whether to look.
	Withheld []string
	// Determined is the producer's own statement about what decided this
	// answer, for the decline memo. It may only narrow what the type's
	// ruling already allows — see Determinacy in memoizable.go, where
	// the rule and the reason for it live together. The zero value says
	// nothing, which on a type whose producers disagree means the
	// decline is not remembered.
	Determined Determinacy
}

// Error implements the error interface. The remedy rides on the end of
// the sentence, in the shape every refusal in dockhand uses — the
// finding, then a dash, then what to do about it — because a decline
// the user cannot act on is a decline they will read as a failure.
func (d *Decline) Error() string {
	msg := fmt.Sprintf("plan: declined: %s", d.Type)
	if d.Detail != "" {
		msg += ": " + d.Detail
	}
	// What went undone rides between the finding and the remedy, because
	// it is part of the finding: a caller who reads only that the port is
	// where it was asked to be has not been told the whole answer. The
	// rules are named, not counted — "modeline" is a thing a reader can
	// look up, and "1 rider" is not.
	if len(d.Withheld) > 0 {
		msg += " (withheld: " + strings.Join(d.Withheld, ", ") + ")"
	}
	if remedy := d.Type.Remedy(); remedy != "" {
		msg += " — " + remedy
	}
	return msg
}

// Code names the decline for a machine: the twin's reason.
//
// The withheld case names itself, rather than sharing the plain
// already-current token with the code beside it. A reason that spanned
// two codes would be the coarser of the two fields, which is backwards
// — the band says which KIND of problem this is and the reason says
// WHICH — and a consumer filtering on the reason would have to read the
// code anyway to learn whether anything went undone.
func (d *Decline) Code() string {
	if d.Type == AlreadyCurrent && len(d.Withheld) > 0 {
		return "already-current-withheld"
	}
	return d.Type.Code()
}

// DockhandExit: a decline is a successful judgment, and it exits in the
// declined band rather than the failure one. A decline that withheld
// riders gets its own code inside that band — the outcome is the same,
// the consequence is not, and a caller sweeping ports needs to see the
// difference without reading the prose.
func (d *Decline) DockhandExit() int {
	if d.Type == AlreadyCurrent && len(d.Withheld) > 0 {
		return exitcode.AlreadyCurrent
	}
	return exitcode.PlanDeclined
}
