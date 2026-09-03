package intent

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"

	"github.com/herbygillot/dockhand/internal/edit"
	"github.com/herbygillot/dockhand/internal/macports/info"
	"github.com/herbygillot/dockhand/internal/macports/port"
	"github.com/herbygillot/dockhand/internal/plan"
	"github.com/herbygillot/dockhand/internal/tcl/syntax"
)

// ErrNoBefore reports a Finish called without the state the change is
// measured against. It is a mistake in the intent, not a judgment about
// the port: a snapshot is total, so an empty one never came from an
// evaluation that succeeded.
var ErrNoBefore = errors.New("intent: finish needs the snapshot taken before the edits")

// ErrNoWitness reports a change that would move nothing observable and
// named no other evidence that it proved anything.
//
// The rule exists so that a plan cannot be unfalsifiable. Every intent
// in the catalogue today justifies itself by the delta it predicts, and
// an empty delta means the plan claims nothing and can therefore be
// contradicted by nothing. An intent that can legitimately reach this
// state has two honest answers: decline first, the way a refresh whose
// recorded sums already match declines AlreadyCurrent, or declare the
// witness — the network round-trip whose bytes it hashed, the config it
// read — as the evidence the evaluation cannot show. Silently emitting
// an empty plan is not a third.
//
// Every intent in the catalogue gives one of the two, so this is a
// backstop and not a user-facing outcome: a bump asked to re-derive a
// port with nothing to re-derive declines about the port, and a revbump
// and a refresh always have their own edit or their own fetch to point
// at. Reaching it is a fourth intent's bug, which is why it stays a
// bare error in the failure band rather than a decline — a refusal that
// names dockhand's own rule is not a judgment about anybody's port.
var ErrNoWitness = errors.New("intent: the predicted delta is empty and no witness was declared")

// FinishOpts is what Finish needs beyond its arguments: three values
// every planner already holds, and the handful of places the intents
// genuinely differ.
//
// Before, Vals and CST are required. They are here rather than in the
// signature because they are the planner's own working state handed
// back, not decisions — and because re-deriving them inside Finish
// would cost a second evaluation of a portdir the plan has already
// pledged the hash of.
type FinishOpts struct {
	// Before is the snapshot taken before any edit or shadow, and the
	// state the plan's PortfileSHA256 pledges. Finish never re-takes it:
	// a second evaluation is a second Tcl round-trip, and it would read
	// a portdir that may have moved under the run.
	Before info.Snapshot
	// Vals is the evaluated state of the context being changed. Its Name
	// is the plan's Port and the context every guard is scoped to.
	Vals info.Values
	// CST is the parsed Portfile, passed to Examine.
	CST *syntax.Script

	// MayChange is the set of fields this intent is allowed to move.
	// Everything else moving is evidence the edits did more than they
	// were asked to.
	MayChange map[info.Field]bool
	// Accept is the intent's own judgment of its predicted delta: that
	// the version arrived where it was sent, that the revision reached
	// its successor, that the checksums actually moved. It is a callback
	// rather than data because the three are not variations on one rule
	// — a bump's differs by whether the carrier transforms its literal,
	// and by what the port fetches — and expressing them as a required-
	// fields set would lose exactly the distinctions their tests pin.
	//
	// It takes only the delta; everything else it needs, the intent
	// already has and closes over.
	Accept func(predicted info.Delta) error
	// ViaSet says an edit in this set landed in a set variable, so the
	// sibling isolation proof applies. Only checksum edits can be
	// located that way, so an intent that emits none passes false.
	ViaSet bool
	// Riders are the rules this intent accepts from Examine. A rule
	// Examine offers and this map does not name is not folded in — which
	// is how a rule can be written once and adopted one intent at a
	// time.
	Riders map[Rule]bool
	// Witness names the evidence a change rests on when the evaluated
	// delta cannot show it: "the distfiles were fetched and hashed",
	// say. It satisfies the empty-delta refusal and goes no further —
	// nothing about it reaches the plan, whose bytes are a hash gate.
	Witness string
	// Dependents are the ports that depend on this one, passed to
	// Examine for the cascade rules. Nothing gathers them yet.
	Dependents []string
}

// Finish is the tail every intent runs once its edits are computed:
// apply them to a copy, evaluate the copy, diff it against the state
// before, refuse anything the prediction did not promise, fold in the
// riders the intent accepts, and assemble the plan.
//
// The order of the refusals is deliberate. The witness rule goes first,
// because an empty delta satisfies every other guard vacuously and
// "nothing moved" is a better answer than whichever guard happens to
// notice second. SubportsUnchanged is next, because a Portfile whose
// structure moved makes every later question meaningless. The isolation
// proof precedes the intent's own judgment because it is about whether
// the edits can be trusted at all, not about whether they worked. And
// the intent's judgment precedes the field sweep, so that a change
// which both failed to reach its target and moved something unrelated
// is reported by the reason the intent knows rather than the generic one.
//
// The riders are folded in after the shadow rather than before it,
// which is sound only while the sole rule is a leading comment: a
// comment cannot change what a Portfile evaluates to, so the prediction
// holds with or without it. The moment Examine offers a rule that can
// move a value, the riders need a shadow of their own — that is the
// double proof, and it is not here yet.
func Finish(ctx context.Context, h port.Handle, src []byte, edits []edit.Edit, id Identity, opts FinishOpts) (*plan.Plan, error) {
	if len(opts.Before) == 0 {
		return nil, ErrNoBefore
	}

	finalSrc, err := edit.Apply(src, edits)
	if err != nil {
		return nil, err
	}
	final, cleanup, err := h.Shadow(finalSrc)
	if err != nil {
		return nil, err
	}
	defer cleanup()
	after, err := final.Snapshot(ctx)
	if err != nil {
		// The verb, not this package's name. A Tcl evaluation that fails
		// under a shadow is the one message where the user most needs to
		// know which run was in flight, and the tail is shared precisely
		// so that it can be reached three ways; the identity is in hand,
		// so the message says so.
		return nil, fmt.Errorf("%s: shadow evaluation: %w", id.Intent, err)
	}
	predicted := opts.Before.Diff(after)
	slog.Debug("shadow prediction", "intent", id.Intent,
		"changed", len(predicted.Changed), "added", len(predicted.Added), "removed", len(predicted.Removed))

	if predicted.Empty() && opts.Witness == "" {
		return nil, ErrNoWitness
	}
	if err := SubportsUnchanged(predicted); err != nil {
		return nil, err
	}
	if opts.ViaSet {
		if err := ViaSetIsolated(predicted, opts.Vals.Name); err != nil {
			return nil, err
		}
	}
	if opts.Accept != nil {
		if err := opts.Accept(predicted); err != nil {
			return nil, err
		}
	}
	if err := OnlyFields(predicted, opts.MayChange); err != nil {
		return nil, err
	}

	// The edit list is copied before anything is added to it or it is
	// sorted: the caller's slice is the caller's, and a tail that
	// reordered it in place would be a surprise waiting for the first
	// planner that looks at its own edits afterwards.
	out := slices.Clone(edits)
	ex := Examine(src, opts.CST, opts.Before, after, opts.Vals, opts.Dependents)
	for _, r := range ex.Riders {
		if opts.Riders[r.Rule] {
			out = append(out, r.Edit)
		}
	}

	// Stable, though no two edits share a start offset today. Riders
	// insert at zero width, and once two of them can land at the same
	// offset an unstable sort makes the plan's bytes depend on the sort
	// implementation — which is the one thing a byte-identity gate
	// cannot survive.
	slices.SortStableFunc(out, func(a, b edit.Edit) int { return a.Start - b.Start })
	return &plan.Plan{
		Format:         plan.Format,
		Intent:         id.Intent,
		Port:           opts.Vals.Name,
		Slug:           id.Slug,
		Summary:        id.Summary,
		ClosesTicket:   id.ClosesTicket,
		Portdir:        h.Target.Portdir,
		Subport:        h.Target.Subport,
		PortfileSHA256: edit.FileSHA256(src),
		Edits:          out,
		Predicted:      plan.FromDelta(predicted),
	}, nil
}
