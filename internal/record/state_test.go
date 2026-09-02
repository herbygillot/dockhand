package record

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// all is every state schema 2 writes. A state added without a line
// here is a state the tables below never weigh.
var all = []RunState{
	Running, Passed, Failed, Unsupported, Blocked,
	Canceled, Superseded, Deferred, Errored,
}

func TestRunStateIsTheBareWireWord(t *testing.T) {
	// The words are what notes on disk and the goldens carry; a rename
	// here is a wire change.
	assert.Equal(t, []string{
		"running", "passed", "failed", "unsupported", "blocked",
		"canceled", "superseded", "deferred", "errored",
	}, func() []string {
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
	for _, s := range []string{"", "quantum", "Passed", "passed "} {
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
		// the platform, the other a dependency failing before the change
		// was reached. A refusal to test is not evidence either way.
		{Unsupported, Neutral},
		{Blocked, Neutral},
		// States of the run rather than findings about the port.
		{Running, Neutral},
		{Canceled, Neutral},
		{Superseded, Neutral},
		{Deferred, Neutral},
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
		// Queued, not finished: status's pump submits it when a slot
		// frees, so nothing should read a deferred run as an outcome.
		{Deferred, false},
		{RunState("quantum"), false},
	} {
		t.Run(string(tc.state), func(t *testing.T) {
			assert.Equal(t, tc.want, tc.state.Terminal())
		})
	}
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
