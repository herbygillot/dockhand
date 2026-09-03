package bumprevision

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/intent"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/info"
	"github.com/herbygillot/dockhand/internal/macports/port"
	"github.com/herbygillot/dockhand/internal/macports/port/porttest"
	"github.com/herbygillot/dockhand/internal/macports/portstyle"
	"github.com/herbygillot/dockhand/internal/plan"
)

func portWith(t *testing.T, lines string) port.Handle {
	t.Helper()
	dir := t.TempDir()
	pf := "PortSystem 1.0\nname revbumpee\n" + lines +
		"\ncategories devel\nmaintainers nomaintainer\nlicense MIT\n" +
		"description synthetic revbump target\nlong_description synthetic revbump target\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, macports.PortfileName), []byte(pf), 0o644))
	return porttest.LiveHandle(t, dir)
}

func TestPlanIncrementsTheRevision(t *testing.T) {
	h := portWith(t, "version 1.0\nrevision 3")
	p, err := BumpRevision{Reason: "openssl soname moved", Riders: intent.RidersNone}.
		Plan(context.Background(), h, nil)
	require.NoError(t, err)
	require.Len(t, p.Edits, 1)
	assert.Equal(t, "3", p.Edits[0].Old)
	assert.Equal(t, "4", p.Edits[0].New)
	assert.Contains(t, p.Edits[0].Reason, "openssl soname moved",
		"the reason travels in the edit, into the plan and the eventual commit")
	assert.Equal(t, "bump-revision", p.Intent)
	assert.Nil(t, p.Riders)
}

// A revbump carries the modeline now, on the run's policy rather than
// on this intent's opinion. It used to carry none: the rule was adopted
// one intent at a time, and a revbump had not adopted it — which was a
// difference no user could see and no test asserted a reason for.
//
// The prediction is the assertion that matters. The rider is folded in
// after the shadow and proved against a second one, so a revbump with a
// modeline must predict exactly what a revbump without it predicts.
func TestPlanCarriesTheModelineRider(t *testing.T) {
	h := portWith(t, "version 1.0\nrevision 3")
	p, err := BumpRevision{Reason: "openssl soname moved"}.Plan(context.Background(), h, nil)
	require.NoError(t, err)
	require.Len(t, p.Edits, 2)
	assert.Equal(t, "modeline", p.Edits[0].Reason, "the insertion at offset 0 sorts first")
	assert.Equal(t, intent.Modeline+"\n", p.Edits[0].New)
	assert.Equal(t, []string{"modeline"}, p.Riders)

	bare, err := BumpRevision{Reason: "openssl soname moved", Riders: intent.RidersNone}.
		Plan(context.Background(), h, nil)
	require.NoError(t, err)
	assert.Equal(t, bare.Predicted, p.Predicted)
	assert.Equal(t, bare.Slug, p.Slug, "a rider does not rename the change it rides on")
	assert.Equal(t, bare.Summary, p.Summary)
}

// 18% of Portfiles carry no revision line, and this used to be where a
// revbump stopped: an insertion needs a placement convention, and the
// tree's positions do not agree on one. Its RELATION does — a revision
// sits under the line carrying the version — so the line is written
// there, at the successor of the implicit 0.
func TestPlanWritesTheMissingRevisionUnderTheVersion(t *testing.T) {
	h := portWith(t, "version 1.0")
	p, err := BumpRevision{Reason: "openssl soname moved", Riders: intent.RidersNone}.
		Plan(context.Background(), h, nil)
	require.NoError(t, err)
	require.Len(t, p.Edits, 1)
	e := p.Edits[0]
	assert.Equal(t, e.Start, e.End, "an insertion replaces nothing")
	assert.Empty(t, e.Old)
	assert.Equal(t, "revision 1\n", e.New)
	assert.Contains(t, e.Reason, "openssl soname moved")
	assert.Equal(t, "revbumpee-rev1", p.Slug, "the counter's successor names the change")

	// The shadow is the whole proof that the line landed where Tcl reads
	// it: a revision written into a comment would arrive at nothing.
	require.Len(t, p.Predicted, 1)
	require.Len(t, p.Predicted[0].Changes, 1)
	assert.Equal(t, []string{"0"}, p.Predicted[0].Changes[0].Old)
	assert.Equal(t, []string{"1"}, p.Predicted[0].Changes[0].New)
}

// The inserted line keeps the Portfile's own value column. MacPorts
// aligns every value at a fixed offset and the carrier's first argument
// is where that column starts, whatever the carrier's style is.
func TestInsertedRevisionKeepsTheValueColumn(t *testing.T) {
	h := portWith(t, "version             1.0")
	p, err := BumpRevision{Reason: "r", Riders: intent.RidersNone}.
		Plan(context.Background(), h, nil)
	require.NoError(t, err)
	require.Len(t, p.Edits, 1)
	assert.Equal(t, "revision            1\n", p.Edits[0].New)
}

// The inserted revision and the modeline rider are two zero-width edits
// in one plan, which is the one shape Apply refuses when they share an
// offset. They do not: the modeline lands at 0 and the revision under
// the version. Worth pinning because it is the first plan in the tree
// that carries two insertions at all.
func TestInsertedRevisionRidesWithTheModeline(t *testing.T) {
	h := portWith(t, "version 1.0")
	p, err := BumpRevision{Reason: "openssl soname moved"}.Plan(context.Background(), h, nil)
	require.NoError(t, err)
	require.Len(t, p.Edits, 2)
	assert.Equal(t, "modeline", p.Edits[0].Reason, "the insertion at offset 0 sorts first")
	assert.Equal(t, "revision 1\n", p.Edits[1].New)
	assert.Equal(t, []string{"modeline"}, p.Riders)

	bare, err := BumpRevision{Reason: "openssl soname moved", Riders: intent.RidersNone}.
		Plan(context.Background(), h, nil)
	require.NoError(t, err)
	assert.Equal(t, bare.Predicted, p.Predicted, "a rider does not move what the change predicts")
}

// The declines the insertion does not make. Each row is a shape whose
// placement would have been a guess, and the point of the type is that
// the Detail says which shape rather than leaving the user to work it
// out from a Portfile they already could not read.
func TestPlanDeclinesEveryAmbiguousRevisionShape(t *testing.T) {
	for _, tc := range []struct {
		name  string
		lines string
		says  string
	}{
		{"subports", "version 1.0\nsubport revbumpee-extra {}", "evaluation contexts"},
		{"revision from elsewhere", "version 1.0\neval revision 5", "evaluates to revision 5"},
		{"version in a set variable", "set v 1.0\nversion ${v}", "`set` variable"},
		{"carrier inside a conditional", "if {1} {\n    version 1.0\n}", "not written at the Portfile's top level"},
		// The line's own shape, both ends of it. A revision goes under
		// the version carrier in that carrier's column, so a carrier
		// sharing its line with something before it has no column to
		// speak of, and one whose line is unterminated has no line after
		// it to write into.
		{"carrier sharing its line", "set foo 1; version 1.0", "shares its line with something before it"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := portWith(t, tc.lines)
			_, err := BumpRevision{Reason: "r"}.Plan(context.Background(), h, nil)
			var d *plan.Decline
			require.ErrorAs(t, err, &d)
			assert.Equal(t, plan.RevisionShapeAmbiguous, d.Type)
			assert.Contains(t, d.Detail, tc.says)
		})
	}
}

// The seventh shape, which portWith cannot build because it always
// writes lines after the ones it is given: a Portfile whose last line is
// the version carrier and which ends without a newline. There is no line
// after the carrier to write the revision into, and appending one would
// be inventing the file's line ending as well as its revision.
func TestPlanDeclinesAnUnterminatedCarrierLine(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, macports.PortfileName), []byte(
		"PortSystem 1.0\nname revbumpee\ncategories devel\nmaintainers nomaintainer\n"+
			"license MIT\ndescription synthetic revbump target\n"+
			"long_description synthetic revbump target\nversion 1.0"), 0o644))

	_, err := BumpRevision{Reason: "r"}.Plan(context.Background(), porttest.LiveHandle(t, dir), nil)
	var d *plan.Decline
	require.ErrorAs(t, err, &d)
	assert.Equal(t, plan.RevisionShapeAmbiguous, d.Type)
	assert.Contains(t, d.Detail, "is not terminated")
}

// The insertion opens on one decline only. A Portfile that DOES write a
// revision and writes it computed is not a Portfile missing a line, and
// Locate said that better than this intent could: propagated, as it
// always was.
func TestPlanPropagatesAComputedRevision(t *testing.T) {
	h := portWith(t, "version 1.0\nset r 3\nrevision ${r}")
	_, err := BumpRevision{Reason: "r"}.Plan(context.Background(), h, nil)
	var d *portstyle.Decline
	require.ErrorAs(t, err, &d)
	assert.Equal(t, portstyle.NotLiteral, d.Type)
}

func TestPlanRequiresAReason(t *testing.T) {
	h := portWith(t, "version 1.0\nrevision 0")
	_, err := BumpRevision{}.Plan(context.Background(), h, nil)
	require.ErrorIs(t, err, ErrNoReason)
}

// Anything but the revision moving is a violation. The sweep that says
// so is Finish's now, asked against this intent's permitted set — so
// the rule is tested where the set is declared, and accept is left to
// judge arrival alone. Note that the revision here DOES arrive: what
// makes this a decline is the version travelling with it.
func TestOnlyTheRevisionMayMove(t *testing.T) {
	key := info.SubportKey{Subport: "foo"}
	predicted := info.Delta{Changed: map[info.SubportKey][]info.FieldChange{
		key: {
			{Field: info.FieldRevision, Old: []string{"3"}, New: []string{"4"}},
			{Field: info.FieldVersion, Old: []string{"1.0"}, New: []string{"2.0"}},
		},
	}}
	require.NoError(t, accept(info.Values{Name: "foo", Revision: "3"}, "4", predicted),
		"the revision did arrive; the intent's own judgment is satisfied")

	err := intent.OnlyFields(predicted, revisionMayChange)
	var d *plan.Decline
	require.ErrorAs(t, err, &d)
	assert.Equal(t, plan.UnexpectedChange, d.Type)
	assert.Equal(t, "foo: version", d.Detail)
}

// A shared revision line moves every subport's revision together; that
// is the expected shape, not a violation, under either question.
func TestSubportsRevbumpTogether(t *testing.T) {
	vals := info.Values{Name: "foo", Revision: "0"}
	predicted := info.Delta{Changed: map[info.SubportKey][]info.FieldChange{
		{Subport: "foo"}:     {{Field: info.FieldRevision, Old: []string{"0"}, New: []string{"1"}}},
		{Subport: "foo-sub"}: {{Field: info.FieldRevision, Old: []string{"0"}, New: []string{"1"}}},
	}}
	require.NoError(t, accept(vals, "1", predicted))
	require.NoError(t, intent.OnlyFields(predicted, revisionMayChange))
}

func TestAcceptRequiresArrival(t *testing.T) {
	vals := info.Values{Name: "foo", Revision: "3"}
	err := accept(vals, "4", info.Delta{})
	var d *plan.Decline
	require.ErrorAs(t, err, &d)
	assert.Equal(t, plan.TargetNotReached, d.Type)
}

// A declared change of reason. This intent used to fold the field sweep
// into the loop that computed arrival, so a delta violating both rules
// at once was reported as the sweep's UnexpectedChange. Finish asks the
// intent's own question first — the order bump and refresh already used
// — so it is now reported as TargetNotReached, which is the more useful
// half: a revbump whose revision did not arrive has not happened at
// all, whatever else moved.
func TestBothViolatedAtOnceReportsTheArrivalFailure(t *testing.T) {
	vals := info.Values{Name: "foo", Revision: "3"}
	predicted := info.Delta{Changed: map[info.SubportKey][]info.FieldChange{
		{Subport: "foo"}: {{Field: info.FieldVersion, Old: []string{"1.0"}, New: []string{"2.0"}}},
	}}
	var arrival, sweep *plan.Decline
	require.ErrorAs(t, accept(vals, "4", predicted), &arrival)
	require.ErrorAs(t, intent.OnlyFields(predicted, revisionMayChange), &sweep)
	assert.Equal(t, plan.TargetNotReached, arrival.Type, "the one Finish asks first, and reports")
	assert.Equal(t, plan.UnexpectedChange, sweep.Type, "the one this intent used to report instead")
}
