// Package bumprevision plans revision increments: the counter that
// tells installed ports to rebuild when their bytes would change
// without their version moving. The edit is the most trivial in the
// catalogue — one integer becomes its successor — and the decision is
// not: whether a change warrants rebuilding every user's installed copy
// is judgment, which is why the intent demands a reason. A bare revbump
// is meaningless in review, and review is where this edit is headed —
// it is the cascade unit promote's commit plan groups ("revbump N
// dependents" is this intent, N times).
package bumprevision

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"

	"github.com/herbygillot/dockhand/internal/distfile"
	"github.com/herbygillot/dockhand/internal/edit"
	"github.com/herbygillot/dockhand/internal/intent"
	"github.com/herbygillot/dockhand/internal/macports/info"
	"github.com/herbygillot/dockhand/internal/macports/port"
	"github.com/herbygillot/dockhand/internal/macports/portstyle"
	"github.com/herbygillot/dockhand/internal/plan"
)

// BumpRevision is the intent to increment a port's revision.
type BumpRevision struct {
	// Reason is why users must rebuild. It travels in the edit, so the
	// plan — and eventually the commit — carries the judgment that
	// produced it.
	Reason string
	// ClosesTicket is the Trac ticket this revision bump closes, bound
	// for the minted commit's trailer.
	ClosesTicket string
}

var _ intent.Planner = BumpRevision{}

// ErrNoReason reports a revbump with no stated why. The edit is
// mechanical; the reason is the part only the caller has.
var ErrNoReason = errors.New("bumprevision: a revision bump needs a reason")

// Plan produces the one-edit plan. The returned error is a
// *plan.Decline or *portstyle.Decline when the refusal is a judgment.
//
// The fetcher is unused: nothing is downloaded to count. It is in the
// signature because every intent is an intent.Planner, and a uniform
// shape is worth one ignored parameter.
func (b BumpRevision) Plan(ctx context.Context, h port.Handle, _ distfile.Fetcher) (*plan.Plan, error) {
	if b.Reason == "" {
		return nil, ErrNoReason
	}
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

	// A port with no revision line is at revision 0 implicitly, and
	// giving it one means inserting a line — which needs a placement
	// convention the tree does not have (18% of Portfiles carry no
	// revision line, and where one goes is inconsistent). Locate's
	// decline says exactly this; it is propagated rather than
	// translated, because "no revision line found" is the truth.
	loc, err := portstyle.Locate(src, cst, vals, info.FieldRevision)
	if err != nil {
		return nil, err
	}
	n, err := strconv.Atoi(loc.Value)
	if err != nil {
		return nil, &plan.Decline{Type: plan.TransformedStyle,
			Detail: fmt.Sprintf("revision %q is not a plain integer", loc.Value)}
	}
	next := strconv.Itoa(n + 1)

	// The reason travels twice, and both journeys are the plan's: into
	// the edit's own Reason, which is what a reader of the plan sees
	// beside the rewritten line, and into the Summary, which becomes the
	// commit's subject. The Identity carries no third copy — a field
	// nothing reads would promise a commit body that no realizer writes.
	edits := []edit.Edit{{
		Start: loc.Span.Start, End: loc.Span.End,
		Old: loc.Span.Text(src), New: next,
		Reason: "revision +1: " + b.Reason,
	}}

	// The tail is intent.Finish, as it is for every intent. What is left
	// here is the arithmetic and the one judgment: that the integer this
	// plan names is the one the Portfile will actually evaluate to.
	return intent.Finish(ctx, h, src, edits,
		intent.Identity{
			Intent:       "bump-revision",
			Slug:         vals.Name + "-rev" + next,
			Summary:      vals.Name + ": " + b.Reason,
			ClosesTicket: b.ClosesTicket,
		},
		intent.FinishOpts{
			Before:    before,
			Vals:      vals,
			CST:       cst,
			MayChange: revisionMayChange,
			Accept: func(predicted info.Delta) error {
				return accept(vals, next, predicted)
			},
			// A revbump spends no network, so the only evidence it can
			// offer is the edit itself: the revision line was rewritten and
			// the result evaluated. That is enough to be falsifiable — when
			// the shadow shows nothing moved, accept below says so — and it
			// is why an empty prediction here is a decline about the port
			// rather than a refusal to have planned.
			Witness: "the revision line was rewritten and the Portfile re-evaluated",
		})
}

// revisionMayChange is the whole of what a revision bump may move. It
// was once written into accept's own loop as a not-equal test; as a set
// it is the same rule said in the vocabulary every intent shares.
var revisionMayChange = map[info.Field]bool{info.FieldRevision: true}

// accept is the intent's judgment of its own predicted delta: the
// revision arrived at its successor in the port's own context. That
// nothing else moved anywhere is Finish's question now, asked of every
// intent against its own permitted set.
//
// A shared revision line moving every subport's revision together is
// the expected shape, not a violation — the port's own context need
// only be among them.
func accept(vals info.Values, next string, predicted info.Delta) error {
	for _, ch := range intent.OwnChanges(predicted, vals.Name) {
		if ch.Field == info.FieldRevision && slices.Equal(ch.New, []string{next}) {
			return nil
		}
	}
	return &plan.Decline{Type: plan.TargetNotReached,
		Detail: fmt.Sprintf("revision would not become %s", next)}
}
