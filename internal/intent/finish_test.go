package intent

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/edit"
	"github.com/herbygillot/dockhand/internal/exitcode"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/info"
	"github.com/herbygillot/dockhand/internal/macports/port"
	"github.com/herbygillot/dockhand/internal/macports/port/porttest"
	"github.com/herbygillot/dockhand/internal/plan"
)

// synthetic writes a Portfile with no modeline, so the rider rule has
// something to offer, and returns a handle on it.
func synthetic(t *testing.T, lines string) port.Handle {
	t.Helper()
	dir := t.TempDir()
	pf := "PortSystem 1.0\nname finishee\n" + lines +
		"\ncategories devel\nmaintainers nomaintainer\nlicense MIT\n" +
		"description synthetic finish target\nlong_description synthetic finish target\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, macports.PortfileName), []byte(pf), 0o644))
	return porttest.LiveHandle(t, dir)
}

// headed is synthetic's twin for the Portfile that already opens with a
// modeline, and therefore offers no rider at all.
func headed(t *testing.T, lines string) port.Handle {
	t.Helper()
	dir := t.TempDir()
	pf := Modeline + "\nPortSystem 1.0\nname finishee\n" + lines +
		"\ncategories devel\nmaintainers nomaintainer\nlicense MIT\n" +
		"description synthetic finish target\nlong_description synthetic finish target\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, macports.PortfileName), []byte(pf), 0o644))
	return porttest.LiveHandle(t, dir)
}

// subject gathers what every planner holds by the time it reaches the
// tail: the source, the parse, the state before, and the values.
func subject(t *testing.T, h port.Handle) ([]byte, FinishOpts) {
	t.Helper()
	ctx := context.Background()
	src, cst, err := h.Source()
	require.NoError(t, err)
	before, err := h.Snapshot(ctx)
	require.NoError(t, err)
	vals, err := h.Values(ctx)
	require.NoError(t, err)
	return src, FinishOpts{Before: before, Vals: vals, CST: cst}
}

// revisionEdit rewrites the revision literal, which is the smallest
// edit in the catalogue that actually moves an evaluated field.
func revisionEdit(t *testing.T, src []byte, old, next string) edit.Edit {
	t.Helper()
	at := bytes.Index(src, []byte("revision "+old))
	require.GreaterOrEqual(t, at, 0)
	start := at + len("revision ")
	return edit.Edit{Start: start, End: start + len(old), Old: old, New: next, Reason: "revision +1"}
}

// A snapshot is total, so an empty one never came from an evaluation
// that succeeded. Refusing it by name beats diffing against nothing and
// reporting that every context appeared.
func TestFinishNeedsTheBeforeSnapshot(t *testing.T) {
	_, err := Finish(context.Background(), port.Handle{}, []byte("PortSystem 1.0\n"), nil,
		Identity{Intent: "bump"}, FinishOpts{})
	require.ErrorIs(t, err, ErrNoBefore)
}

func TestFinishAssemblesThePlanFromTheIdentity(t *testing.T) {
	h := synthetic(t, "version 1.0\nrevision 3")
	src, opts := subject(t, h)
	opts.MayChange = map[info.Field]bool{info.FieldRevision: true}

	p, err := Finish(context.Background(), h, src,
		[]edit.Edit{revisionEdit(t, src, "3", "4")},
		Identity{Intent: "bump-revision", Slug: "finishee-rev4", Summary: "finishee: openssl soname moved"},
		opts)
	require.NoError(t, err)
	assert.Equal(t, plan.Format, p.Format)
	assert.Equal(t, "bump-revision", p.Intent)
	assert.Equal(t, "finishee", p.Port, "the plan's port is the evaluated name, not the target's spelling")
	assert.Equal(t, "finishee-rev4", p.Slug)
	assert.Equal(t, "finishee: openssl soname moved", p.Summary)
	assert.Equal(t, h.Target.Portdir, p.Portdir)
	assert.Equal(t, edit.FileSHA256(src), p.PortfileSHA256, "the hash pledges the source the edits were computed against")
	require.Len(t, p.Predicted, 1)
	assert.Equal(t, "finishee", p.Predicted[0].Subport)
}

// The ticket is carried for the commit's trailer, in a field of its
// own. What it must never do is reach a name: Slug is the branch and
// Summary is the commit's subject, and both are what the plan gate
// hashes. A trailer is not a subject.
func TestFinishCarriesTheTicketWithoutNamingTheChangeAfterIt(t *testing.T) {
	h := synthetic(t, "version 1.0\nrevision 0")
	src, opts := subject(t, h)
	opts.MayChange = map[info.Field]bool{info.FieldRevision: true}

	p, err := Finish(context.Background(), h, src,
		[]edit.Edit{revisionEdit(t, src, "0", "1")},
		Identity{
			Intent: "bump-revision", Slug: "finishee-rev1", Summary: "finishee: a stated reason",
			ClosesTicket: "12345",
		}, opts)
	require.NoError(t, err)
	assert.Equal(t, "finishee: a stated reason", p.Summary, "the summary is the intent's, not a composition")
	assert.Equal(t, "finishee-rev1", p.Slug, "the branch is named after the change, not after the ticket")
	assert.Equal(t, "12345", p.ClosesTicket, "the bare number; the realizer makes the URL")
}

// An intent that names no ticket leaves the field absent from the
// plan's bytes. It is what keeps every plan that predates --closes
// hashing to exactly what it did.
func TestFinishOmitsAnAbsentTicket(t *testing.T) {
	h := synthetic(t, "version 1.0\nrevision 0")
	src, opts := subject(t, h)
	opts.MayChange = map[info.Field]bool{info.FieldRevision: true}

	p, err := Finish(context.Background(), h, src,
		[]edit.Edit{revisionEdit(t, src, "0", "1")},
		Identity{Intent: "bump-revision", Slug: "finishee-rev1", Summary: "finishee: r"}, opts)
	require.NoError(t, err)

	var buf bytes.Buffer
	require.NoError(t, p.Encode(&buf, exitcode.Twin{}))
	assert.NotContains(t, buf.String(), "closes_ticket")
}

// Every headline intent is examined now, so what decides whether a
// rider rides is the run's policy and nothing else. The plan names what
// it carried, because a note and a pull request body cite the rule
// rather than the edit.
func TestFinishFoldsInRidersOnTheRunsPolicy(t *testing.T) {
	h := synthetic(t, "version 1.0\nrevision 1")
	src, opts := subject(t, h)
	opts.MayChange = map[info.Field]bool{info.FieldRevision: true}
	edits := []edit.Edit{revisionEdit(t, src, "1", "2")}
	id := Identity{Intent: "bump-revision", Slug: "finishee-rev2", Summary: "finishee: r"}

	carried, err := Finish(context.Background(), h, src, edits, id, opts)
	require.NoError(t, err)
	require.Len(t, carried.Edits, 2)
	assert.Equal(t, "modeline", carried.Edits[0].Reason, "sorted by offset, so a zero-width insertion at 0 leads")
	assert.Equal(t, Modeline+"\n", carried.Edits[0].New)
	assert.Equal(t, []string{"modeline"}, carried.Riders)

	opts.Riders = RidersNone
	bare, err := Finish(context.Background(), h, src, edits, id, opts)
	require.NoError(t, err)
	require.Len(t, bare.Edits, 1)
	assert.Equal(t, "revision +1", bare.Edits[0].Reason)
	assert.Nil(t, bare.Riders, "no riders is an absent field, not an empty list")

	assert.Equal(t, bare.Predicted, carried.Predicted,
		"a rider that moved the prediction would not have ridden")
	assert.Len(t, edits, 1, "the caller's edit list is the caller's; the tail copies before it appends or sorts")
}

// The semantic half of the double proof, and the case that shows why
// the structural half cannot stand alone.
//
// The rider below inserts a complete comment line above the revision —
// a line of its own, ending in a newline, in the gap between two
// commands. The structural proof passes it twice over: nothing it
// touches was evaluated before, and nothing it wrote is a command after.
// What it actually does is end in a backslash, which continues a Tcl
// comment onto the next line and swallows the revision whole. The bytes
// are a comment in both trees and the port evaluates to something else,
// which no span can report and only an evaluation can.
func TestFinishWithholdsARiderThatMovesThePrediction(t *testing.T) {
	h := synthetic(t, "version 1.0\nrevision 1")
	src, opts := subject(t, h)
	opts.MayChange = map[info.Field]bool{info.FieldRevision: true}
	edits := []edit.Edit{revisionEdit(t, src, "1", "2")}
	id := Identity{Intent: "bump-revision", Slug: "finishee-rev2", Summary: "finishee: r"}

	const swallow = "# a note \\\n"
	withRules(t, "commenter", func(s []byte) (edit.Edit, bool) {
		at := indexOf(t, string(s), "revision ")
		return edit.Edit{Start: at, End: at, Old: "", New: swallow, Reason: "commenter"}, true
	})

	// The structural proof passes: the point it lands on begins a
	// command, the line it writes is its own, and a gap boundary is where
	// every housekeeping line in a Portfile goes.
	at := indexOf(t, string(src), "revision ")
	require.True(t, InCommentOrSpace(src, opts.CST, edit.Edit{Start: at, End: at, New: swallow}))
	require.Len(t, Riders(src, opts.CST), 1, "so Examine offers it")
	// And it passes again in the tree it made, where its bytes are a
	// comment: the revision line is inside that comment rather than
	// beside it, so there is no command for the range to overlap.
	require.True(t, stillInert(mustApply(t, src, append(slices.Clone(edits),
		edit.Edit{Start: at, End: at, New: swallow})),
		ridersLanded(edits, []Rider{{Rule: "commenter", Edit: edit.Edit{Start: at, End: at, New: swallow}}})))

	// The second does not, and the headline change carries on without it.
	p, err := Finish(context.Background(), h, src, edits, id, opts)
	require.NoError(t, err)
	require.Len(t, p.Edits, 1, "the rider came off; the revision edit is the user's and stands")
	assert.Equal(t, "revision +1", p.Edits[0].Reason)
	assert.Nil(t, p.Riders)

	// With nothing but the rider to plan there is nothing left to fall
	// back to, and the rule's bug is said out loud.
	opts.Riders = RidersOnly
	opts.Witness = "the rider edits were proved inert"
	opts.MayChange = nil
	_, err = Finish(context.Background(), h, src, nil, id, opts)
	require.ErrorIs(t, err, ErrRiderMoved)
	assert.Contains(t, err.Error(), "commenter")
}

// The structural proof is made in the tree the edits produce as well as
// in the one they were computed against, and this is the rider that
// says why.
//
// It writes complete lines into the gap in front of a command — an
// insertion ending in a newline, at a boundary, which is the shape every
// housekeeping line has and which the old-tree proof therefore cannot
// distinguish. What it writes is two commands. The semantic proof cannot
// cover for it either: info.Delta observes twenty metadata fields, and
// neither configure.args nor a post-extract block is one of them, so the
// prediction does not move and the smuggled build step rides into a
// branch that never sees a VM. Measured, with this rule.
func TestFinishWithholdsARiderThatWritesACommandIntoTheGap(t *testing.T) {
	h := synthetic(t, "version 1.0\nrevision 1\nconfigure.args --with-foo")
	src, opts := subject(t, h)
	opts.MayChange = map[info.Field]bool{info.FieldRevision: true}
	edits := []edit.Edit{revisionEdit(t, src, "1", "2")}
	id := Identity{Intent: "bump-revision", Slug: "finishee-rev2", Summary: "finishee: r"}

	const smuggled = "post-extract { system \"echo pwned\" }\nconfigure.args-append --enable-evil\n"
	withRules(t, "smuggler", func(s []byte) (edit.Edit, bool) {
		at := indexOf(t, string(s), "configure.args ")
		return edit.Edit{Start: at, End: at, Old: "", New: smuggled, Reason: "smuggler"}, true
	})

	// The old tree cannot see it: the bytes land in a gap and end their
	// line, which is every housekeeping line's shape.
	at := indexOf(t, string(src), "configure.args ")
	require.True(t, InCommentOrSpace(src, opts.CST, edit.Edit{Start: at, End: at, New: smuggled}))
	require.Len(t, Riders(src, opts.CST), 1, "so Examine offers it")

	p, err := Finish(context.Background(), h, src, edits, id, opts)
	require.NoError(t, err)
	require.Len(t, p.Edits, 1, "the smuggled commands came off; the revision edit stands")
	assert.Nil(t, p.Riders)
}

// A rider whose edits make the Portfile unevaluable fails the proof
// rather than the run: the shadow it broke is its own, and the headline
// still evaluates.
func TestFinishWithholdsARiderThatBreaksTheEvaluation(t *testing.T) {
	h := synthetic(t, "version 1.0\nrevision 1")
	src, opts := subject(t, h)
	opts.MayChange = map[info.Field]bool{info.FieldRevision: true}
	edits := []edit.Edit{revisionEdit(t, src, "1", "2")}

	withRules(t, "breaker", func([]byte) (edit.Edit, bool) {
		return edit.Edit{Start: 0, End: 0, Old: "", New: "{\n", Reason: "breaker"}, true
	})
	p, err := Finish(context.Background(), h, src, edits,
		Identity{Intent: "bump-revision", Slug: "finishee-rev2", Summary: "finishee: r"}, opts)
	require.NoError(t, err)
	require.Len(t, p.Edits, 1)
	assert.Nil(t, p.Riders)
}

// The witness rule: a plan that predicts nothing claims nothing, and a
// claim nothing can contradict is not a claim. An intent that can
// legitimately reach an empty delta either declines first or says what
// its evidence was.
func TestFinishRefusesAnEmptyDeltaWithoutAWitness(t *testing.T) {
	h := synthetic(t, "version 1.0\nrevision 0")
	src, opts := subject(t, h)
	// A leading comment cannot change what a Portfile evaluates to.
	inert := []edit.Edit{{Start: 0, End: 0, Old: "", New: "# nothing to see\n", Reason: "inert"}}
	id := Identity{Intent: "bump", Slug: "finishee-1.0", Summary: "finishee: update to 1.0"}

	_, err := Finish(context.Background(), h, src, inert, id, opts)
	require.ErrorIs(t, err, ErrNoWitness)

	opts.Witness = "the distfiles were fetched and hashed; upstream re-rolled nothing"
	p, err := Finish(context.Background(), h, src, inert, id, opts)
	require.NoError(t, err)
	assert.Empty(t, p.Predicted)

	var buf bytes.Buffer
	require.NoError(t, p.Encode(&buf, exitcode.Twin{}))
	assert.NotContains(t, buf.String(), "re-rolled nothing", "a witness is a plan-time precondition, not a plan field")
}

// When a change both failed to reach its target and moved something
// unrelated, the intent's own reason is the informative one. The field
// sweep runs after the intent's judgment for exactly that.
func TestFinishReportsTheIntentsReasonBeforeTheFieldSweep(t *testing.T) {
	h := synthetic(t, "version 1.0\nrevision 0")
	src, opts := subject(t, h)
	// A revision-only intent, handed a version edit: the sweep would
	// refuse it, and so would the intent.
	opts.MayChange = map[info.Field]bool{info.FieldRevision: true}
	at := bytes.Index(src, []byte("version 1.0"))
	require.GreaterOrEqual(t, at, 0)
	edits := []edit.Edit{{Start: at + len("version "), End: at + len("version 1.0"), Old: "1.0", New: "2.0", Reason: "version"}}
	id := Identity{Intent: "bump-revision", Slug: "finishee-rev1", Summary: "finishee: r"}

	sweptOnly, err := Finish(context.Background(), h, src, edits, id, opts)
	require.Nil(t, sweptOnly)
	assert.Equal(t, plan.UnexpectedChange, declineOf(t, err).Type)

	opts.Accept = func(info.Delta) error {
		return &plan.Decline{Type: plan.TargetNotReached, Detail: "revision would not become 1"}
	}
	_, err = Finish(context.Background(), h, src, edits, id, opts)
	d := declineOf(t, err)
	assert.Equal(t, plan.TargetNotReached, d.Type, "the intent knows why; the sweep only knows that")
	assert.Equal(t, "revision would not become 1", d.Detail)
}

// A structural change makes every later question meaningless, so it is
// asked before the intent's own judgment — which is where all three
// copies already agreed.
func TestFinishAsksAboutSubportsBeforeTheIntentJudges(t *testing.T) {
	h := synthetic(t, "version 1.0\nrevision 0\nsubport finishee-extra {}")
	src, opts := subject(t, h)
	opts.Accept = func(info.Delta) error {
		t.Fatal("the intent's judgment must not run on a delta whose contexts moved")
		return nil
	}
	at := bytes.Index(src, []byte("subport finishee-extra {}"))
	require.GreaterOrEqual(t, at, 0)
	edits := []edit.Edit{{
		Start: at, End: at + len("subport finishee-extra {}"),
		Old: "subport finishee-extra {}", New: "subport finishee-other {}", Reason: "rename",
	}}

	_, err := Finish(context.Background(), h, src, edits,
		Identity{Intent: "bump", Slug: "s", Summary: "s"}, opts)
	d := declineOf(t, err)
	assert.Equal(t, plan.SubportsChanged, d.Type)
	assert.Equal(t, "1 added, 1 removed", d.Detail)
}
