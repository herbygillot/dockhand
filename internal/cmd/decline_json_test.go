package cmd

// The decline document: what --plan emits when there is no plan.
//
// A caller asking for JSON gets JSON however the run ends. Before the
// exit contract, a declined --plan wrote nothing at all to stdout and
// left the reason in an English sentence on stderr, so every consumer
// of --plan had two parsers or one blind spot.

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/engine"
	"github.com/herbygillot/dockhand/internal/exitcode"
	"github.com/herbygillot/dockhand/internal/macports/info"
	"github.com/herbygillot/dockhand/internal/macports/portstyle"
	"github.com/herbygillot/dockhand/internal/plan"
	"github.com/herbygillot/dockhand/internal/runstate"
	"github.com/herbygillot/dockhand/internal/upstream"
)

// declineDoc is the published shape, restated rather than reached for
// — the same reason statusDoc is restated. A test that names the keys
// it expects is checking the contract; one that unmarshals the
// producer's own struct is agreeing with whatever it currently
// marshals.
type declineDoc struct {
	Exit struct {
		Code     int      `json:"code"`
		Family   string   `json:"family"`
		Reason   string   `json:"reason"`
		Detail   string   `json:"detail"`
		Remedy   string   `json:"remedy"`
		Withheld []string `json:"withheld"`
	} `json:"exit"`
}

func TestPlanDeclineEmitsItsDocument(t *testing.T) {
	d := &plan.Decline{Type: plan.AlreadyCurrent, Detail: "jq is already at 1.8"}
	// Wrapped, the way an intent hands one back: the document is found
	// through the chain, not by the error happening to be a decline.
	in := fmt.Errorf("bump: %w", d)

	var out, errb bytes.Buffer
	rs := &runstate.Context{Out: &out, Err: &errb}
	err := intentAction{opts: engine.Policy{PlanOnly: true}}.sayDecline(rs, in)

	require.ErrorIs(t, err, in, "the decline still travels; the document does not replace it")
	var got declineDoc
	require.NoError(t, json.Unmarshal(out.Bytes(), &got), "stdout must be one JSON document: %s", out.String())
	assert.Equal(t, exitcode.PlanDeclined, got.Exit.Code)
	assert.Equal(t, "declined", got.Exit.Family)
	assert.Equal(t, "already-current", got.Exit.Reason, "the machine name, not the prose")
	assert.Equal(t, "jq is already at 1.8", got.Exit.Detail)
	assert.Equal(t, plan.AlreadyCurrent.Remedy(), got.Exit.Remedy)
	assert.Nil(t, got.Exit.Withheld, "nothing was held back, so the key is absent")
	assert.Equal(t, got.Exit.Code, ExitCode(err), "the document and $? are one fact")
	assert.Empty(t, errb.String())
}

// The same decline having held a rider back. The reason says that
// something went undone and the withheld list says what — a sweep
// deciding whether to come back with --riders needs the second, and a
// list it has to derive from prose is a list it will get wrong.
func TestPlanDeclineNamesWhatItWithheld(t *testing.T) {
	d := &plan.Decline{Type: plan.AlreadyCurrent, Detail: "1.8", Withheld: []string{"modeline"}}

	var out, errb bytes.Buffer
	rs := &runstate.Context{Out: &out, Err: &errb}
	err := intentAction{opts: engine.Policy{PlanOnly: true}}.sayDecline(rs, fmt.Errorf("bump: %w", d))

	var got declineDoc
	require.NoError(t, json.Unmarshal(out.Bytes(), &got), "stdout must be one JSON document: %s", out.String())
	assert.Equal(t, exitcode.AlreadyCurrent, got.Exit.Code)
	assert.Equal(t, "already-current-withheld", got.Exit.Reason)
	assert.Equal(t, []string{"modeline"}, got.Exit.Withheld)
	assert.Equal(t, got.Exit.Code, ExitCode(err))

	// And the sentence a person reads names them too, between the
	// finding and the remedy, because it is part of the finding.
	assert.Contains(t, d.Error(), "(withheld: modeline)")
}

func TestPlanDeclineDocumentCarriesTheUpstreamBand(t *testing.T) {
	// The same decline, banded by the verdict underneath: the words a
	// user reads are the planner's either way, and only the code and
	// the reason say whose problem it was.
	in := upstream.Unresolved(upstream.LivecheckRot, &plan.Decline{
		Type: plan.LatestUnresolved, Detail: "livecheck rot: regex matches nothing (forge has 1.9)"})

	var out, errb bytes.Buffer
	rs := &runstate.Context{Out: &out, Err: &errb}
	err := intentAction{opts: engine.Policy{PlanOnly: true}}.sayDecline(rs, in)

	require.Error(t, err)
	var got declineDoc
	require.NoError(t, json.Unmarshal(out.Bytes(), &got), "stdout must be one JSON document: %s", out.String())
	assert.Equal(t, exitcode.LatestUnresolved, got.Exit.Code)
	assert.Equal(t, "upstream", got.Exit.Family)
	assert.Equal(t, "witness-unresolved", got.Exit.Reason,
		"the wrapper's own token, not the decline's: sharing one would leave the reason unable to say which of the two this was")
	assert.Equal(t, plan.LatestUnresolved.Remedy(), got.Exit.Remedy,
		"the remedy is the decline's, whichever band carries it")
	assert.Equal(t, got.Exit.Code, ExitCode(err))
}

func TestALocationDeclineGetsADocumentToo(t *testing.T) {
	// The revision-less Portfile — near a fifth of the tree — is the
	// most common decline after already-current, and it comes from
	// portstyle rather than from a planner. A --plan that emitted a
	// document for one kind of decline and silence for the other would
	// be exactly the blind spot the document exists to close.
	in := &portstyle.Decline{Type: portstyle.UnknownStyle, Field: info.FieldRevision}

	var out, errb bytes.Buffer
	rs := &runstate.Context{Out: &out, Err: &errb}
	err := intentAction{opts: engine.Policy{PlanOnly: true}}.sayDecline(rs, in)

	require.ErrorIs(t, err, in)
	var got declineDoc
	require.NoError(t, json.Unmarshal(out.Bytes(), &got), "stdout must be one JSON document: %s", out.String())
	assert.Equal(t, exitcode.PlanDeclined, got.Exit.Code, "a location decline is a plan decline one level down")
	assert.Equal(t, "declined", got.Exit.Family)
	assert.Equal(t, "style-unknown", got.Exit.Reason)
	assert.Equal(t, "revision", got.Exit.Detail, "the field it could not find is the detail")
	assert.Equal(t, in.Remedy(), got.Exit.Remedy)
}

func TestOnlyPlanOnlyGetsADocument(t *testing.T) {
	d := &plan.Decline{Type: plan.AlreadyCurrent}

	// --diff's stdout is a patch — something a caller pipes into `git
	// apply` — so a decline there stays prose on stderr and an exit
	// code. One flag, one output language.
	var out, errb bytes.Buffer
	rs := &runstate.Context{Out: &out, Err: &errb}
	err := intentAction{opts: engine.Policy{Diff: true}}.sayDecline(rs, d)
	require.ErrorIs(t, err, d)
	assert.Empty(t, out.String(), "a patch stream never carries a JSON document")

	// And the default realization, which prints nothing on stdout at
	// all.
	out.Reset()
	err = intentAction{}.sayDecline(rs, d)
	require.ErrorIs(t, err, d)
	assert.Empty(t, out.String())

	// A failure that is not a decline is not a decline document either,
	// even under --plan: there is nothing to say about a remedy.
	out.Reset()
	boom := fmt.Errorf("evaluating sysutils/jq: %w", errNoEvaluation)
	err = intentAction{opts: engine.Policy{PlanOnly: true}}.sayDecline(rs, boom)
	require.ErrorIs(t, err, errNoEvaluation)
	assert.Empty(t, out.String())
}

// errNoEvaluation stands in for any ordinary failure reaching the same
// seam: a document is for a decline, and a failure is not one.
var errNoEvaluation = errors.New("the Portfile does not evaluate")
