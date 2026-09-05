package plan

// This file holds one rule: whether a decline may be remembered.
//
// The rule is here, on the taxonomy, rather than at the store that
// keeps the memos, because a list of memoizable kinds kept beside the
// store is a list somebody has to remember to extend. Determinacy is an
// exhaustive switch over DeclineType, so an eleventh kind does not
// compile past the linter until it has said what its answer depends on
// — and the fallthrough refuses, so the one way to get a decline
// remembered is to have ruled on it.

// Determinacy says what a decline's answer depends on, which is the
// whole of the memo's safety.
//
// A decline is a judgment, and a judgment may be replayed from a store
// keyed by its inputs only if the store's key is the WHOLE of its
// inputs. The memo's key is the Portfile's bytes, the intent, the
// resolved input and a digest of the evaluation environment. A decline
// decided by any of those may be replayed; a decline decided by
// something upstream served may not, because nothing in the key moves
// when upstream moves, and a content-addressed store never expires.
//
// refresh-checksums is the case that makes this a safety rule rather
// than a performance one. Its "recorded checksums match what upstream
// serves" is an AlreadyCurrent whose cause is a fetch, and the event
// the whole verb exists to catch — an upstream re-rolling an artifact
// at an unchanged version — is precisely the event in which the
// Portfile's bytes do NOT change. A memo of that answer would suppress
// the detection permanently.
type Determinacy int

const (
	// Unstated is the zero value: nothing has been said. On a type whose
	// producers disagree it means the decline is not memoizable, because
	// silence is the answer that stores nothing.
	Unstated Determinacy = iota
	// ByPortfile means the answer follows from the Portfile's bytes and
	// the environment they were evaluated in — the memo's key, exactly.
	// An evaluated value counts: it is a function of the bytes and the
	// PortGroups, both of which the key covers.
	ByPortfile
	// ByNetwork means something outside the key decided it: what a forge
	// published, what a server served, what a fetch returned. Never
	// memoizable, at any ruling, from any producer.
	ByNetwork
)

// String names the determinacy for a message.
func (d Determinacy) String() string {
	switch d {
	case Unstated:
		return "unstated"
	case ByPortfile:
		return "portfile-determined"
	case ByNetwork:
		return "network-determined"
	}
	return "unknown determinacy"
}

// Determinacy is the TYPE's ruling: what every producer of this kind of
// decline has in common. It is the ceiling, and a producer may only
// narrow it — a producer that claims more than its type allows is
// ignored, so no call site can talk its way past a ruling made here.
//
// Three kinds are Unstated because their producers genuinely disagree,
// and the disagreement is the point:
//
//   - AlreadyCurrent is raised four times in three classes. bump's
//     "the carrier already reads this version" is the Portfile alone;
//     bump's "records no checksums, so a re-derivation has nothing to
//     fetch" is the evaluation; housekeeping's "every rule this build
//     knows already holds" is this binary; and refresh's "recorded
//     checksums match what upstream serves" is the network. One ruling
//     for the type would either memoize the last of those or refuse the
//     first, and there is no third answer.
//   - FetchNotDriven is raised by bump from a shadow prediction, and by
//     refresh from a comparison that is only reached once a fetch has
//     already shown a difference. The second is a judgment about the
//     Portfile standing downstream of a network branch, and reading the
//     ordering right is the producer's job, not the taxonomy's.
//   - VendoredBlock is decided by three things the key does not hold,
//     and anyone ruling on it later needs all three rather than the
//     first. Cargo's veto reads the patch files in the portdir's files/
//     directory, so a maintainer who rewrites the offending patch has
//     fixed the port while the Portfile's bytes stand still — and the
//     memo's promise that a changed Portfile re-arms it would be false.
//     A family's regeneration ALSO raises this kind, out of an archive
//     it fetched from a forge and extracted with whatever tools this
//     machine has: what a server served is not in the key, and neither
//     is the toolchain — engine.MemoParams omits Params.Tools
//     deliberately and ledger.Env digests the PortGroups, the base, the
//     prefix, the platform and the shim, not what is installed. Closing
//     the files/ gap alone would leave two.
//
// A value outside the taxonomy answers ByNetwork. Not because it is,
// but because that is the answer that stores nothing, and a kind nobody
// has ruled on must not be stored.
func (t DeclineType) Determinacy() Determinacy {
	switch t {
	case TransformedStyle, ChecksumsNotLocated, SubportsChanged,
		TargetNotReached, UnexpectedChange, RevisionShapeAmbiguous:
		// Every producer of these reads the Portfile, the parse tree, or a
		// shadow evaluation of them. None reaches the network.
		return ByPortfile
	case LatestUnresolved:
		// The decline IS upstream resolution failing. It is also
		// unreachable from the memo by construction: the memo is consulted
		// after resolution, so a run that raises this never had a resolved
		// input to key on.
		return ByNetwork
	case PatchWontRelocate:
		// Decided by two things the key does not hold, and the same two
		// that keep VendoredBlock unstated: the patch's own bytes under
		// the portdir's files/ directory, which a maintainer rewrites
		// without moving the Portfile, and the source the hunks are
		// looked for in, which is whatever a server served for the new
		// version. Every producer relocates against a fetch — there is
		// no other way to ask the question — so unlike VendoredBlock
		// there is no producer that could honestly say ByPortfile, and
		// the ruling is the ceiling rather than a deferral.
		return ByNetwork
	case AlreadyCurrent, FetchNotDriven, VendoredBlock:
		return Unstated
	}
	return ByNetwork
}

// Memoizable reports whether this decline may be written to the memo.
//
// Two gates, and both must pass. The type's ruling is the ceiling, and
// the producer's own statement may only narrow it: on a ByPortfile type
// a producer that knows better says ByNetwork and is believed; on an
// Unstated type nothing is stored until a producer says ByPortfile; on
// a ByNetwork type nothing is stored, ever.
//
// A decline that withheld riders is refused whatever it is otherwise.
// What a sweep held back is a function of the run's rider policy and
// not of the Portfile, and the memo's key does not name the policy — so
// a memo of it would answer a --no-riders run with a riders run's
// sentence. The rule lives here rather than at the store because it is
// a fact about this decline, and re-deriving is cheap.
func (d *Decline) Memoizable() bool {
	if len(d.Withheld) > 0 {
		return false
	}
	switch d.Type.Determinacy() {
	case ByNetwork:
		return false
	case ByPortfile:
		return d.Determined != ByNetwork
	case Unstated:
		return d.Determined == ByPortfile
	}
	return false
}

// DeclineTypeFor maps a stable code back to its type — the reverse of
// Code, for the one reader that has only the token: a memo replaying a
// decline it wrote in an earlier run.
//
// It is a switch and not a loop over the taxonomy on purpose. A type
// added without an entry here fails the round-trip test rather than
// quietly becoming a memo nothing can read back.
func DeclineTypeFor(code string) (DeclineType, bool) {
	switch code {
	case "already-current":
		return AlreadyCurrent, true
	case "transformed-style":
		return TransformedStyle, true
	case "fetch-not-driven":
		return FetchNotDriven, true
	case "checksums-not-located":
		return ChecksumsNotLocated, true
	case "subports-changed":
		return SubportsChanged, true
	case "target-not-reached":
		return TargetNotReached, true
	case "unexpected-change":
		return UnexpectedChange, true
	case "latest-unresolved":
		return LatestUnresolved, true
	case "vendored-block":
		return VendoredBlock, true
	case "revision-shape-ambiguous":
		return RevisionShapeAmbiguous, true
	case "patch-wont-relocate":
		return PatchWontRelocate, true
	}
	return 0, false
}
