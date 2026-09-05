package record

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// all is every state schema 3 writes. A state added without a line
// here is a state the tables below never test.
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

func TestOutcome(t *testing.T) {
	for _, tc := range []struct {
		state RunState
		want  bool
	}{
		// Answers to "what happened to this port here" — including the
		// three where what happened is that it was not tried: a port
		// declining the platform, a member behind a failed prerequisite,
		// and a member this build held back have each been answered for.
		{Passed, true},
		{Failed, true},
		{Unsupported, true},
		{Blocked, true},
		{Withheld, true},
		// The machine's silence, a person's "no", and the branch moving
		// out from under the run: terminal, and about something other
		// than the port. A best-effort dependent in one of these is
		// unanswered, and the change waits.
		{Errored, false},
		{Canceled, false},
		{Superseded, false},
		// Not finished, so not yet an answer.
		{Queued, false},
		{Submitting, false},
		{Running, false},
		// A word this build cannot read says nothing about the port.
		{RunState("quantum"), false},
		{RunState(""), false},
	} {
		t.Run(string(tc.state), func(t *testing.T) {
			assert.Equal(t, tc.want, tc.state.Outcome())
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
	assert.False(t, Submitting.Outcome())
}

// The property behind the gate, read over every state: exactly one
// state argues for a change and exactly one argues against it. A
// subject whose only run is s is promotable when and only when s is a
// pass, and a pass beside a run in s is promotable unless s is a
// failure. Everything else — a refusal to test, a run still going, the
// machine's silence, a word this build cannot read — is evidence
// neither for the change nor against it.
//
// This was stated through a per-state weight that the gate did not
// read, and the tally built on it could disagree with the gate at a
// cohort without either noticing. It is stated against the gate now,
// so a state that starts arguing either way fails here rather than
// somewhere a tally would have hidden it.
func TestOnlyAPassArguesForAndOnlyAFailureAgainst(t *testing.T) {
	jq := []Subject{{Port: "jq"}}
	states := append(append([]RunState{}, all...), RunState("quantum"), RunState(""))
	for _, s := range states {
		t.Run(string(s), func(t *testing.T) {
			alone := Record{Subjects: jq, Runs: map[string]Run{
				RunKey("jq", "Testos"): {State: s}}}
			assert.Equal(t, s == Passed, alone.Promotable(), "alone")

			beside := Record{Subjects: jq, Runs: map[string]Run{
				RunKey("jq", "Testos"): {State: Passed},
				RunKey("jq", "Oldos"):  {State: s}}}
			assert.Equal(t, s != Failed, beside.Promotable(), "beside a pass")
		})
	}
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
// It is terminal — nothing will come back to change it — and it is an
// outcome, because the gate asks each dependent for an answer and a
// member held back on purpose has one: this build did not run it, and
// nothing about it is the reason.
func TestWithheldIsTerminalAndAnOutcome(t *testing.T) {
	assert.True(t, Withheld.Terminal(), "no later reading turns a withheld run into something else")
	assert.True(t, Withheld.Outcome(), "a member nobody built is answered for, not unanswered")

	got, err := ParseRunState("withheld")
	require.NoError(t, err, "the word must survive a round trip through the wire")
	assert.Equal(t, Withheld, got)
}

// And it must never be mistaken for a pass. A promotion needs some run
// to have proven the change, and a withheld member counting as proof
// would authorize publishing on evidence nobody produced.
func TestWithheldIsNotAPass(t *testing.T) {
	r := Record{
		Subjects: []Subject{{Port: "gegl-devel"}},
		Runs:     map[string]Run{RunKey("gegl-devel", "Testos"): {State: Withheld, Platform: "Testos"}},
	}
	assert.False(t, r.Promotable(), "answered for, and not proven")
}
