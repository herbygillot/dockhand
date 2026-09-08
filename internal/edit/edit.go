// Package edit holds the byte-span edit and, now, WHAT an edit is. It is
// a leaf: it imports nothing of dockhand's, so the durable record, the
// planners and the analyses can all speak its vocabulary and none of
// them owns it. That is exactly the role artifact already plays for
// manifests and probes, and it is why the kind does not live in plan —
// record may not import a planning package, and two vocabularies one
// import apart is the weak identity this whole design is about.
//
// The single import below is internal/text, which Apply reaches for to
// splice spans. text is the primitive this package sits directly on and
// not a layer that could own the vocabulary, so the dependency arrow
// still points from workflow down to primitive and never sideways.
package edit

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"github.com/herbygillot/dockhand/internal/text"
)

// Kind is what a byte-span edit IS.
//
// It exists because the machine-publication ruling asks a question the
// current shapes cannot answer. "Simple" means every edit falls in the
// version, checksum or vendored region — and today the only provenance
// an edit carries is Reason, a free-form sentence: "version", "revision
// reset", "checksum " + r.Type, "regenerate " + k.String(), "modeline",
// "go.toolchain_min", and — fatally — "revision +1: " + reason, where
// reason is text the USER typed (internal/intent/bumprevision/
// bumprevision.go:153). Classifying an edit would mean prefix-matching a
// string a caller controls, which is rule 6 ("nothing recovers a fact by
// reading words") and defect D14 (a string vocabulary with no owner).
// Position cannot stand in for it either: intent/finish.go:291 sorts the
// set by offset, so construction order is gone by the time anyone looks.
//
// The kind is stamped by the function that CORROBORATED the span, so the
// label is asserted by the strongest witness available rather than
// bolted on afterwards.
type Kind uint8

const (
	// Unclassified is the zero value and it is never permitted. A
	// future edit-producing package that forgets to stamp therefore
	// NARROWS what a machine may do, which is the only safe direction.
	Unclassified Kind = iota
	Version
	RevisionReset // revision -> 0, riding a version change
	// EpochBump rides a version that goes BACKWARDS. MacPorts orders
	// installs by (epoch, version, revision) with epoch dominant, so a
	// port whose version decreases without an epoch increment is one
	// nobody upgrades to: vercmp reads the new version as older and the
	// upgrade never happens. The increment is therefore a CONSEQUENCE of
	// the downgrade and not a judgment about it — exactly the relationship
	// RevisionReset has to a version bump — so the planner emits it rather
	// than asking anyone.
	//
	// WHERE IT GOES when the port has no epoch line at all, which is the
	// common case — only 477 of 20,069 Portfiles carry one. Ruled: insert
	// it AFTER revision when a revision line is present, else after
	// version. That is the tree's plurality convention (of the 477, the
	// command immediately above an epoch line is revision 142 times and
	// version 47 — 40% together) and it keeps the three lines that
	// constitute a port's version identity contiguous, which matters
	// because a downgrade resets the revision in the same breath.
	//
	// The tree does carry a second convention — epoch in the header, above
	// version, after name or PortGroup — and dockhand picks one rather
	// than trying to infer which file it is looking at.
	//
	// THE GUARD IS STRUCTURAL, NOT EVALUATED. Insert only when no epoch
	// COMMAND exists anywhere in the descended scope. Guarding on
	// vals.Epoch == "0" is wrong and there is a port that proves it:
	// python/py-openssl sets `epoch 1` inside an if-branch whose else has
	// none, so on a host taking the else the evaluated epoch is "0" while
	// the command plainly exists, and a value-guarded insertion would
	// write a second epoch line beside the first.
	//
	// EPOCH IS PER-CONTEXT, like version and revision: devel/cmake-devel
	// carries three, one top-level and one in each of two subport blocks.
	// The insertion belongs in the context being changed, not at the top.
	EpochBump
	RevisionBump // revision +1 on its own; its justification is a typed sentence
	Checksum
	// ChecksumSet is the set-carrier path, and it is a SEPARATE kind
	// because it is not the same act. rewrite.Edits falls back to placing
	// checksums in arbitrary top-level `set` commands by an aliasing
	// heuristic, and the code itself says the caller then owes a proof
	// that no sibling context moved.
	ChecksumSet
	// VendoredBlock replaces a span vendored.Locate corroborated: one
	// named Tcl command, refused on zero or multiple matches.
	VendoredBlock
	// VendoredNew is a ZERO-WIDTH INSERT that introduces a vendored
	// block the Portfile did not have — cargo's "add cargo.crates_github"
	// (internal/vendored/cargo/gitcrates.go:149). It is not the same act
	// as regenerating one: there is no located span to contain it, and
	// what it adds is a new FETCH SOURCE. The span-containment argument
	// does not reach it.
	VendoredNew
	DistfileName
	ToolchainMin // which compilers the port demands: a buildability claim
	Rider        // housekeeping, proved inert but never proved right
)

// Edit is one byte-span replacement. Reason survives as the sentence a
// person reads; nothing decides from it any more.
type Edit struct {
	Kind   Kind   `json:"kind"`
	Start  int    `json:"start"`
	End    int    `json:"end"`
	Old    string `json:"old"`
	New    string `json:"new"`
	Reason string `json:"reason"`
}

// Apply returns src with the edits applied, verifying each edit's
// recorded Old against the bytes it claims to replace. Every path that
// realizes edits comes through here — a planner shadowing its own edit
// set as much as a plan being carried out — so an edit whose Old
// disagrees with its span cannot survive planning: it fails in the
// planner's shadow, as the planner's bug, instead of surfacing at
// realization time as a false drift blamed on the user's tree.
func Apply(src []byte, edits []Edit) ([]byte, error) {
	tedits := make([]text.Edit, 0, len(edits))
	for _, e := range edits {
		span := text.Span{Start: e.Start, End: e.End}
		if e.End > len(src) || e.Start < 0 || span.Text(src) != e.Old {
			return nil, fmt.Errorf("edit at %d..%d: recorded old text does not match the source", e.Start, e.End)
		}
		tedits = append(tedits, text.Edit{Span: span, New: []byte(e.New)})
	}
	return text.Apply(src, tedits)
}

// FileSHA256 is the precondition hash: the hex sha256 of the file
// bytes an edit set was computed against.
func FileSHA256(src []byte) string {
	sum := sha256.Sum256(src)
	return hex.EncodeToString(sum[:])
}
