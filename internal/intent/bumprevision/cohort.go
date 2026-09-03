package bumprevision

import (
	"context"

	"github.com/herbygillot/dockhand/internal/edit"
	"github.com/herbygillot/dockhand/internal/intent"
	"github.com/herbygillot/dockhand/internal/macports/info"
	"github.com/herbygillot/dockhand/internal/macports/port"
	"github.com/herbygillot/dockhand/internal/plan"
	"github.com/herbygillot/dockhand/internal/tcl/syntax"
)

// The plural mode: one revision bump per member of a cohort, planned
// from the bytes the branch tip holds.
//
// It is the same intent and deliberately not a second one. A cohort
// member's edit is exactly the edit a solo revbump makes — the counter
// gets its successor, or a revision line is written under the version
// carrier — and every refusal is the same refusal: a Portfile whose
// shape does not say where an inserted line belongs declines here for
// the reason it declines there. What is different is only where the
// bytes come from and what happens to the members that decline.
//
// The unit is the PORTDIR and not the port. A real tree collapses
// judy's eight dependents into two directories — netdata, and the seven
// php*-Judy subports that share php/php-Judy — and one revision line in
// that directory moves all seven together. Planning per port would
// produce seven plans for one file, six of which would be refused as
// drift by the first one that landed.

// Cohort plans one member of an accepted proposal.
//
// It carries the proposal's own words rather than composing new ones:
// Reason is the criterion verbatim, which travels into the edit and
// from there into the plan a person reads before the verb acts. The
// commit's own message is render's, and it restates the same sentence —
// one claim, said the same way in the edit, the commit and the pull
// request.
type Cohort struct {
	// Headline is the port whose ABI moved, and Target what this change
	// moved it to. Both are for the member's own one-line summary: the
	// cohort is one commit and its subject is written elsewhere, so what
	// a member's summary owes a reader is which change it is riding.
	Headline string
	Target   string
	// Reason is the criterion, verbatim from the measurement.
	Reason string
}

var _ intent.CohortPlanner = Cohort{}

// PlanMember plans one member's revision bump over the source the
// caller read from the branch tip.
//
// The source is a parameter and not something read from the handle,
// and that is the whole point of this entry point. The working tree is
// sacred and may differ from the branch in ways that have nothing to do
// with this change — a half-finished edit, another branch checked out —
// so a cohort planned from it would compute its edits against bytes the
// commit will never carry, and the precondition hash the plan pledges
// would be a hash of the wrong file. What the branch tip says is what
// the extend commit is built on.
//
// The state the change is measured against is the tip's too. The
// snapshot is taken over a shadow of the tip's bytes rather than over
// the portdir on disk: a before read from the working tree would make
// the predicted delta a diff between two different files, and every
// guard in the tail is a comparison against it.
//
// Riders never ride here, and the omission is deliberate rather than an
// oversight. A cohort commit revbumps other people's ports because a
// library they link moved; inserting a modeline into eight Portfiles on
// the way past would be housekeeping nobody asked for, in a commit
// whose whole claim is that it made one mechanical change for one
// stated reason.
func (c Cohort) PlanMember(ctx context.Context, h port.Handle, src []byte, portdir string) (*plan.Plan, error) {
	if c.Reason == "" {
		return nil, ErrNoReason
	}
	cst, errs := syntax.Parse(src)
	if len(errs) != 0 {
		return nil, &port.ParseError{Path: portdir, Detail: errs[0].Describe(src)}
	}
	tip, cleanup, err := h.Shadow(src)
	if err != nil {
		return nil, err
	}
	defer cleanup()
	before, err := tip.Snapshot(ctx)
	if err != nil {
		return nil, err
	}
	vals, err := tip.Values(ctx)
	if err != nil {
		return nil, err
	}
	next, e, err := revisionEdit(src, cst, before, vals, c.Reason)
	if err != nil {
		return nil, err
	}
	return intent.Finish(ctx, tip, src, []edit.Edit{e},
		intent.Identity{
			Intent: "bump-revision",
			Slug:   vals.Name + "-rev" + next,
			// The member's own line, not the commit's subject. A cohort is
			// one commit and render writes its message; what this says is
			// which change this member is riding, which is what a person
			// reading the plan summary before the verb acts needs to know.
			Summary: vals.Name + ": " + ridingFor(c.Headline, c.Target),
		},
		intent.FinishOpts{
			Before:    before,
			Vals:      vals,
			CST:       cst,
			MayChange: revisionMayChange,
			Accept: func(predicted info.Delta) error {
				return accept(vals, next, predicted)
			},
			// The plan names the directory the change lands in, not the
			// copy of it this was computed over.
			Portdir: portdir,
			Riders:  intent.RidersNone,
			Witness: "the revision line was written and the Portfile re-evaluated",
		})
}

// ridingFor says which change a member is riding, in one clause.
func ridingFor(headline, target string) string {
	what := "revbump for " + headline
	if target != "" {
		what += " " + target
	}
	return what
}
