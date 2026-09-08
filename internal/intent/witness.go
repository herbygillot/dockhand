package intent

import "github.com/herbygillot/dockhand/internal/macports/info"

// WitnessKind is the evidence a change rests on when the evaluated
// delta cannot show what it did.
//
// It exists because the rule it serves used to be satisfied by a
// SENTENCE. Finish accepts an empty predicted delta when a witness is
// declared, and until now a witness was a free-form English string —
// "the port's distfiles were fetched from upstream and hashed" — which
// means the falsifiability rule was enforced by len(s) != 0. Any
// non-empty text passed it, nothing checked that the words described
// what the run actually spent, and nothing could: recovering a fact by
// reading words is rule 6, and a string vocabulary with no owner is
// defect D14. This is the same change edit.Kind made to edit.Reason one
// step earlier, and it is made the same way — a closed set of constants
// owned by the package that consumes them, stamped by the code that
// KNOWS what happened, with a refused zero.
//
// WHAT A WITNESS CLAIMS is narrow and worth stating exactly. It does
// not say the change was right; the intent's own Accept says that. It
// says: here is a field this change is about, and the reason the
// prediction shows nothing moving in it is that the evidence lives
// somewhere an evaluation cannot look — in bytes pulled off the
// network, or in a proof about the source text. Excuses names those
// fields, which is what makes the claim checkable: Finish requires
// every field a witness excuses to be a field the intent declared it
// may change, so a witness cannot excuse an emptiness in a field the
// change was never about.
type WitnessKind uint8

const (
	// NoWitness is the zero value and it is never permitted where a
	// witness is required. An intent that forgets to declare one is
	// therefore refused rather than admitted, which is the only safe
	// direction: the refusal is ErrNoWitness, and it is reached only by
	// an intent whose predicted delta is empty — every intent in the
	// catalogue that CAN predict nothing declares a kind below, so a
	// fourth one reaching this is a bug in that intent and not a fact
	// about anybody's port.
	NoWitness WitnessKind = iota

	// WitnessFetched is the strongest evidence in the catalogue: the
	// port's distfiles were fetched from upstream — deliberately not
	// from the mirrors — and hashed. What was hashed is what upstream
	// serves right now.
	//
	// It excuses the checksums. A refresh that fetches and finds the
	// recorded sums already correct predicts nothing, and that is not a
	// plan claiming nothing: bytes crossed the network and were
	// compared. The same is true of a bump that fetched the target
	// version's distfiles.
	WitnessFetched

	// WitnessVersionWritten is a bump that rewrote the version carrier
	// and re-evaluated the Portfile. No network was spent, but the
	// Portfile's own bytes moved and the shadow could have contradicted
	// it — which is what makes the change falsifiable even when the
	// evaluation shows nothing.
	//
	// It excuses the version. A version that does not move under it is
	// the intent's own judgment to make, and bump's accept makes it in
	// the port's words.
	WitnessVersionWritten

	// WitnessRevisionWritten is a revbump: the revision line was
	// written and the result evaluated. A revbump spends no network, so
	// the edit itself is the only evidence it can offer, and it is
	// enough to be falsifiable — when the shadow shows nothing moved,
	// the intent's accept says so, which is why an empty prediction
	// there is a decline about the port rather than a refusal to have
	// planned.
	WitnessRevisionWritten

	// WitnessRidersInert is the housekeeping change, and it is the one
	// kind that excuses NO FIELD — deliberately, and not as an
	// oversight this list forgot to fill in.
	//
	// A housekeeping change predicts nothing BY CONSTRUCTION. It is not
	// that some field moved where the evaluation cannot see it; it is
	// that nothing was ever supposed to move, and the claim being made
	// is exactly that. The evidence is the double proof: every rider's
	// bytes land in comment or whitespace spans, they are still in such
	// spans in the tree they produced, and a second shadow predicts
	// what the headline predicted alone. That is why housekeeping's
	// MayChange is nil, and why Finish pairs the two: an intent that
	// declared fields it may change and then excuses none of them has
	// not explained its own emptiness, and is refused.
	WitnessRidersInert
)

// Excuses names the predicted fields this witness accounts for: the
// fields the change is about whose movement the evaluation cannot show.
//
// It is a method on the kind and not data on the call site on purpose.
// If an intent could hand Finish both a kind and a field list, the
// field list would be as unowned as the sentence it replaced — an
// intent could claim to excuse anything. The kind IS the claim, and
// what it covers is declared here, once, in the package that checks it.
func (k WitnessKind) Excuses() []info.Field {
	switch k {
	case NoWitness:
		return nil
	case WitnessFetched:
		return []info.Field{info.FieldChecksums}
	case WitnessVersionWritten:
		return []info.Field{info.FieldVersion}
	case WitnessRevisionWritten:
		return []info.Field{info.FieldRevision}
	case WitnessRidersInert:
		return nil
	}
	return nil
}

// String is the witness in the words a debug line and an error message
// use. It is prose ABOUT a typed fact rather than the fact itself,
// which is the distinction the whole change rests on: nothing decides
// from these words.
func (k WitnessKind) String() string {
	switch k {
	case NoWitness:
		return "no witness"
	case WitnessFetched:
		return "the distfiles were fetched from upstream and hashed"
	case WitnessVersionWritten:
		return "the version carrier was rewritten and the Portfile re-evaluated"
	case WitnessRevisionWritten:
		return "the revision line was written and the Portfile re-evaluated"
	case WitnessRidersInert:
		return "the rider edits were proved inert: comment and whitespace spans only, and a shadow that predicted nothing"
	}
	return "unknown witness"
}
