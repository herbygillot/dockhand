package intent

import (
	"bytes"
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/edit"
	"github.com/herbygillot/dockhand/internal/exitcode"
	"github.com/herbygillot/dockhand/internal/plan"
)

// --riders makes housekeeping the whole change: the riders are the
// plan's only edits, the change is named after them rather than after
// the verb that chose the port, and the prediction is empty because a
// rider that predicted anything would not have ridden.
func TestHousekeepingPlansTheRidersAlone(t *testing.T) {
	h := synthetic(t, "version 1.0\nrevision 1")
	p, err := Housekeeping{}.Plan(context.Background(), h, nil)
	require.NoError(t, err)

	assert.Equal(t, "housekeeping", p.Intent)
	assert.Equal(t, "finishee-housekeeping", p.Slug)
	assert.Equal(t, "finishee: add the editor modeline", p.Summary)
	require.Len(t, p.Edits, 1)
	assert.Equal(t, "modeline", p.Edits[0].Reason)
	assert.Equal(t, 0, p.Edits[0].Start)
	assert.Equal(t, 0, p.Edits[0].End)
	assert.Equal(t, []string{"modeline"}, p.Riders)
	assert.Empty(t, p.Predicted, "housekeeping moves nothing a port evaluates")

	// And the key is absent rather than null. This is the one plan in the
	// tree that predicts nothing, so it is the one that would have handed
	// a consumer iterating `.predicted[]` a null it never had to guard.
	var buf bytes.Buffer
	require.NoError(t, p.Encode(&buf, exitcode.Twin{}))
	assert.NotContains(t, buf.String(), `"predicted"`)
}

// Asked for housekeeping where there is none, the answer is the plain
// already-current decline. Nothing was withheld — the caller asked for
// exactly this and there was none of it — so it keeps the ordinary
// declined band rather than the one that means something went undone.
func TestHousekeepingDeclinesWhenEveryRuleAlreadyHolds(t *testing.T) {
	h := headed(t, "version 1.0\nrevision 1")
	_, err := Housekeeping{}.Plan(context.Background(), h, nil)

	d := declineOf(t, err)
	assert.Equal(t, plan.AlreadyCurrent, d.Type)
	assert.Empty(t, d.Withheld)
	assert.Equal(t, exitcode.PlanDeclined, d.DockhandExit())
	assert.Contains(t, d.Detail, "needs no housekeeping")
	// This build's rules against these bytes, and nothing else. The
	// build is not in the memo's key and does not have to be:
	// ledger.MemoFormat is bumped by hand when a rule changes.
	assert.Equal(t, plan.ByPortfile, d.Determined)
	assert.True(t, d.Memoizable())
}

// A rule that had an edit and lost it to the first proof is a different
// fact from a port that is already clean, and this is the run where the
// difference matters most: the caller asked for the housekeeping by
// name. It used to be told every rule already holds, with the suppressed
// rule visible only under --debug.
func TestHousekeepingSaysWhenARuleWasSuppressedRatherThanSatisfied(t *testing.T) {
	h := synthetic(t, "version 1.0\nrevision 1")
	withRules(t, "overreaching", func(s []byte) (edit.Edit, bool) {
		at := indexOf(t, string(s), "revision 1")
		return edit.Edit{Start: at, End: at + len("revision 1"), Old: "revision 1",
			New: "revision 2", Reason: "overreaching"}, true
	})
	_, err := Housekeeping{}.Plan(context.Background(), h, nil)

	d := declineOf(t, err)
	assert.Equal(t, plan.AlreadyCurrent, d.Type)
	assert.Contains(t, d.Detail, "overreaching")
	assert.Contains(t, d.Detail, "a bug in the rule")
	assert.NotContains(t, d.Detail, "every rule this build knows already holds",
		"the port is not clean; a rule misfired")
}
