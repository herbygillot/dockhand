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
	case check.Standing() == CheckPassed && len(b.Edited) == 0 && check.Evidence != nil:
		return SubmitReady
	}
	return NotReady
}

// ActionSubmit asks to submit the branch: push it, and open or update its
// pull request.
const ActionSubmit Action = "submit"

// submitVerdict is the verdict on submitting: refused where the branch
// isn't ready, with what moves it on, the check's standing first.
func submitVerdict(facts Facts) Verdict {
	verdict := Verdict{Standing: facts.Check.Standing()}
	if SubmitReadiness(facts) == SubmitReady {
		return verdict
	}
	b, check := facts.Branch, facts.Check
	name := b.Record.ShortName()
	verdict.Refused = true
	next := func(n Next) *Next { n.Branch = name; return &n }
	switch {
	case b.Missing:
		verdict.Next = next(Next{Verb: VerbAdopt, Args: []string{"<new name>"}})
	case b.Pushed:
		verdict.Next = next(Next{Verb: VerbStatus})
	case b.Commits == 0 || len(b.Edited) > 0:
		verdict.Next = next(Next{Verb: VerbTidy})
	case verdict.Standing == Checking:
		verdict.Next = next(Next{Verb: VerbWait, Args: []string{check.Checking().Name()}, After: AfterCheck})
	case verdict.Standing == CheckStopped:
		verdict.Next = next(Next{Verb: VerbWait, Args: []string{check.Stopped.Name()}})
	case verdict.Standing == CheckFailed || verdict.Standing == CheckAttention:
		verdict.Next = next(Next{Verb: VerbLogs, Args: []string{check.Latest.Name()}})
	default:
		verdict.Next = next(Next{Verb: VerbCheck})
	}
	return verdict
}
