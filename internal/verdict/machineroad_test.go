package verdict

// The machine road's rulings, one test per ruling, named for it.
//
// Every test here asserts a REFUSAL or a wait. There is deliberately no
// test that an unattended publication succeeds: on this build it cannot,
// the gate above this judgment refuses first, and a test that asserted
// the happy path would be pinning behaviour no binary has.

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/exitcode"
	"github.com/herbygillot/dockhand/internal/record"
)

// machineAsk is the same publication put by the machine, over a verdict
// set, with the phase spelled at every call so no test leans on a zero
// value the production road never passes.
func machineAsk(r record.Record, phase Phase) PublishAsk {
	return PublishAsk{Record: r, Promotable: r.Promotable(),
		Branch: "dockhand/jq", Tip: "abc1234", By: record.Machine, Phase: phase}
}

// THE RULING: a machine publishes on positive evidence only.
//
// This is the table. Every state the human road merely complains about
// is a refusal here, and the refusal is in the refused band at the
// machine gate — the code reserved for "a policy refused the machine
// where a person would have been allowed".
func TestAMachinePublishesOnPositiveEvidenceOnly(t *testing.T) {
	cases := []struct {
		name   string
		states map[string]record.RunState
		code   int
		reason string
	}{
		{"nothing recorded at all", nil, exitcode.MachineGate, "no-positive-evidence"},
		{"a platform the port declines", map[string]record.RunState{"Sequoia": record.Unsupported},
			exitcode.MachineGate, "no-positive-evidence"},
		{"a blocked neighbour", map[string]record.RunState{"Sequoia": record.Blocked},
			exitcode.MachineGate, "no-positive-evidence"},
		{"a canceled run", map[string]record.RunState{"Sequoia": record.Canceled},
			exitcode.MachineGate, "no-positive-evidence"},
		{"an errored environment", map[string]record.RunState{"Sequoia": record.Errored},
			exitcode.MachineGate, "no-positive-evidence"},
		{"a superseded run", map[string]record.RunState{"Sequoia": record.Superseded},
			exitcode.MachineGate, "no-positive-evidence"},
		// Negative evidence keeps its own band: the failing run's verdict
		// is what is being enforced, machine or not.
		{"a failed build", map[string]record.RunState{"Sequoia": record.Failed},
			exitcode.VerifyFailed, "verification-failed"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := DecidePublish(machineAsk(set(tc.states), PhaseInFlight))
			require.Error(t, d.Refusal, "the machine must not publish this")
			assert.False(t, d.SayUnverified, "the machine refuses; it does not complain and proceed")
			assert.False(t, d.NoOp, "a refusal is not nothing to do")

			var coder exitcode.Coder
			require.ErrorAs(t, d.Refusal, &coder)
			assert.Equal(t, tc.code, coder.DockhandExit())
			var namer exitcode.Reasoner
			require.ErrorAs(t, d.Refusal, &namer)
			assert.Equal(t, tc.reason, namer.Code())
		})
	}

	// The same sets on the human road: every one of them publishes, with
	// a complaint, except the failure. That contrast IS the ruling — the
	// machine road is not stricter by accident, it is stricter because
	// nobody is reading the complaint.
	for _, tc := range cases {
		if tc.code == exitcode.VerifyFailed {
			continue
		}
		t.Run("a person publishes "+tc.name, func(t *testing.T) {
			d := DecidePublish(PublishAsk{Record: set(tc.states), Branch: "dockhand/jq", Tip: "abc1234"})
			require.NoError(t, d.Refusal)
			assert.True(t, d.SayUnverified)
		})
	}
}

// THE RULING: --no-verify is a person's override and is not honoured on
// the machine road.
//
// A failed build refuses the machine whatever the flags say. There is no
// way to spell "publish it anyway" from an unattended pass, because
// there is nobody to mean it.
func TestNoVerifyIsUnreachableFromTheMachineRoad(t *testing.T) {
	failed := set(map[string]record.RunState{"Sequoia": record.Failed})

	ask := machineAsk(failed, PhaseInFlight)
	ask.NoVerify = true
	d := DecidePublish(ask)
	require.Error(t, d.Refusal, "the machine may not talk its way past a failure")
	var refusal *FailedVerificationError
	require.ErrorAs(t, d.Refusal, &refusal)

	// A person setting the same flag over the same record publishes.
	human := DecidePublish(PublishAsk{Record: failed, Branch: "dockhand/jq", Tip: "abc1234", NoVerify: true})
	require.NoError(t, human.Refusal)
}

// THE RULING: a run still going is PENDING, not refused.
//
// The band matters more than the words. An unattended pass runs on a
// timer over a namespace where something is nearly always building; a
// caller that read "waiting" out of the refused band would page somebody
// every ten minutes about work proceeding exactly as designed.
func TestAnUnfinishedRunIsPendingAndNotARefusal(t *testing.T) {
	for _, state := range []record.RunState{record.Queued, record.Submitting, record.Running} {
		t.Run(string(state), func(t *testing.T) {
			d := DecidePublish(machineAsk(set(map[string]record.RunState{"Sequoia": state}), PhaseInFlight))
			require.Error(t, d.Refusal)

			var pending *PromotionPendingError
			require.ErrorAs(t, d.Refusal, &pending)
			assert.Equal(t, exitcode.PromotionPending, pending.DockhandExit())
			assert.Equal(t, "pending", exitcode.Family(pending.DockhandExit()),
				"nobody's problem yet is its own band, and this is what it is for")
			assert.Equal(t, []string{"Sequoia"}, pending.Platforms,
				"which build is still going is something a reader can look up")
		})
	}

	// A pass beside a run still going is still a pass: the tally is what
	// Promotable answers, and one unfinished platform does not withdraw
	// the evidence another one produced.
	both := set(map[string]record.RunState{"Sequoia": record.Passed, "Sonoma": record.Running})
	require.True(t, both.Promotable())
	require.NoError(t, DecidePublish(machineAsk(both, PhaseInFlight)).Refusal)

	// A failure outranks a pending run: the negative evidence is the
	// answer, and waiting for the rest of it would be waiting to hear the
	// same no twice.
	failing := set(map[string]record.RunState{"Sequoia": record.Failed, "Sonoma": record.Running})
	var failed *FailedVerificationError
	require.ErrorAs(t, DecidePublish(machineAsk(failing, PhaseInFlight)).Refusal, &failed)
}

// THE RULING: a machine acts on an in-flight change only, and a
// published one is a NO-OP rather than a refusal.
//
// This is what makes the unattended pass idempotent. A pass that exited
// non-zero over work it had already finished would report failure on
// every run after the successful one.
func TestAPublishedChangeIsNothingToDoAndNotAnError(t *testing.T) {
	passed := set(map[string]record.RunState{"Sequoia": record.Passed})
	unproven := set(nil)

	for _, tc := range []struct {
		name string
		pr   PRFact
		want Phase
	}{
		{"an open pull request", open, PhasePublished},
		{"a merged one", merged, PhaseRetired},
		{"one closed without merging", closed, PhaseRetired},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, PhaseOf(tc.pr))

			d := DecidePublish(machineAsk(passed, tc.want))
			assert.True(t, d.NoOp, "the change is already out; there is nothing to publish")
			require.NoError(t, d.Refusal, "and nothing is wrong")

			// The phase is asked FIRST, so a change already out is not
			// re-judged on its evidence. A machine that refused a published
			// change for being unproven would be complaining about a pull
			// request reviewers are already reading.
			d = DecidePublish(machineAsk(unproven, tc.want))
			assert.True(t, d.NoOp)
			require.NoError(t, d.Refusal)
		})
	}

	// A branch with no pull request is in flight, which is the one phase
	// the machine may act on.
	assert.Equal(t, PhaseInFlight, PhaseOf(PRFact{}))
	assert.False(t, DecidePublish(machineAsk(passed, PhaseInFlight)).NoOp)

	// A phase nobody set is not in flight. The permissive default would
	// be the dangerous one — an unset field would mean "go ahead" — so
	// the zero value reads as nothing to do.
	assert.True(t, DecidePublish(machineAsk(passed, "")).NoOp,
		"an unset phase must not read as permission to publish")
}

// THE RULING: the phase is a fact about the machine road and never about
// a person's.
//
// A person re-promoting an open pull request is refreshing it on
// purpose, which is the documented `--force` road. If the phase were
// consulted for everybody, that verb would silently do nothing.
func TestThePhaseNeverSilencesAPerson(t *testing.T) {
	passed := set(map[string]record.RunState{"Sequoia": record.Passed})
	for _, phase := range []Phase{PhaseInFlight, PhasePublished, PhaseRetired, ""} {
		d := DecidePublish(PublishAsk{Record: passed, Promotable: true,
			Branch: "dockhand/jq", Tip: "abc1234", Phase: phase})
		assert.False(t, d.NoOp, "phase %q must not silence a person's promote", phase)
		require.NoError(t, d.Refusal)
	}
}

// The zero By is a person. Every verb dockhand has is one, and a road
// that had to remember to say so would eventually forget — in the
// direction of calling a person a machine, which refuses work that
// should happen, or of calling a machine a person, which publishes work
// that should not. The safe default is the one every existing caller
// already means.
func TestTheZeroInvokerIsAPerson(t *testing.T) {
	d := DecidePublish(PublishAsk{Record: set(nil), Branch: "dockhand/jq", Tip: "abc1234"})
	require.NoError(t, d.Refusal, "the zero invoker takes the human road")
	assert.True(t, d.SayUnverified)
}

// Unfinished is what "pending" is measured with, and an unknown state
// counts as unfinished: the reading that waits is the only one that
// cannot publish on a verdict this build did not understand.
func TestUnfinishedWaitsOnAStateItCannotRead(t *testing.T) {
	r := set(map[string]record.RunState{"Sequoia": record.Passed})
	assert.Empty(t, Unfinished(r), "a terminal state is finished")

	runOn(r, "Sonoma", record.Run{State: record.RunState("teleported")})
	assert.Equal(t, []string{"Sonoma"}, Unfinished(r),
		"a word this build cannot read is not evidence, and not finished either")
}

// The dependents are best effort on the unattended road too (maintainer's
// ruling, 2026-09-04): a machine publishes a cohort whose dependent
// failed, on the same evidence a person would, and the body names the
// failure. Every fixture above is single-subject, which is why this
// shape was never tested and why a change to Promotable reached the
// machine road unnoticed — a cohort with a failed dependent is
// Promotable, and decideForMachine returns before it ever reaches the
// AnyState(Failed) check. That is now the intended reading, and this
// pins it as such rather than leaving it to be rediscovered as a bug.
func TestAMachinePublishesACohortWhoseDependentFailed(t *testing.T) {
	n := record.Record{
		Subjects: []record.Subject{{Port: "libraw"}, {Port: "gthumb"}},
		Runs: map[string]record.Run{
			record.RunKey("libraw", "Sequoia"): {State: record.Passed, Platform: "Sequoia"},
			record.RunKey("gthumb", "Sequoia"): {State: record.Failed, Platform: "Sequoia"},
		},
	}
	d := DecidePublish(PublishAsk{Record: n, Promotable: n.Promotable(),
		Branch: "dockhand/libraw-0.22.2", Tip: "abc123", By: record.Machine, Phase: PhaseInFlight})
	require.NoError(t, d.Refusal, "best effort holds for the machine: the headline passed and the dependent reached an outcome")
	assert.False(t, d.NoOp)
}

// And what best effort does not cover, on either road: a dependent
// whose run ended for a reason that says nothing about the port. The
// machine's silence and a person's cancellation are not outcomes, so
// the member is unanswered and the machine must wait or refuse.
func TestAMachineDoesNotPublishOverAnErroredOrCanceledDependent(t *testing.T) {
	for _, st := range []record.RunState{record.Errored, record.Canceled} {
		t.Run(string(st), func(t *testing.T) {
			n := record.Record{
				Subjects: []record.Subject{{Port: "libraw"}, {Port: "gthumb"}},
				Runs: map[string]record.Run{
					record.RunKey("libraw", "Sequoia"): {State: record.Passed, Platform: "Sequoia"},
					record.RunKey("gthumb", "Sequoia"): {State: st, Platform: "Sequoia"},
				},
			}
			d := DecidePublish(PublishAsk{Record: n, Promotable: n.Promotable(),
				Branch: "dockhand/libraw-0.22.2", Tip: "abc123", By: record.Machine, Phase: PhaseInFlight})
			require.Error(t, d.Refusal, "a %s dependent is not an outcome about the port", st)
		})
	}
}

// The headline is never best effort, whoever is asking.
func TestAMachineNeverPublishesAFailedHeadline(t *testing.T) {
	n := record.Record{
		Subjects: []record.Subject{{Port: "libraw"}, {Port: "gegl"}},
		Runs: map[string]record.Run{
			record.RunKey("libraw", "Sequoia"): {State: record.Failed, Platform: "Sequoia"},
			record.RunKey("gegl", "Sequoia"):   {State: record.Passed, Platform: "Sequoia"},
		},
	}
	d := DecidePublish(PublishAsk{Record: n, Promotable: n.Promotable(),
		Branch: "dockhand/libraw-0.22.2", Tip: "abc123", By: record.Machine, Phase: PhaseInFlight})
	require.Error(t, d.Refusal)
	var coder exitcode.Coder
	require.ErrorAs(t, d.Refusal, &coder)
	assert.Equal(t, exitcode.VerifyFailed, coder.DockhandExit(), "the failing run's verdict is what is enforced")
}
