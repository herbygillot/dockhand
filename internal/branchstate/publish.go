package branchstate

import (
	"strings"

	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/refusal"
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

// Publish is the one gate of a submission, a person's, ship's, bump's,
// or serve's (R3-10, where ship's submitPassed and serve's SubmitForServe
// each held it): nothing that blocks it, the commit it was bound to, and,
// where nobody looks (Mode.Unattended), nothing that holds it, which waits
// for a person's look (refusal.Attention). A person's own submission shows
// what holds it and goes on. Nil where it goes.
func Publish(p Publication, mode Mode, v Vocabulary) *refusal.Refusal {
	next := Next{Verb: VerbSubmit, Branch: p.Branch}
	switch {
	case len(p.Blocks) > 0:
		var words []string
		for _, block := range p.Blocks {
			words = append(words, block.Words(v))
		}
		next.After = AfterEdit
		return refusal.New(refusal.Failed, "can't submit yet: %s", strings.Join(words, "; ")).Then(next)
	case p.Bound != "" && p.Commit != p.Bound:
		return refusal.New(refusal.Failed, "%s moved to %s while it was being checked. Nothing was submitted: ship was working on %s", p.Branch, p.Commit.Short(), p.Bound.Short()).Then(next)
	case mode.Unattended() && len(p.Holds) > 0:
		next.After = AfterLook
		return refusal.New(refusal.Attention, "%s needs review before submission: %s", p.Branch, strings.Join(v.Holds(p.Holds), "; ")).Then(next)
	}
	return nil
}

// SubmitsDespiteCheck reports whether a submission ship carried goes up
// after its check didn't pass: a draft does, as submit --draft allows, with
// the check's exit; anything else stops there.
func SubmitsDespiteCheck(draft bool) bool { return draft }
