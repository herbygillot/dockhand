package record

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// all is every state schema 3 writes. A state added without a line
// here is a state the tables below never weigh.
var all = []RunState{
	Queued, Submitting, Running, Passed, Failed,
	Unsupported, Blocked, Canceled, Superseded, Errored,
	Withheld,
}

func TestRunStateIsTheBareWireWord(t *testing.T) {
	// The words are what the notes and the goldens carry; a rename here
	// is a wire change. "queued" is where schema 2 wrote "deferred",
	// which is the one word this schema respells.
	assert.Equal(t, []string{
		"queued", "submitting", "running", "passed", "failed",
		"unsupported", "blocked", "canceled", "superseded", "errored", "withheld"}, func() []string {
		out := make([]string, 0, len(all))
		for _, s := range all {
			out = append(out, s.String())
		}
		return out
	}())
}

func TestParseRunStateAcceptsEveryStateOnTheWire(t *testing.T) {
	for _, s := range all {
		got, err := ParseRunState(string(s))
		require.NoError(t, err)
		assert.Equal(t, s, got)
	}
}

func TestParseRunStateRefusesAWordThisBuildDoesNotKnow(t *testing.T) {
	// "deferred" is in the list on purpose: it was schema 2's word for
	// queued, and a build that still accepted it would let the old
	// spelling back onto the wire one flag at a time.
	for _, s := range []string{"", "quantum", "deferred", "Passed", "passed "} {
		_, err := ParseRunState(s)
		require.ErrorIs(t, err, ErrUnknownRunState, "%q", s)
		assert.Contains(t, err.Error(), s, "the refusal quotes what it was given")
	}
}

func TestWeight(t *testing.T) {
	for _, tc := range []struct {
		state RunState
		want  Weight
	}{
		{Passed, Positive},
		{Failed, Negative},
		// Neither of these tested the change: one is the port declining
		// the platform, the other something failing before the change was
		// reached. A refusal to test is not evidence either way.
		{Unsupported, Neutral},
		{Blocked, Neutral},
		// States of the run rather than findings about the port.
		{Queued, Neutral},
		{Submitting, Neutral},
		{Running, Neutral},
		{Canceled, Neutral},
		{Superseded, Neutral},
		{Errored, Neutral},
		// A word this build cannot read is not evidence.
		{RunState("quantum"), Neutral},
		{RunState(""), Neutral},
	} {
		t.Run(string(tc.state), func(t *testing.T) {
			assert.Equal(t, tc.want, tc.state.Weight())
		})
	}
}

func TestTerminal(t *testing.T) {
	for _, tc := range []struct {
		state RunState
		want  bool
	}{
		{Passed, true},
		{Failed, true},
		{Unsupported, true},
		{Blocked, true},
		{Canceled, true},
		{Superseded, true},
		{Errored, true},
		// A worker is still on it.
		{Running, false},
		// Waiting for a slot: status's pump submits it when one frees,
		// so nothing should read a queued run as an outcome.
		{Queued, false},
		// The claim is down and the guest is starting. This is the one
		// that matters: a claimed run reading terminal lets a peer
		// conclude the work is finished and start a second guest, which
		// is the exact failure the claim exists to end.
		{Submitting, false},
		{RunState("quantum"), false},
	} {
		t.Run(string(tc.state), func(t *testing.T) {
			assert.Equal(t, tc.want, tc.state.Terminal())
		})
	}
}

func TestAClaimedRunIsNeverTerminal(t *testing.T) {
	// Stated on its own, because it is a safety property and not a
	// table row: everything a peer could read as "nobody is on this"
	// must be false while a claim is down.
	require.False(t, Submitting.Terminal())
	assert.Equal(t, Neutral, Submitting.Weight())
}

func TestOnlyAPassAndAFailureCarryWeight(t *testing.T) {
	// The property behind Promotable: exactly one state argues for the
	// change and exactly one argues against it.
	var positive, negative int
	for _, s := range all {
		switch s.Weight() {
		case Positive:
			positive++
		case Negative:
			negative++
		case Neutral:
		}
	}
	assert.Equal(t, 1, positive)
	assert.Equal(t, 1, negative)
}

func TestDestinationIsAWordAndNotANumber(t *testing.T) {
	// The engine held this as an iota, which is fine inside a process
	// and wrong on a wire: a number's meaning lives in the order of a
	// const block, and inserting a member renumbers every note on disk.
	assert.Equal(t, "branch", string(ToBranch))
	assert.Equal(t, "verdict", string(ToVerdict))
	assert.Equal(t, "published", string(ToPublished))
}

func TestDispositionIsTheAnswerToAFinding(t *testing.T) {
	assert.Equal(t, "proposed", string(Proposed))
	assert.Equal(t, "accepted", string(Accepted))
	assert.Equal(t, "dismissed", string(Dismissed))
}

// Withheld is a state this build gives a subject it chose not to run.
// It is terminal — nothing will come back to change it — and it weighs
// nothing, because holding a member back is not evidence for or against
// the change.
func TestWithheldIsTerminalAndWeighsNothing(t *testing.T) {
	assert.True(t, Withheld.Terminal(), "no later reading turns a withheld run into an outcome")
	assert.Equal(t, Neutral, Withheld.Weight(),
		"a member nobody built argues neither for the change nor against it")

	got, err := ParseRunState("withheld")
	require.NoError(t, err, "the word must survive a round trip through the wire")
	assert.Equal(t, Withheld, got)
}

// And it must never be mistaken for a pass. A promotion sums the passes
// over every member, so a withheld member counting as one would
// authorize publishing on evidence nobody produced.
func TestWithheldIsNotAPass(t *testing.T) {
	assert.NotEqual(t, Passed.Weight(), Withheld.Weight())
}
