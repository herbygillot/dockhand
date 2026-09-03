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
	"bytes"
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/herbygillot/dockhand/internal/distfile"
	"github.com/herbygillot/dockhand/internal/edit"
	"github.com/herbygillot/dockhand/internal/intent"
	"github.com/herbygillot/dockhand/internal/macports/info"
	"github.com/herbygillot/dockhand/internal/macports/port"
	"github.com/herbygillot/dockhand/internal/macports/portstyle"
	"github.com/herbygillot/dockhand/internal/plan"
	"github.com/herbygillot/dockhand/internal/tcl/syntax"
	"github.com/herbygillot/dockhand/internal/text"
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
	// Riders is the run's rider policy. A revbump never declines
	// already-current — the counter always has a successor — so this is
	// only ever the fold, never a withholding.
	Riders intent.RiderPolicy
}

var _ intent.Planner = BumpRevision{}

// ErrNoReason reports a revbump with no stated why. The edit is
// mechanical; the reason is the part only the caller has.
var ErrNoReason = errors.New("bumprevision: a revision bump needs a reason")

// Plan produces the one-edit plan. The returned error is a
// *plan.Decline or *portstyle.Decline when the refusal is a judgment.
//
// The one edit is a rewrite where the Portfile writes a revision and an
// insertion where it does not; revisionEdit decides which, and declines
// RevisionShapeAmbiguous when the file's shape does not say where an
// inserted line belongs.
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

	// The reason travels twice, and both journeys are the plan's: into
	// the edit's own Reason, which is what a reader of the plan sees
	// beside the rewritten line, and into the Summary, which becomes the
	// commit's subject. The Identity carries no third copy — a field
	// nothing reads would promise a commit body that no realizer writes.
	next, e, err := revisionEdit(src, cst, before, vals, b.Reason)
	if err != nil {
		return nil, err
	}
	edits := []edit.Edit{e}

	// The tail is intent.Finish, as it is for every intent. What is left
	// here is the one judgment: that the integer this plan names is the
	// one the Portfile will actually evaluate to. It is the whole proof
	// of an inserted line as much as a rewritten one — a revision written
	// somewhere Tcl does not read it arrives at nothing, and accept says
	// so.
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
			Riders: b.Riders,
			// A revbump spends no network, so the only evidence it can
			// offer is the edit itself: the revision line was written and
			// the result evaluated. That is enough to be falsifiable — when
			// the shadow shows nothing moved, accept below says so — and it
			// is why an empty prediction here is a decline about the port
			// rather than a refusal to have planned.
			Witness: "the revision line was written and the Portfile re-evaluated",
		})
}

// revisionEdit is the one edit a revbump makes, and the successor it
// names: the counter's successor written over the revision line, or —
// where the Portfile has no revision line — a revision line written in.
//
// The insertion is the narrower half, and it is narrow on purpose. A
// port with no revision line is at revision 0 implicitly, so the value
// is never in question: the successor is 1. What was in question was
// placement, and for a long time the answer was to decline, because 18%
// of the tree carries no revision line and where one goes is not
// consistent enough to copy. But the RELATION is consistent even where
// the position is not — a revision sits under the line that carries the
// version — so dockhand writes it there when the Portfile admits exactly
// one such line, and declines by name in every shape where it does not.
func revisionEdit(src []byte, cst *syntax.Script, before info.Snapshot, vals info.Values, reason string) (string, edit.Edit, error) {
	loc, err := portstyle.Locate(src, cst, vals, info.FieldRevision)
	if err == nil {
		n, cerr := strconv.Atoi(loc.Value)
		if cerr != nil {
			return "", edit.Edit{}, &plan.Decline{Type: plan.TransformedStyle,
				Detail: fmt.Sprintf("revision %q is not a plain integer", loc.Value)}
		}
		next := strconv.Itoa(n + 1)
		return next, edit.Edit{
			Start: loc.Span.Start, End: loc.Span.End,
			Old: loc.Span.Text(src), New: next,
			Reason: "revision +1: " + reason,
		}, nil
	}
	// Only the no-style decline opens the insertion. Its siblings are
	// facts about a Portfile that DOES write a revision somewhere:
	// NotLiteral means a revision command was found and none of them
	// carried the evaluated value, which is a computed counter and not a
	// missing one. Locate said those best, so they travel unchanged.
	var d *portstyle.Decline
	if !errors.As(err, &d) || d.Type != portstyle.UnknownStyle {
		return "", edit.Edit{}, err
	}
	e, ierr := insertRevision(src, cst, before, vals, reason)
	if ierr != nil {
		return "", edit.Edit{}, ierr
	}
	return "1", e, nil
}

// insertRevision writes the missing revision line under the version
// carrier, or names the shape that made the placement a guess.
//
// The guards are ordered from the fact that is most about the port to
// the fact that is most about the file, so a reader of the decline is
// told the largest true thing first: a Portfile with subports is
// refused as a Portfile with subports, whatever its version line looks
// like.
//
// Note what is NOT guarded here, because Locate already settled it: this
// runs only on UnknownStyle, and revision has exactly one style, so
// there is no revision command in any scope this port's evaluation
// reads — top level, the conditional bodies that run, or its own subport
// block. A second one hiding in a taken branch would have reached
// NotLiteral instead and never arrived.
func insertRevision(src []byte, cst *syntax.Script, before info.Snapshot, vals info.Values, reason string) (edit.Edit, error) {
	ambiguous := func(detail string) (edit.Edit, error) {
		return edit.Edit{}, &plan.Decline{Type: plan.RevisionShapeAmbiguous, Detail: detail}
	}
	if n := contexts(before); n > 1 {
		// A shared revision line moving every subport together is the right
		// shape for a line that is already there and a decision for one
		// that is not: whether all of them want rebuilding is the
		// maintainer's judgment, and an insertion would make it silently.
		return ambiguous(fmt.Sprintf(
			"the Portfile defines %d evaluation contexts, and one inserted revision line would move all of them", n))
	}
	if vals.Revision != "0" {
		// The counter is coming from somewhere — a PortGroup, most likely.
		// A line saying 1 would contradict it rather than increment it.
		return ambiguous(fmt.Sprintf(
			"the port evaluates to revision %s with no revision line to increment", vals.Revision))
	}
	loc, verr := portstyle.Locate(src, cst, vals, info.FieldVersion)
	if verr != nil {
		what := "the version carrier could not be located"
		var vd *portstyle.Decline
		if errors.As(verr, &vd) {
			what += " (" + vd.Type.String() + ")"
		}
		return ambiguous(what + ", so there is no line to write the revision under")
	}
	if loc.Style == portstyle.SetVariable {
		return ambiguous("the version is carried by a `set` variable, which is not a line a revision belongs under")
	}
	cmd, ok := topLevelCarrier(cst, loc.Span)
	if !ok {
		// A carrier inside an if, a variant or a platform block is a
		// version this host resolved one way and another host may not,
		// and a revision written under the block rather than inside it
		// would not be under that version at all.
		return ambiguous("the version carrier is not written at the Portfile's top level")
	}
	start := lineStart(src, cmd.Span.Start)
	indent := src[start:cmd.Span.Start]
	if bytes.ContainsFunc(indent, func(r rune) bool { return r != ' ' && r != '\t' }) {
		return ambiguous("the version carrier shares its line with something before it")
	}
	nl := bytes.IndexByte(src[cmd.Span.End:], '\n')
	if nl < 0 {
		return ambiguous("the version carrier's line is not terminated, so there is no line after it")
	}
	at := cmd.Span.End + nl + 1
	return edit.Edit{
		Start: at, End: at, Old: "",
		New:    string(indent) + "revision" + gap(cmd) + "1\n",
		Reason: "revision +1 (line added): " + reason,
	}, nil
}

// contexts counts the evaluation contexts a snapshot holds, by subport
// name. The count and not the keys, because a snapshot may carry the
// same subport under several variant frames and a port is one port
// however many frames were evaluated.
func contexts(s info.Snapshot) int {
	names := make(map[string]bool, len(s))
	for k := range s {
		names[k.Subport] = true
	}
	return len(names)
}

// topLevelCarrier finds the top-level command that carries a located
// span as one of its OWN words.
//
// Containment is not the test, and the difference is the whole point: a
// command's span covers its braced bodies, so a `version` inside an `if`
// is contained by a top-level command — the `if` — that is not the
// version line. Insert under that and the revision lands outside the
// condition the version lives in, which is a revision this host would
// read and another host would not.
func topLevelCarrier(cst *syntax.Script, span text.Span) (syntax.Command, bool) {
	for _, item := range cst.Items {
		c, ok := item.(syntax.Command)
		if !ok {
			continue
		}
		for _, w := range c.Words[1:] {
			if w.Span == span {
				return c, true
			}
		}
	}
	return syntax.Command{}, false
}

// lineStart is the offset of the start of the line an offset sits on.
func lineStart(src []byte, at int) int {
	if i := bytes.LastIndexByte(src[:at], '\n'); i >= 0 {
		return i + 1
	}
	return 0
}

// gap is the run of spaces that puts the inserted value in the carrier's
// own column. MacPorts Portfiles align every value at a fixed offset, and
// a revision line that broke the column would be the one thing in the
// diff a reviewer stopped on. The carrier's FIRST argument sets the
// column whatever the style is: `github.setup` puts an author there and
// `version` puts the version, and both are where the column starts.
func gap(cmd syntax.Command) string {
	const width = len("revision")
	if len(cmd.Words) > 1 {
		if n := cmd.Words[1].Span.Start - cmd.Span.Start - width; n > 1 {
			return strings.Repeat(" ", n)
		}
	}
	return " "
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
