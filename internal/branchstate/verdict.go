package branchstate

import "github.com/herbygillot/dockhand/internal/model"

// Facts are what's recorded of a branch, or already read of the world,
// that the verdict reads, gathered by the engine. Each step of part 4
// brings more of them, as it brings the judgment that reads them.
type Facts struct {
	Branch Branch
	Check  Check
}

// Action is what's asked of a branch.
type Action string

const (
	// ActionStatus asks what the branch needs, as status lists it.
	ActionStatus Action = "status"
)

// Mode is how the action is asked: who answers its questions, a person
// at a terminal, a --yes, or nobody (Answered), which alone decides
// whether it may go on unlooked at. Who started the branch is a fact for
// the record and its words, never a second reading of consent
// (architecture's note on R3-36).
type Mode struct {
	Answered model.Answered
}

// Unattended reports whether nobody looks over what the action does before
// it goes on: no person was there to ask, as for serve or a script, or one
// answered --yes, which skips the question and approves nothing shown
// after it (Codex's review of 832f059e, high 1). It's the one reading of
// consent, which ship's two and tidy's terminal tests each made (R3-9).
func (m Mode) Unattended() bool { return m.Answered != model.AnsweredAsked }

// Verdict is the one judgment of a branch's state for an action: its
// checks' standing, whether the action is refused, and what moves it on.
type Verdict struct {
	Standing Standing
	// Refused is whether the action can't go on as asked.
	Refused bool
	// Next is what moves the branch on; nil where nothing is needed.
	Next *Next
}

// Evaluate is the verdict on a branch's state for an action asked one way.
func Evaluate(facts Facts, action Action, mode Mode) Verdict {
	if action == ActionSubmit {
		return submitVerdict(facts)
	}
	verdict := Verdict{Standing: facts.Check.Standing()}
	if rows := Attend(facts); len(rows) > 0 {
		verdict.Next = &rows[0].Next
	}
	return verdict
}
