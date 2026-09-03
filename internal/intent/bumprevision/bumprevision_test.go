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
	p, err := BumpRevision{Reason: "openssl soname moved"}.Plan(context.Background(), h, nil)
	require.NoError(t, err)
	require.Len(t, p.Edits, 1)
	assert.Equal(t, "3", p.Edits[0].Old)
	assert.Equal(t, "4", p.Edits[0].New)
	assert.Contains(t, p.Edits[0].Reason, "openssl soname moved",
		"the reason travels in the edit, into the plan and the eventual commit")
	assert.Equal(t, "bump-revision", p.Intent)
}

// 18% of Portfiles carry no revision line; giving one a line means an
// insertion, which needs a placement convention the tree does not have.
// The decline is Locate's own, propagated because it is the truth.
func TestPlanDeclinesWithoutARevisionLine(t *testing.T) {
	h := portWith(t, "version 1.0")
	_, err := BumpRevision{Reason: "r"}.Plan(context.Background(), h, nil)
	var d *portstyle.Decline
	require.ErrorAs(t, err, &d)
	assert.Equal(t, portstyle.UnknownStyle, d.Type)
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
