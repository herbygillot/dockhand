package vendored

// Decline is a family's refusal to act on a block, stated precisely.
// Like portstyle's and the planner's, it is an error so it travels
// normal plumbing and typed so callers branch on it: a refusal is a
// judgment about the port, never a failure of the run.
//
// It is this package's own type rather than the planner's because a
// family knows things nothing above it knows — what a block's format
// can express, which pins a generator can resolve, what regenerating
// would state about bytes nobody fetched — and the sentence a person
// reads has to come from there. What a refusal MEANS to a plan is the
// plan's vocabulary, and the two are joined once, in vendored/families,
// on the way to the intent that asked. That is the only reason this
// type exists: without it the packages holding a block's format would
// have to name internal/plan, which puts the whole planning layer under
// every generator. depguard holds the edge.
type Decline struct {
	// Kind names the block the refusal is about.
	Kind Kind
	// Detail is the sentence the family chose, carried through to the
	// plan's decline verbatim.
	Detail string
}

// Error implements the error interface.
func (d *Decline) Error() string {
	return "vendored: " + d.Kind.String() + ": " + d.Detail
}
