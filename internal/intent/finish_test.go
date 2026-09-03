package intent

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
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

// A rule Examine offers and the intent does not accept is not folded
// in. That gate is what lets a rule be written once and adopted one
// intent at a time — and it is what keeps the intents that have not
// adopted the modeline byte-identical.
func TestFinishFoldsInOnlyTheAcceptedRiders(t *testing.T) {
	h := synthetic(t, "version 1.0\nrevision 1")
	src, opts := subject(t, h)
	opts.MayChange = map[info.Field]bool{info.FieldRevision: true}
	edits := []edit.Edit{revisionEdit(t, src, "1", "2")}
	id := Identity{Intent: "bump-revision", Slug: "finishee-rev2", Summary: "finishee: r"}

	declined, err := Finish(context.Background(), h, src, edits, id, opts)
	require.NoError(t, err)
	require.Len(t, declined.Edits, 1)
	assert.Equal(t, "revision +1", declined.Edits[0].Reason)

	opts.Riders = map[Rule]bool{RuleModeline: true}
	accepted, err := Finish(context.Background(), h, src, edits, id, opts)
	require.NoError(t, err)
	require.Len(t, accepted.Edits, 2)
	assert.Equal(t, "modeline", accepted.Edits[0].Reason, "sorted by offset, so a zero-width insertion at 0 leads")
	assert.Equal(t, Modeline+"\n", accepted.Edits[0].New)

	assert.Len(t, edits, 1, "the caller's edit list is the caller's; the tail copies before it appends or sorts")
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
