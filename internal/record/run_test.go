package record

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestTerminalNamesTheThreeStatesWorkStillMoves(t *testing.T) {
	for _, s := range []RunState{Queued, Submitting, Running} {
		assert.False(t, s.Terminal(), "state %q", s)
	}
	for _, s := range []RunState{
		Passed, Failed, Unsupported, Blocked, Canceled, Superseded, Errored, Withheld,
	} {
		assert.True(t, s.Terminal(), "state %q", s)
	}
}

func TestAClaimedRunIsNeverTerminal(t *testing.T) {
	// Submitting is the window between the claim going down and the
	// provider handing back a lease. A run that read terminal there
	// would let a peer conclude the work was finished and start a second
	// guest for it, which is the exact failure the claim ends.
	assert.False(t, Submitting.Terminal())
}

func TestAWordThisBuildDoesNotKnowIsTerminal(t *testing.T) {
	// A state from another shape has no drain behind it and no provider
	// that will ever settle it, so reading it as unfinished would hold a
	// lease open on nothing.
	assert.True(t, RunState("deferred").Terminal())
	assert.True(t, RunState("").Terminal())
}

func TestAskIsSeparateFromWhatCameBack(t *testing.T) {
	// The four flags are on the ASK, so a queued attempt with no verdict
	// yet still carries what the person requested. The zero ask is a
	// plain build.
	assert.Equal(t, Ask{}, Run{}.Ask)
	a := Ask{Test: true, KeepEnv: true, FromSource: true, Forced: "py312-foo"}
	r := Run{Ask: a, State: Queued}
	assert.Equal(t, a, r.Ask)
}

func TestLintDistinguishesNotLintedFromLintedAndSilent(t *testing.T) {
	// One field where the shipped record had two — a Linted bool beside
	// a Lint string — so "linted, and it said nothing" and "not linted"
	// were spelled by a combination rather than by a value.
	assert.Nil(t, Run{}.Lint)
	silent := ""
	assert.NotNil(t, Run{Lint: &silent}.Lint)
	assert.Empty(t, *Run{Lint: &silent}.Lint)
}
