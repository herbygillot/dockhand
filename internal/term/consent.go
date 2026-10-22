package term

import (
	"fmt"
	"strings"

	"github.com/herbygillot/dockhand/internal/model"
)

// Decision is a yes-or-no question a command asks before it acts: what it
// asks on a terminal, its answer when the person just presses return, and
// what was left undone without one, "nothing was removed", with what --yes
// would do, "--yes removes these".
type Decision struct {
	Question string
	Yes      bool
	Undone   string
	ByYes    string
	// Nobody is the answer where nobody is there to ask and none refuses,
	// as for a question with a safe way on, stopping a running check
	// that a script asked to replace; nil refuses.
	Nobody *bool
}

// Answer is an answer for Decision.Nobody.
func Answer(yes bool) *bool { return &yes }

// Decide settles a decision: --yes takes it, a terminal asks it, and
// otherwise it's refused in one sentence, "nothing was removed: without a
// terminal, --yes removes these", exit 1. Seven commands each wrote the
// refusal their own way, two of them exiting 0 (clean's and review --pr's).
// It says how it was answered; a person's no is an answer, not an error.
func (s Streams) Decide(yes bool, d Decision) (bool, model.Answered, error) {
	if yes {
		return true, model.AnsweredYes, nil
	}
	if !s.Terminal() {
		if d.Nobody != nil {
			return *d.Nobody, model.AnsweredNobody, nil
		}
		return false, model.AnsweredNobody, &ExitError{Code: ExitFailure, Message: fmt.Sprintf("%s: without a terminal, %s", d.Undone, d.ByYes)}
	}
	ok, err := s.AskYes(d.Question, d.Yes)
	return ok, model.AnsweredAsked, err
}

// AskYes asks a yes-or-no question on the terminal, with the answer an
// empty line gives: Decide's asking half, for a question whose command
// has made sure a person is there, as setup's offers and submit's tested
// checkboxes do.
func (s Streams) AskYes(question string, empty bool) (bool, error) {
	answer, err := s.Ask(question)
	if err != nil {
		return false, err
	}
	if answer == "" {
		return empty, nil
	}
	return strings.EqualFold(answer, "y") || strings.EqualFold(answer, "yes"), nil
}
