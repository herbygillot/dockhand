package report

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/app"
	"github.com/herbygillot/dockhand/internal/change"
	"github.com/herbygillot/dockhand/internal/publish"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/statestore"
)

// now is this file's clock read, so an age is a constant and not a
// stopwatch: every projection here is handed a clock the caller already
// read, and the reason is exactly that a golden pinning a running
// attempt's line must not depend on when the test ran.
var now = time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)

// THE REMEDY LINE IS DERIVED, AND THE DEFECT IT REPLACES IS
// UNREPRESENTABLE. docs/todo.md records a `cycle` report telling a
// reader of stdout to run the verb they had just run; a sentence
// derived from the residency lock cannot print that, because a `cycle`
// invocation IS the thing doing the starting.
func TestTheTwoRemedySentencesAndTheOneAdmission(t *testing.T) {
	resident := Remedy(app.Residency{State: app.DispatcherResident, Holder: record.OwnerID{PID: 4821}})
	assert.Equal(t, "  dispatch (pid 4821) will start it", resident)

	none := Remedy(app.Residency{State: app.NoDispatcher})
	assert.Contains(t, none, "`dockhand dispatch` keeps them moving")
	assert.Contains(t, none, "`dockhand cycle` starts them once")

	unknown := Remedy(app.Residency{State: app.ResidencyUnknown})
	assert.Contains(t, unknown, "not established")
	assert.NotContains(t, unknown, "dockhand ", "an unestablished residency instructs nothing (rule 7)")
}

// AN UNKNOWN RESIDENCY SAYS WHAT IS UNKNOWN AND NEVER WHY. The state is
// reached two ways — a probe that failed, and `status --no-update`,
// which takes no lock at all and is Unknown by construction — and the
// value does not distinguish them. The shipped sentences asserted the
// first cause for both, so a healthy pure read announced that the
// dispatch lock could not be read when nothing had opened it.
func TestAnUnknownResidencyNeverBlamesTheLock(t *testing.T) {
	unknown := app.Residency{State: app.ResidencyUnknown}
	for _, line := range []string{Residency(unknown), Remedy(unknown)} {
		assert.NotContains(t, line, "could not be read",
			"the cause is not on the value, so no projection may name one")
		assert.NotContains(t, line, "lock",
			"nothing here establishes that a lock was so much as opened")
	}
	assert.Contains(t, Residency(unknown), "not established",
		"the fact itself is still said, because a missing line reads as an answer (rule 7)")
}

// A STAMP THAT COULD NOT BE READ IS STILL A RESIDENT DISPATCHER. "The
// lock is held" and "who holds it" are two facts, and the sentence has
// to survive holding only the first.
func TestAnUnreadableStampStillNamesAResidentDispatcher(t *testing.T) {
	line := Residency(app.Residency{State: app.DispatcherResident})
	assert.Contains(t, line, "resident")
	assert.NotContains(t, line, "pid 0")
}

// A QUEUED CHANGE ENDS ON THE REMEDY AND A STARTED ONE DOES NOT. The
// exit band says the same thing in a number — 60 against 0 — and the
// two must agree, because the whole point of deriving the sentence is
// that a script and a person are told the same story.
func TestAQueuedChangeCarriesTheRemedyAndAStartedOneDoesNot(t *testing.T) {
	var b bytes.Buffer
	Change(&b, app.Result{Did: app.Queued, Attempt: "a-91c4"}, app.Residency{State: app.NoDispatcher})
	assert.Contains(t, b.String(), "attempt a-91c4 queued")
	assert.Contains(t, b.String(), "dockhand dispatch")

	b.Reset()
	Change(&b, app.Result{Did: app.Started, Attempt: "a-91c4", Lease: "7f2a1234ff"}, app.Residency{State: app.NoDispatcher})
	assert.Contains(t, b.String(), "attempt a-91c4 started (lease 7f2a1234)")
	assert.NotContains(t, b.String(), "dockhand dispatch",
		"a build that STARTED is waiting on nobody; the verdict arrives through status")
}

// A DEFERRAL REPLACES THE REMEDY RATHER THAN JOINING IT. A queued
// attempt with a typed Deferral is not waiting on a scheduler — it is
// waiting on a person to provision something — and printing both
// sentences would name two different next steps for one row.
func TestATypedDeferralSpeaksInsteadOfTheRemedy(t *testing.T) {
	var b bytes.Buffer
	Change(&b, app.Result{
		Did:      app.Queued,
		Attempt:  "a-1",
		Deferred: &app.Deferral{Reason: app.NoEnvironment, Detail: "no base for macOS 26"},
	}, app.Residency{State: app.NoDispatcher})
	assert.Contains(t, b.String(), "dockhand provision tart")
	assert.NotContains(t, b.String(), "dockhand dispatch")
}

// A MINT WITH NO PROVIDER EXITS ZERO AND SAYS SO. The branch exists and
// nothing on this host can verify it, which is the contract narrowing
// rather than failing.
func TestAMintWithNoProviderSaysTheBranchIsUnverified(t *testing.T) {
	var b bytes.Buffer
	Change(&b, app.Result{
		Did:      app.Minted,
		Deferred: &app.Deferral{Reason: app.NoProvider, Detail: "unverified; install tart and `dockhand verify`"},
	}, app.Residency{State: app.ResidencyUnknown})
	assert.Contains(t, b.String(), "unverified")
}

// THE ATTENTION ORDER IS IMPOSED ON THE LISTING AND NEVER ON THE VALUE.
// Refname order is alphabetical order of a slug nobody chose for
// reading, so the one branch that failed sits wherever its port name
// puts it; a fleet's report is scanned for the handful of changes that
// want a person.
func TestTheAttentionOrderPutsWhatNeedsAPersonFirst(t *testing.T) {
	st := statestore.State{
		Changes:  map[string]record.Change{},
		Attempts: map[string]record.Attempt{},
	}
	add := func(id, branch string, state record.RunState) {
		st.Changes[id] = record.Change{
			ID: record.ChangeID(id), Branch: branch, Tip: id + "-tip", State: record.ChangeMinted,
		}
		st.Attempts[id+"-a"] = record.Attempt{
			ID: id + "-a", Change: record.ChangeID(id), Sha: id + "-tip",
			Phase: record.Finished, Started: now.Add(-time.Hour),
			Runs: map[string]record.Run{"p": {State: state}},
		}
	}
	// Alphabetically the passing one comes first; by attention it must not.
	add("chg-a", "dockhand/aaa-1.0", record.Passed)
	add("chg-z", "dockhand/zzz-1.0", record.Failed)

	var b bytes.Buffer
	Standings(&b, app.StatusResult{State: st, Residency: app.Residency{State: app.NoDispatcher}}, now)
	lines := nonEmpty(strings.Split(b.String(), "\n"))
	require.GreaterOrEqual(t, len(lines), 3)
	assert.Contains(t, lines[1], "zzz", "the FAILED change is above the passing one: %q", b.String())
	assert.Contains(t, lines[2], "aaa")
	assert.Contains(t, lines[1], "FAILED")
}

// --no-update SAYS SO ON ITS FIRST LINE rather than letting a missing
// line read as an answer: a report with no PR standings looks exactly
// like a report of no pull requests, and a reader must not have to guess
// which one they are holding.
func TestTheNoUpdateDepthSaysWhatItDidNotAsk(t *testing.T) {
	var b bytes.Buffer
	Standings(&b, app.StatusResult{
		State:     statestore.State{},
		Residency: app.Residency{State: app.ResidencyUnknown},
		NoUpdate:  true,
	}, now)
	first := nonEmpty(strings.Split(b.String(), "\n"))[0]
	assert.Contains(t, first, "nothing was polled")
	assert.Contains(t, first, "no forge was asked")
}

// AND IT SAYS IT ONLY WHEN IT WAS ASKED FOR. A default `status` whose
// lock probe failed carries the same unknown residency and settles
// nothing, and the shipped condition — unknown plus an empty Settled —
// claimed nothing had been polled over a report that had polled the
// provider and read the forge cache.
func TestADefaultStatusNeverClaimsItAskedNothing(t *testing.T) {
	var b bytes.Buffer
	Standings(&b, app.StatusResult{
		State:     statestore.State{},
		Residency: app.Residency{State: app.ResidencyUnknown}, // the probe failed
	}, now)
	assert.NotContains(t, b.String(), "nothing was polled",
		"the depth is on the result; it is never inferred from what came back empty")
}

// A DISAGREEING REF IS SHOWN AND NEVER SKIPPED, with the two remedies
// its two halves earn: a branch that MOVED is followed with `verify`,
// and one that is GONE is ended with `discard`. A change missing from
// the listing would read as nothing to report (rule 7).
func TestADisagreeingRefIsShownWithTheRemedyItsHalfEarns(t *testing.T) {
	moved := &change.TipDisagreement{
		ID: "chg-01", Ref: "refs/heads/dockhand/jq-1.8.1",
		Recorded: "4a1cbe9f0d2e5", Found: "0000abc",
	}
	gone := &change.TipDisagreement{
		ID: "chg-02", Ref: "refs/heads/dockhand/foo-2.0",
		Recorded: "77c1e2b", Absent: true,
	}
	assert.Contains(t, disagreement(moved), "dockhand verify dockhand/jq-1.8.1")
	assert.Contains(t, disagreement(moved), "moved by hand")
	assert.Contains(t, disagreement(gone), "dockhand discard dockhand/foo-2.0")
	assert.NotContains(t, disagreement(gone), "dockhand verify",
		"a branch that is gone cannot be followed")
}

// THE MOVED HALF MUST NOT ADVERTISE `discard`, and this is the one
// assertion in the file that is about a road rather than a sentence.
// app.Discard resolves before it does anything, meets the very
// disagreement this line was rendered from, and refuses it with exit 45
// — so the shipped remedy told a person to abandon their change by
// running a command that could not work. What is offered instead is the
// pair app.Discard's own doc names: `verify` follows the commit, and git
// puts the ref back at the tip the record holds, which is what makes
// every other verb resolve again.
func TestTheMovedRemedyOffersOnlyRoadsThatRun(t *testing.T) {
	moved := &change.TipDisagreement{
		ID: "chg-01", Ref: "refs/heads/dockhand/jq-1.8.1", Recorded: "4a1cbe9f0d2e5",
	}
	line := disagreement(moved)
	assert.NotContains(t, line, "discard",
		"discard refuses a moved tip (exit 45); advertising it is a remedy that cannot run")
	assert.Contains(t, line, "git branch -f dockhand/jq-1.8.1 4a1cbe9f",
		"the recorded tip is printed, because a remedy a reader must look something up for is one they will get wrong")
}

// A PIN IS NOT A BRANCH. A branchless snapshot lives on
// refs/dockhand/verify/<id>, which `git branch -f` would not move — it
// would make a branch called refs/heads/refs/dockhand/... — so the
// restore for one is `git update-ref`, and the verb takes the change id
// rather than a branch name it has not got.
func TestAMovedPinIsRestoredWithUpdateRefAndNamedByItsID(t *testing.T) {
	pin := &change.TipDisagreement{
		ID: "chg-77", Ref: change.PinRef("chg-77"), Recorded: "b19f0c4d",
	}
	line := disagreement(pin)
	assert.Contains(t, line, "git update-ref refs/dockhand/verify/chg-77 b19f0c4d")
	assert.Contains(t, line, "dockhand verify chg-77")
	assert.NotContains(t, line, "git branch -f")
}

// A PASS SAYS ITS COUNTS FIRST AND THE ROWS THAT WANT A PERSON AFTER: a
// pass is N outcomes, most of them uneventful, and a reader scanning for
// the handful addressed to them should not have to scroll past thirty
// that are not.
func TestAPassLeadsWithCountsAndNamesWhatNeedsAPerson(t *testing.T) {
	var b bytes.Buffer
	Pass(&b, app.Pass{
		Changes: map[record.ChangeID]app.Result{
			"chg-1": {Did: app.Started},
			"chg-2": {Did: app.Stood, Verdict: record.Passed},
		},
		Refusals: []app.Refusal{{Change: "chg-9", Err: assertErr{"held"}}},
	}, false)
	lines := nonEmpty(strings.Split(b.String(), "\n"))
	assert.Contains(t, lines[0], "1 settled")
	assert.Contains(t, lines[0], "1 started")
	assert.Contains(t, b.String(), "chg-9 needs you: held")
}

// A DRY RUN IS NOT A READ-ONLY PASS AND THE REPORT MUST NOT CALL IT
// ONE: it still observes, judges and settles, because settling is how it
// learns what it would do.
func TestADryRunRefusesToCallItselfReadOnly(t *testing.T) {
	var b bytes.Buffer
	Pass(&b, app.Pass{Changes: map[record.ChangeID]app.Result{}}, true)
	assert.Contains(t, b.String(), "NOT a read-only pass")
}

// THE ALLOWANCE IS REPORTED OVER THE PACE'S WINDOW AND NOT THE STORE'S.
// publish.MaxWindow is 24h — the floor compaction keeps machine rows
// above, and the span Gather collects stamps over because Gather is
// handed no Pace — and nothing is ever measured against it. The line
// used to count over it, so a machine with two publications in the six
// hours that decide the next refusal was reported as nine in a day, and
// the number a reader compared against --publish-max was not the number
// publish.Authorize would compute.
func TestTheAllowanceIsCountedOverThePaceWindow(t *testing.T) {
	inside := now.Add(-2 * time.Hour)
	outside := now.Add(-9 * time.Hour) // inside MaxWindow, outside the pace
	state := statestore.State{At: "9f1c4ae2b7", Publications: map[string]record.Publication{
		"pub-1": {ID: "pub-1", By: record.Machine, Steps: []record.Step{
			{Kind: record.OpenPR, Phase: record.Finished, At: inside},
		}},
		"pub-2": {ID: "pub-2", By: record.Machine, Steps: []record.Step{
			{Kind: record.OpenPR, Phase: record.Finished, At: outside},
		}},
	}}
	spend := publish.Spent(state, now)
	require.Equal(t, 2, spend.Within(publish.MaxWindow, now), "both are inside the store's floor")

	line := allowance(spend, now)
	assert.Contains(t, line, "1 of 20 in the last 6h",
		"the window is publish.DefaultPace's, and the cap it is measured against is printed beside it")
	assert.Contains(t, line, "the default allowance",
		"a dispatcher's own --publish-every lives in the process holding the lock; this line may not claim to know it")
	assert.Contains(t, line, "9f1c4ae2", "the state commit the count was taken from")
	assert.NotContains(t, line, "24h")
}

// nonEmpty drops blank lines so a test can index the lines that carry
// something.
func nonEmpty(lines []string) []string {
	var out []string
	for _, l := range lines {
		if strings.TrimSpace(l) != "" {
			out = append(out, l)
		}
	}
	return out
}

// assertErr is a refusal with a fixed sentence, so a row's words are the
// test's and not an error type's.
type assertErr struct{ s string }

func (e assertErr) Error() string { return e.s }
