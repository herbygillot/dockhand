package branchstate

// Readiness is how a branch stands for submission, from its record and
// its checks: not one to submit at all, ready, or checked and not ready.
type Readiness int

const (
	// NotCandidate is a branch with nothing to submit by its checks: gone,
	// nothing committed, never checked, or its pull request already has
	// its head.
	NotCandidate Readiness = iota
	// SubmitReady is a branch whose committed files' checks passed, every
	// changed target built, with nothing uncommitted.
	SubmitReady
	// NotReady is a branch checked whose checks don't stand for its
	// committed files, or with files edited since.
	NotReady
)

// SubmitReadiness is the one definition of a branch that passed and is
// ready to submit, for ship --passing and for serve: it has commits and a
// finished check, its pull request doesn't already have its head, and the
// checks of exactly its committed files built every changed target and
// passed, whatever --only narrowed (PassingBranches, where the engine
// judged it).
func SubmitReadiness(facts Facts) Readiness {
	b, check := facts.Branch, facts.Check
	switch {
	case b.Missing || b.Commits == 0 || check.Latest == nil || b.Pushed:
		return NotCandidate
	case check.Publishable() && len(b.Edited) == 0:
		return SubmitReady
	}
	return NotReady
}
