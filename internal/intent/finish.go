package intent

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"

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

// ErrRiderMoved reports a rider that failed the second half of the
// double proof: the shadow with it predicted something the shadow
// without it did not.
//
// It is a bug in the rule and never a fact about the port, which is why
// it is a bare error rather than a decline — and why only a change that
// was nothing BUT riders returns it. A headline change whose rider
// misbehaves drops the rider and carries on: the user asked for the
// headline, and dockhand's own housekeeping is not entitled to refuse
// it.
var ErrRiderMoved = errors.New("intent: a rider moved the predicted delta")

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
	// Riders is what this run does with the riders Examine offers. Every
	// headline intent is examined, so this is the only thing that decides
	// whether one rides.
	Riders RiderPolicy
	// Witness names the evidence a change rests on when the evaluated
	// delta cannot show it: "the distfiles were fetched and hashed",
	// say. It satisfies the empty-delta refusal and goes no further —
	// nothing about it reaches the plan, whose bytes are a hash gate.
	Witness string
	// Dependents are the ports that depend on this one, passed to
	// Examine for the finding rules.
	Dependents []string
	// Portdir overrides the directory the plan records, for a planner
	// whose handle is deliberately a copy.
	//
	// One planner needs it: a cohort member is planned from the bytes
	// the BRANCH TIP holds rather than from the working tree, which
	// means shadowing those bytes and evaluating the shadow — so the
	// handle's own portdir is a temporary directory, and a plan naming
	// it would name somewhere the change can never land. Empty keeps the
	// handle's own, which is every other planner.
	Portdir string
}

// Finish is the tail every intent runs once its edits are computed:
// apply them to a copy, evaluate the copy, diff it against the state
// before, refuse anything the prediction did not promise, fold in the
// riders the run's policy allows, and assemble the plan.
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
// The riders get a shadow of their own, and that is the second half of
// the double proof. The first half is structural and free — Examine
// offers only edits that touch bytes Tcl never reads — and it is not
// enough on its own, because what an inserted byte SAYS is not something
// a span can answer: a `#` at the start of a line has an innocent span
// and comments a command out. So the accepted set is applied on top of
// the headline's, shadowed a second time, and required to predict
// exactly what the headline predicted alone. A rider that moves the
// prediction is not a rider.
//
// Both sides of that comparison are shadow evaluations, which is why the
// headline is shadowed even when it has no edits: comparing a shadow
// against the real portdir would blame the rider for every difference
// between a portdir and a copy of one.
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
	// The examination happens whatever the rider policy is, and only its
	// riders are policy. --no-riders says what this change carries, not
	// what dockhand is allowed to have noticed: a maintainer's written
	// instruction to revbump a dependent is a fact about the port, and a
	// flag about housekeeping edits must not suppress it.
	ex := Examine(Portfile{
		Src: src, CST: opts.CST, Portdir: portdirOf(h, opts),
		Before: opts.Before, After: after, Vals: opts.Vals,
		Dependents: opts.Dependents,
	})
	var riders []Rider
	if opts.Riders != RidersNone {
		riders = ex.Riders
	}
	if len(riders) > 0 {
		held, err := ridersHold(ctx, h, src, out, riders, opts.Before, predicted)
		if err != nil {
			return nil, err
		}
		if !held {
			if opts.Riders == RidersOnly {
				return nil, fmt.Errorf("%w: %s", ErrRiderMoved, strings.Join(Names(riders), ", "))
			}
			// The headline is the user's and it stands. The rule is
			// dockhand's and it does not. Which half of the proof refused
			// is a debug line inside ridersHold; the warning says the
			// thing the user's run is affected by.
			slog.Warn("riders withheld: the proof did not hold",
				"intent", id.Intent, "rules", strings.Join(Names(riders), ", "))
			riders = nil
		}
	}
	for _, r := range riders {
		out = append(out, r.Edit)
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
		Portdir:        portdirOf(h, opts),
		Subport:        h.Target.Subport,
		PortfileSHA256: edit.FileSHA256(src),
		Edits:          out,
		Riders:         Names(riders),
		Findings:       ex.Findings,
		Predicted:      plan.FromDelta(predicted),
	}, nil
}

// portdirOf is the directory a plan records: the handle's own, unless
// the planner said otherwise because its handle is a copy. It is read
// twice — once for the plan and once for the finding that cites a
// Portfile path — and they must be the same answer.
func portdirOf(h port.Handle, opts FinishOpts) string {
	if opts.Portdir != "" {
		return opts.Portdir
	}
	return h.Target.Portdir
}

// ridersHold is the second half of the double proof: apply the riders on
// top of the headline's own edits, prove the new bytes are still a gap
// in the tree they produced, shadow the result, and require the
// prediction to be exactly what the headline predicted alone.
//
// The re-read of the new tree completes the structural proof rather than
// adding a third one. Examine proves an edit lands where Tcl reads
// nothing; this proves it is still somewhere Tcl reads nothing once it
// is there — which is what "the edit is in a gap" was always taken to
// mean, and is not the same claim. A whole command written into the gap
// between two commands has an innocent span in the old tree and is a
// command in the new one, and the shadow comparison below cannot see it:
// that comparison observes info.Delta's twenty metadata fields, so an
// inserted configure.args-append or post-extract block moves nothing it
// looks at. Measured, both of them, with a rule written to do it.
//
// The whole accepted set is proved at once rather than one rule at a
// time, because the set is what gets applied: two individually inert
// edits can still interact, and proving each alone would prove something
// that never happens. The cost of the finer answer is one shadow per
// rule, and the coarser one is honest — a set that moves the prediction
// does not ride, and the debug line names every rule in it.
func ridersHold(ctx context.Context, h port.Handle, src []byte, edits []edit.Edit, riders []Rider,
	before info.Snapshot, predicted info.Delta) (bool, error) {
	with := make([]edit.Edit, 0, len(edits)+len(riders))
	with = append(with, edits...)
	for _, r := range riders {
		with = append(with, r.Edit)
	}
	withSrc, err := edit.Apply(src, with)
	if err != nil {
		// A rider that cannot be applied beside the headline's own edits
		// is not a rider — two zero-width insertions at one offset are
		// refused outright, and that is the shape a rule collides in.
		// Withheld rather than raised, on the same terms as everything
		// else here: the headline is the user's.
		slog.Debug("rider edits do not apply beside the headline's",
			"rules", strings.Join(Names(riders), ", "), "err", err)
		return false, nil
	}
	if !stillInert(withSrc, ridersLanded(edits, riders)) {
		slog.Debug("rider bytes are not a gap in the tree they made",
			"rules", strings.Join(Names(riders), ", "))
		return false, nil
	}
	shadow, cleanup, err := h.Shadow(withSrc)
	if err != nil {
		return false, err
	}
	defer cleanup()
	after, err := shadow.Snapshot(ctx)
	if err != nil {
		// A Portfile the riders made unevaluable is the strongest
		// possible failure of the proof, and not the run's problem: the
		// headline still evaluates, so the riders come off and the error
		// is a debug line rather than the plan's answer.
		slog.Debug("rider shadow failed to evaluate", "rules", strings.Join(Names(riders), ", "), "err", err)
		return false, nil
	}
	held := before.Diff(after).Equal(predicted)
	slog.Debug("rider shadow prediction", "rules", strings.Join(Names(riders), ", "), "held", held)
	return held, nil
}

// ridersLanded is where the rider bytes ended up in the source the whole
// edit set produced: each rider's own start, shifted by every edit
// applied before it.
//
// It restates edit.Apply's ordering — by (Start, End), spans
// non-overlapping — because that ordering is what makes the shift a sum
// rather than a search. The two must agree; the tests that insert beside
// a headline edit are what says so.
func ridersLanded(headline []edit.Edit, riders []Rider) []edit.Edit {
	type entry struct {
		e     edit.Edit
		rider bool
	}
	all := make([]entry, 0, len(headline)+len(riders))
	for _, e := range headline {
		all = append(all, entry{e: e})
	}
	for _, r := range riders {
		all = append(all, entry{e: r.Edit, rider: true})
	}
	slices.SortStableFunc(all, func(a, b entry) int {
		if a.e.Start != b.e.Start {
			return a.e.Start - b.e.Start
		}
		return a.e.End - b.e.End
	})
	var out []edit.Edit
	shift := 0
	for _, en := range all {
		start := en.e.Start + shift
		shift += len(en.e.New) - (en.e.End - en.e.Start)
		if en.rider {
			out = append(out, edit.Edit{Start: start, End: start + len(en.e.New), New: en.e.New})
		}
	}
	return out
}

// stillInert re-reads the source the edits produced and requires every
// range the riders wrote to be bytes no command occupies.
//
// A source the riders made unparsable fails here rather than in the
// shadow: the answer is the same — these are not riders — and it is
// reached without paying for an evaluation.
func stillInert(src []byte, landed []edit.Edit) bool {
	if len(landed) == 0 {
		return true
	}
	cst, errs := syntax.Parse(src)
	if len(errs) > 0 {
		return false
	}
	for _, span := range landed {
		if span.Start == span.End {
			// A rider that wrote nothing occupies nothing. Nothing to
			// prove, and a zero-width span here would be read as an
			// insertion into a tree it is not being inserted into.
			continue
		}
		if !InCommentOrSpace(src, cst, span) {
			return false
		}
	}
	return true
}
