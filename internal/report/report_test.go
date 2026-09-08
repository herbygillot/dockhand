package report

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/app"
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
	assert.Contains(t, unknown, "could not be read")
	assert.NotContains(t, unknown, "dockhand ", "an unreadable lock instructs nothing (rule 7)")
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
	}, now)
	first := nonEmpty(strings.Split(b.String(), "\n"))[0]
	assert.Contains(t, first, "nothing was polled")
	assert.Contains(t, first, "no forge was asked")
}

// A DISAGREEING REF IS SHOWN AND NEVER SKIPPED, with the two remedies
// its two halves earn: a branch that MOVED is followed with `verify`,
// and one that is GONE is ended with `discard`. A change missing from
// the listing would read as nothing to report (rule 7).
func TestADisagreeingRefIsShownWithTheRemedyItsHalfEarns(t *testing.T) {
	assert.Contains(t, disagreement(false), "dockhand verify")
	assert.Contains(t, disagreement(false), "moved by hand")
	assert.Contains(t, disagreement(true), "dockhand discard")
	assert.NotContains(t, disagreement(true), "dockhand verify",
		"a branch that is gone cannot be followed")
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
