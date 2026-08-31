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
}

var _ plan.Planner = BumpRevision{}

// ErrNoReason reports a revbump with no stated why. The edit is
// mechanical; the reason is the part only the caller has.
var ErrNoReason = errors.New("bumprevision: a revision bump needs a reason")

// Plan produces the one-edit plan. The returned error is a
// *plan.Decline or *portstyle.Decline when the refusal is a judgment.
//
// The fetcher is unused: nothing is downloaded to count. It is in the
// signature because every intent is a plan.Planner, and a uniform shape
// is worth one ignored parameter.
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

	edits := []plan.Edit{{
		Start: loc.Span.Start, End: loc.Span.End,
		Old: loc.Span.Text(src), New: next,
		Reason: "revision +1: " + b.Reason,
	}}

	finalSrc, err := plan.ApplyEdits(src, edits)
	if err != nil {
		return nil, err
	}
	shadow, cleanup, err := h.Shadow(finalSrc)
	if err != nil {
		return nil, err
	}
	defer cleanup()
	after, err := shadow.Snapshot(ctx)
	if err != nil {
		return nil, fmt.Errorf("bumprevision: shadow evaluation: %w", err)
	}
	predicted := before.Diff(after)
	if err := accept(vals, next, predicted); err != nil {
		return nil, err
	}

	slices.SortFunc(edits, func(a, b plan.Edit) int { return a.Start - b.Start })
	return &plan.Plan{
		Format:         plan.Format,
		Intent:         "bump-revision",
		Portdir:        h.Target.Portdir,
		Subport:        h.Target.Subport,
		PortfileSHA256: plan.FileSHA256(src),
		Edits:          edits,
		Predicted:      plan.FromDelta(predicted),
	}, nil
}

// accept is the intent's judgment of its own predicted delta: the
// revision arrived at its successor in the port's own context, and
// nothing else moved anywhere. A shared revision line moving every
// subport's revision together is the expected shape, not a violation —
// each context's change must simply be the revision and only the
// revision.
func accept(vals info.Values, next string, predicted info.Delta) error {
	if len(predicted.Added) > 0 || len(predicted.Removed) > 0 {
		return &plan.Decline{Type: plan.SubportsChanged,
			Detail: fmt.Sprintf("%d added, %d removed", len(predicted.Added), len(predicted.Removed))}
	}
	var reached bool
	for key, changes := range predicted.Changed {
		for _, ch := range changes {
			if ch.Field != info.FieldRevision {
				return &plan.Decline{Type: plan.UnexpectedChange,
					Detail: fmt.Sprintf("%s: %s", key.Subport, ch.Field)}
			}
			if key.Subport == vals.Name && slices.Equal(ch.New, []string{next}) {
				reached = true
			}
		}
	}
	if !reached {
		return &plan.Decline{Type: plan.TargetNotReached,
			Detail: fmt.Sprintf("revision would not become %s", next)}
	}
	return nil
}
