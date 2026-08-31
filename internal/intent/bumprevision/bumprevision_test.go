package bumprevision

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/eval"
	"github.com/herbygillot/dockhand/internal/macports/info"
	"github.com/herbygillot/dockhand/internal/macports/port"
	"github.com/herbygillot/dockhand/internal/macports/portstyle"
	"github.com/herbygillot/dockhand/internal/macports/tree"
	"github.com/herbygillot/dockhand/internal/plan"
	"github.com/herbygillot/dockhand/internal/tcl/shell"
	"github.com/herbygillot/dockhand/internal/testenv"
)

func newEvaluator(t *testing.T) *eval.Evaluator {
	t.Helper()
	path := testenv.PortTclsh(t)
	proc, err := shell.Start(context.Background(), path)
	require.NoError(t, err)
	ev, err := eval.New(context.Background(), proc)
	require.NoError(t, err)
	t.Cleanup(func() { ev.Close() })
	return ev
}

func portWith(t *testing.T, lines string) port.Handle {
	t.Helper()
	dir := t.TempDir()
	pf := "PortSystem 1.0\nname revbumpee\n" + lines +
		"\ncategories devel\nmaintainers nomaintainer\nlicense MIT\n" +
		"description synthetic revbump target\nlong_description synthetic revbump target\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, macports.PortfileName), []byte(pf), 0o644))
	return port.New(tree.Target{Portdir: dir}, newEvaluator(t))
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

// accept is pure: anything but the revision moving is a violation, and
// the revision must arrive at its successor.
func TestAcceptOnlyTheRevisionMayMove(t *testing.T) {
	vals := info.Values{Name: "foo", Revision: "3"}
	key := info.SubportKey{Subport: "foo"}
	err := accept(vals, "4", info.Delta{Changed: map[info.SubportKey][]info.FieldChange{
		key: {
			{Field: info.FieldRevision, Old: []string{"3"}, New: []string{"4"}},
			{Field: info.FieldVersion, Old: []string{"1.0"}, New: []string{"2.0"}},
		},
	}})
	var d *plan.Decline
	require.ErrorAs(t, err, &d)
	assert.Equal(t, plan.UnexpectedChange, d.Type)
}

// A shared revision line moves every subport's revision together; that
// is the expected shape, not a violation.
func TestAcceptSubportsRevbumpTogether(t *testing.T) {
	vals := info.Values{Name: "foo", Revision: "0"}
	err := accept(vals, "1", info.Delta{Changed: map[info.SubportKey][]info.FieldChange{
		{Subport: "foo"}:     {{Field: info.FieldRevision, Old: []string{"0"}, New: []string{"1"}}},
		{Subport: "foo-sub"}: {{Field: info.FieldRevision, Old: []string{"0"}, New: []string{"1"}}},
	}})
	require.NoError(t, err)
}

func TestAcceptRequiresArrival(t *testing.T) {
	vals := info.Values{Name: "foo", Revision: "3"}
	err := accept(vals, "4", info.Delta{})
	var d *plan.Decline
	require.ErrorAs(t, err, &d)
	assert.Equal(t, plan.TargetNotReached, d.Type)
}
