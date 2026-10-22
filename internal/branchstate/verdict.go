package branchstate

// Facts are what's recorded of a branch, or already read of the world,
// that the verdict reads, gathered by the engine. Each step of part 4
// brings more of them, as it brings the judgment that reads them.
type Facts struct {
	Check Check
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
	Answered Answered
}

// Answered is who answers an action's questions.
type Answered string

const (
	// Asked is a person at a terminal, asked.
	Asked Answered = "asked"
	// Yes is a person's --yes, given ahead.
	Yes Answered = "yes"
	// Nobody is no one: a script, or serve.
	Nobody Answered = "nobody"
)

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
	return Verdict{Standing: facts.Check.Standing()}
}
