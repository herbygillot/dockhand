package verdict

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/herbygillot/dockhand/internal/exitcode"
)

// The matches are the answer, so the type carries them and the
// sentence is built from them rather than the other way about.
func TestAmbiguousTargetError(t *testing.T) {
	err := &AmbiguousTargetError{Target: "jq", Matches: []string{"dockhand/jq-1.8.1", "dockhand/jq-1.8.2"}}
	assert.Equal(t,
		`ambiguous target: "jq" names 2 branches (dockhand/jq-1.8.1, dockhand/jq-1.8.2); use the full branch name`,
		err.Error())
	assert.Equal(t, exitcode.Ambiguous, err.DockhandExit())
	assert.Equal(t, "declined", exitcode.Family(err.DockhandExit()))
	assert.Equal(t, "ambiguous-target", err.Code())
}

// The three ways a run ends without a verdict about the port. They
// used to be one sentence — "no environment available" — which sent a
// user whose neighbour was broken off to provision a machine that was
// fine. Each is its own code inside one family, so a script can treat
// them alike and a person cannot be misled.
func TestTerminalRunRefusals(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		code int
		msg  string
		why  string
	}{
		{
			name: "blocked",
			err:  &BlockedError{Port: "jq", Platform: "Sequoia", Detail: "dependency olm fails to build"},
			code: exitcode.VerifyBlocked,
			msg:  "verification of jq on Sequoia was blocked before it reached the change: dependency olm fails to build",
			why:  "verification-blocked",
		},
		{
			name: "unsupported",
			err:  &UnsupportedError{Port: "jq", Platform: "Sequoia", Detail: "no base for Sequoia"},
			code: exitcode.VerifyUnsupported,
			msg:  "verification of jq on Sequoia is not something this provider can run: no base for Sequoia",
			why:  "verification-unsupported",
		},
		{
			name: "errored",
			err:  &ErroredError{Port: "jq", Platform: "Sequoia", Detail: "the agent never answered"},
			code: exitcode.VerifyErrored,
			msg:  "verification of jq on Sequoia could not answer: the agent never answered",
			why:  "verification-errored",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.msg, tc.err.Error())
			assert.Equal(t, exitcode.Twin{Code: tc.code, Family: "verdict", Reason: tc.why},
				exitcode.TwinOf(tc.err))
			assert.Equal(t, tc.code, exitcode.TwinOf(fmt.Errorf("verify: %w", tc.err)).Code,
				"the band survives wrapping, which is how it reaches cmd")
		})
	}
}

// The three ways a run ends that are nobody's failure. A follow used to
// answer all three with "could not answer", which is a fact about the
// machine — so a user who canceled their own build was told the
// environment had broken, and a run still sitting in the queue was
// reported as a verification that had ended.
func TestARunEndingWithoutAFailure(t *testing.T) {
	for _, tc := range []struct {
		name   string
		err    error
		code   int
		family string
		msg    string
		why    string
	}{
		{
			name:   "canceled",
			err:    &CanceledError{Port: "jq", Platform: "Sequoia", Detail: "canceled by the user"},
			code:   exitcode.VerifyErrored,
			family: "verdict",
			msg:    "verification of jq on Sequoia was canceled: canceled by the user",
			why:    "verification-canceled",
		},
		{
			name:   "superseded",
			err:    &SupersededError{Port: "jq", Platform: "Sequoia"},
			code:   exitcode.Superseded,
			family: "refused",
			msg:    "verification of jq on Sequoia was superseded by a newer run",
			why:    "verification-superseded",
		},
		{
			name:   "queued",
			err:    &QueuedError{Port: "jq", Platform: "Sequoia", Detail: "all 2 verification slots are busy"},
			code:   exitcode.VerifyQueued,
			family: "pending",
			msg: "verification of jq on Sequoia has not started yet: all 2 verification slots are busy" +
				" — `dockhand status` starts it when it can",
			why: "verify-queued",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.msg, tc.err.Error())
			assert.Equal(t, exitcode.Twin{Code: tc.code, Family: tc.family, Reason: tc.why},
				exitcode.TwinOf(tc.err))
		})
	}
}

// Both halves of the sentence are optional: a caller reading a note
// may not know the platform, and a run that ended with nothing to say
// should not print a trailing colon.
func TestTerminalRefusalSentenceDegrades(t *testing.T) {
	assert.Equal(t, "verification of jq could not answer", (&ErroredError{Port: "jq"}).Error())
	assert.Equal(t, "verification of jq on Sonoma could not answer",
		(&ErroredError{Port: "jq", Platform: "Sonoma"}).Error())
	assert.Equal(t, "verification of jq could not answer: the agent never answered",
		(&ErroredError{Port: "jq", Detail: "the agent never answered"}).Error())
}
