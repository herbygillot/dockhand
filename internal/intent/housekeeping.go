package intent

import (
	"context"
	"strings"

	"github.com/herbygillot/dockhand/internal/distfile"
	"github.com/herbygillot/dockhand/internal/macports/port"
	"github.com/herbygillot/dockhand/internal/plan"
)

// Housekeeping is the planner --riders selects: the riders are the whole
// change, and the verb the user typed only chose the port.
//
// It is a planner rather than a branch inside each intent because there
// is nothing intent-specific left once the headline is dropped. A bump
// asked for housekeeping and a refresh asked for housekeeping are the
// same change, and three copies of that would be three chances for them
// to stop being the same.
//
// The three intents keep their own already-current declines for the
// default policy, where the riders are named as withheld rather than
// planned. This is only the road for a caller who asked for the
// housekeeping by itself.
type Housekeeping struct{}

var _ Planner = Housekeeping{}

// Plan reads the Portfile, offers it to the rules, and finishes with no
// headline edits at all.
//
// The fetcher is unused: housekeeping downloads nothing. It is in the
// signature because every intent is a Planner.
func (Housekeeping) Plan(ctx context.Context, h port.Handle, _ distfile.Fetcher) (*plan.Plan, error) {
	src, cst, err := h.Source()
	if err != nil {
		return nil, err
	}
	before, err := h.Snapshot(ctx)
	if err != nil {
		return nil, err
	}
	vals, err := h.Values(ctx)
	if err != nil {
		return nil, err
	}
	riders, dropped := Sweep(src, cst)
	if len(riders) == 0 {
		// Nothing withheld: the caller asked for the housekeeping and
		// there is none, which is a different answer from a change that
		// left some behind. The plain already-current code says so.
		//
		// A rule that HAD an edit and lost it to the first proof is a
		// third answer again, and the run that asked for the housekeeping
		// by name is the one place it is visible at all. Saying "every
		// rule already holds" over a suppressed rule states the port is
		// clean when what happened is that dockhand's own rule misfired.
		detail := vals.Name + " needs no housekeeping: every rule this build knows already holds"
		if len(dropped) > 0 {
			detail = vals.Name + " has no housekeeping to carry: " +
				strings.Join(ruleNames(dropped), ", ") +
				" had an edit and it was dropped for touching evaluated bytes, which is a bug in the rule"
		}
		// The rules this build carries, run against these bytes. The
		// build is not in the memo's key and does not have to be: the
		// memo format is bumped by hand whenever a rule that produces or
		// suppresses a decline changes, which is what ledger.MemoFormat
		// is for.
		return nil, &plan.Decline{Type: plan.AlreadyCurrent, Detail: detail,
			Determined: plan.ByPortfile}
	}
	return Finish(ctx, h, src, nil,
		Identity{
			Intent:  "housekeeping",
			Slug:    vals.Name + "-housekeeping",
			Summary: vals.Name + ": " + Phrase(riders),
		},
		FinishOpts{
			Before: before,
			Vals:   vals,
			CST:    cst,
			Riders: RidersOnly,
			// MayChange stays nil: nothing may move. A rider that moved
			// anything is not a rider, and this is the third place that is
			// enforced — after the token spans and after the rider shadow —
			// in the vocabulary every intent shares.
			//
			// The witness is the double proof itself. A housekeeping change
			// predicts nothing by construction, which is exactly the shape
			// the witness rule exists for: the evidence is real, the
			// evaluation just cannot show it.
			Witness: "the rider edits were proved inert: comment and whitespace spans only, and a shadow that predicted nothing",
		})
}
