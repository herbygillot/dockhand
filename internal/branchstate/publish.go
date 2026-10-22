package branchstate

import (
	"fmt"
	"strings"

	"github.com/herbygillot/dockhand/internal/model"
)

// Publication is a submission about to go: what stops it, what would hold
// it for a person's look, and the commit it pushes beside the one a
// caller bound it to, as ship binds the commit its check was of.
type Publication struct {
	Branch string
	Blocks []Block
	Holds  []model.Concern
	// Commit is what the submission pushes; Bound what the caller bound
	// it to, empty where none.
	Commit, Bound model.ObjectID
}

// PublishKind is why a submission doesn't go.
type PublishKind string

const (
	// PublishBlocked is a submission something stops (Block).
	PublishBlocked PublishKind = "blocked"
	// PublishMoved is a branch that moved from the commit it was bound to.
	PublishMoved PublishKind = "moved"
	// PublishHeld is a submission nobody looks over that something holds
	// for a person's look.
	PublishHeld PublishKind = "held"
)

// Refusal is why a submission doesn't go: its kind, its words, and what
// moves it on.
type Refusal struct {
	Kind  PublishKind
	Words string
	Next  Next
	// Holds are what holds it, for PublishHeld.
	Holds []model.Concern
}

func (r *Refusal) Error() string { return r.Words }

// Publish is the one gate of a submission, a person's, ship's, bump's,
// or serve's (R3-10, where ship's submitPassed and serve's SubmitForServe
// each held it): nothing that blocks it, the commit it was bound to, and,
// where nobody looks (Mode.Unattended), nothing that holds it. A person's
// own submission shows what holds it and goes on. Nil where it goes.
func Publish(p Publication, mode Mode, v Vocabulary) *Refusal {
	next := Next{Verb: VerbSubmit, Branch: p.Branch}
	switch {
	case len(p.Blocks) > 0:
		var words []string
		for _, block := range p.Blocks {
			words = append(words, block.Words(v))
		}
		next.After = AfterEdit
		return &Refusal{Kind: PublishBlocked, Words: "can't submit yet: " + strings.Join(words, "; "), Next: next}
	case p.Bound != "" && p.Commit != p.Bound:
		return &Refusal{Kind: PublishMoved, Words: fmt.Sprintf("%s moved to %s while it was checked; nothing was submitted, since ship binds %s", p.Branch, p.Commit.Short(), p.Bound.Short()), Next: next}
	case mode.Unattended() && len(p.Holds) > 0:
		next.After = AfterLook
		return &Refusal{Kind: PublishHeld, Words: p.Branch + " is held for a look: " + strings.Join(v.Holds(p.Holds), "; "), Next: next, Holds: p.Holds}
	}
	return nil
}

// SubmitsDespiteCheck reports whether a submission ship carried goes up
// after its check didn't pass: a draft does, as submit --draft allows, with
// the check's exit; anything else stops there.
func SubmitsDespiteCheck(draft bool) bool { return draft }
