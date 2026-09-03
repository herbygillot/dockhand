package verdict

// MintDecision is what a plan becomes when it is realized as a branch.
type MintDecision int

const (
	// NothingToMint is a plan with no edits: realized as a branch it
	// would be an empty commit, which is a lie about work having
	// happened.
	NothingToMint MintDecision = iota
	// NothingToReplace is a plan with no edits under --replace. The
	// standing branch, if there is one, stands: --replace replaces only
	// when there is something to replace it with, and a user who asked
	// for that and got silence would reasonably believe it happened.
	NothingToReplace
	// MintBranch mints onto the primary branch's local position.
	MintBranch
	// ReplaceThenMint demolishes the standing branch first — its running
	// verification canceled, its workers released, its notes removed —
	// and then mints. Only what dockhand placed there may go this way;
	// commits the user added are theirs, and discard remains the explicit
	// act for those.
	ReplaceThenMint
)

// DecideMint chooses between them.
//
// hasBranch is consulted only when there are edits and --replace was
// given, which is the order the effectful caller must follow too: the
// no-edits answer comes before the plan is resolved against the
// repository at all, so an empty plan never reports drift, and the
// branch probe comes after, so a drift refusal precedes a replacement.
// A caller that has not probed may ask MintProbesBranch first and pass
// false when the answer is no.
func DecideMint(hasEdits, replace, hasBranch bool) MintDecision {
	if !hasEdits {
		if replace {
			return NothingToReplace
		}
		return NothingToMint
	}
	if replace && hasBranch {
		return ReplaceThenMint
	}
	return MintBranch
}

// MintProbesBranch reports whether the has-branch question is worth
// asking. It costs a git call, and everything but a replacing mint of a
// plan that actually edits something reaches the same decision without
// it.
func MintProbesBranch(hasEdits, replace bool) bool { return hasEdits && replace }
