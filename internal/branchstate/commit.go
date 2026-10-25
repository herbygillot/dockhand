package branchstate

import (
	"errors"
	"fmt"
	"strings"

	"github.com/herbygillot/dockhand/internal/model"
)

// Plan is what's proposed of a branch's commits, as the verdict on
// committing it reads it: whether its history is kept as it is, and each
// commit proposed.
type Plan struct {
	Branch string
	Keep   bool
	Groups []Group
}

// Group is one commit proposed: its port directory, whether its files and
// subject are exactly dockhand's authoring commands', or a new port create
// wrote, why it needs a look, and what must be settled before it applies.
type Group struct {
	Directory          string
	FromEdits, Created bool
	Notes, Blocking    []string
}

// Unambiguous reports whether the plan may be applied without review:
// every commit is one port directory's edits, exactly as dockhand's
// authoring commands wrote them, under the subject they wrote.
func (p Plan) Unambiguous() bool {
	if p.Keep || len(p.Groups) == 0 {
		return false
	}
	for _, g := range p.Groups {
		if !g.FromEdits && !g.Created || g.Directory == "" || len(g.Notes) > 0 || len(g.Blocking) > 0 {
			return false
		}
	}
	return true
}

// Blocking lists what must be settled before the plan can be applied.
func (p Plan) Blocking() []string {
	var all []string
	for _, g := range p.Groups {
		all = append(all, g.Blocking...)
	}
	return all
}

// AppliesUnasked says whether the plan may be applied with nobody
// reviewing it on a terminal, and why not: one made only of dockhand's own
// edits (Unambiguous) where nobody was there to ask, any the person
// answered for with --yes or by giving the plan itself (explicit), and
// none while something blocks it. It's ship's "commit if unambiguous" and
// tidy's alike (the command package split's 7b row 6).
func (p Plan) AppliesUnasked(mode Mode, explicit bool) error {
	if !explicit && mode.Answered != model.AnsweredYes && !p.Unambiguous() {
		return errors.New("review this plan in an interactive terminal before applying it, or approve the displayed plan with --yes. To combine the changes into one commit, use --squash --message \"port: what changed\"")
	}
	if blocking := p.Blocking(); len(blocking) > 0 {
		return fmt.Errorf("nothing was applied: %s", strings.Join(blocking, "; "))
	}
	return nil
}

// CommitUnasked is whether a plan may be committed on the way to a check
// or a submission, ship's and serve's, and why not: one that isn't
// dockhand's own edits needs a look on a terminal, the way on tidy, and
// --yes approves no plan it hasn't shown, so where nobody looks only
// dockhand's own edits are committed unasked (Codex's review of 832f059e,
// high 1). Where a person was asked, they answered for it.
func (p Plan) CommitUnasked(mode Mode) error {
	if mode.Unattended() {
		mode.Answered = model.AnsweredNobody
	}
	if p.AppliesUnasked(mode, false) != nil {
		// A hand edit's reason is said with the way on (Then's item 3).
		var hand []string
		for _, g := range p.Groups {
			for _, note := range g.Notes {
				if strings.HasPrefix(note, "hand edit (") {
					hand = append(hand, g.Directory+": "+note)
				}
			}
		}
		if len(hand) > 0 {
			return fmt.Errorf("review the commit plan before committing: run dockhand tidy --branch %s; %s", p.Branch, strings.Join(hand, "; "))
		}
		return fmt.Errorf("review the commit plan before committing: run dockhand tidy --branch %s", p.Branch)
	}
	return nil
}
