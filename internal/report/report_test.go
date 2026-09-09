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
	assert.Contains(t, Residency(unknown), "not known",
		"the fact itself is still said, because a missing line reads as an answer (rule 7)")

	// AND IT MUST NOT READ AS THE NEGATIVE CASE. "not established" said
	// what is unknown without blaming the lock — which is what this test
	// was written for — but still parsed as a finding of absence, one
	// line away from the genuine negative below. A resident dispatcher
	// ran through a whole field exercise while status --no-update printed
	// it, and it was read five times as "there is no dispatcher".
	assert.NotEqual(t, Residency(unknown), Residency(app.Residency{State: app.NoDispatcher}))
	for _, word := range []string{"no dispatcher", "not established"} {
		assert.NotContains(t, Residency(unknown), word,
			"an unknown residency may not borrow the negative case's words")
	}
}

// THE RESIDENT LINE PARENTHESISES ONCE. who() already brackets what it
// knows, and the caller used to wrap it again: "resident ((pid 1 on h),
// since 13:04)".
func TestTheResidentLineHasNoDoubledParens(t *testing.T) {
	line := Residency(app.Residency{
		State:  app.DispatcherResident,
		Holder: record.OwnerID{PID: 4821, Host: "mac"},
		Since:  time.Date(2026, 9, 8, 13, 31, 0, 0, time.UTC),
	})
	assert.NotContains(t, line, "((")
	assert.Contains(t, line, "pid 4821 on mac")
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
	Change(&b, app.Result{Did: app.Queued, Attempt: "a-91c4"}, app.Residency{State: app.NoDispatcher}, Created)
	assert.Contains(t, b.String(), "attempt a-91c4 queued")
	assert.Contains(t, b.String(), "dockhand dispatch")

	b.Reset()
	Change(&b, app.Result{Did: app.Started, Attempt: "a-91c4", Lease: "7f2a1234ff"}, app.Residency{State: app.NoDispatcher}, Created)
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
	}, app.Residency{State: app.NoDispatcher}, Created)
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
	}, app.Residency{State: app.ResidencyUnknown}, Created)
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
	})
	lines := nonEmpty(strings.Split(b.String(), "\n"))
	assert.Contains(t, lines[0], "1 settled")
	assert.Contains(t, lines[0], "1 started")
	assert.Contains(t, b.String(), "chg-9 needs you: held")
}

// A DRY RUN IS A SURVEY AND IS REPORTED AS ONE. It used to be an acting
// pass with a banner over it — the banner said so, honestly, because the
// pass really did settle, release and submit. It no longer does, so the
// report is the survey's own shape and the counts are of what WOULD
// happen rather than of what did.
func TestADryRunReportsWhatWouldHappenAndNotWhatDid(t *testing.T) {
	var b bytes.Buffer
	Pass(&b, app.Pass{
		Changes: map[record.ChangeID]app.Result{},
		Would: &app.Would{
			Settle: []string{"att-1"},
			Start:  []string{"att-2"},
			Retire: []record.ChangeID{"chg-1"},
		},
	})
	out := b.String()
	assert.Contains(t, out, "nothing was performed")
	assert.Contains(t, out, "1 settle")
	assert.Contains(t, out, "would poll and judge attempt att-1")
	assert.Contains(t, out, "would start attempt att-2")
	assert.Contains(t, out, "would retire chg-1")
	assert.NotContains(t, out, "0 settled", "an acting pass's counts are not a survey's")
}

// An acting pass carries no Would, and a reader can tell the two kinds
// apart from the value alone.
func TestAnActingPassIsReportedAsOne(t *testing.T) {
	var b bytes.Buffer
	Pass(&b, app.Pass{Changes: map[record.ChangeID]app.Result{}})
	assert.Contains(t, b.String(), "0 settled")
	assert.NotContains(t, b.String(), "would")
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

// AN EMPTY REPORT SAYS IT IS EMPTY, and it distinguishes the two ways of
// being empty. Every listing in Standings is a loop, so a checkout with
// nothing open printed the residency line and stopped — indistinguishable
// from a report that broke off mid-render, which is the silence rule 7
// forbids. `purge` removes the state ref by design, so a store with none
// is the ordinary thing the next command finds.
func TestAnEmptyStandingSaysSoAndNamesTheRoadOnward(t *testing.T) {
	var b bytes.Buffer
	Standings(&b, app.StatusResult{
		State:     statestore.State{}, // no At: statestore.ReadOrEmpty's answer
		Residency: app.Residency{State: app.NoDispatcher},
	}, now)
	assert.Contains(t, b.String(), "nothing is in flight")
	assert.Contains(t, b.String(), "dockhand bump", "a virgin checkout is told where to start")
}

// AND A STORE THAT HAS RUN IS NOT TOLD IT HAS NEVER RUN. "dockhand has
// recorded nothing here" and "everything recorded here is closed" are
// different facts, and only the first names a first step.
func TestAnEmptyStandingOverAWrittenStoreDoesNotSayItIsVirgin(t *testing.T) {
	var b bytes.Buffer
	Standings(&b, app.StatusResult{
		State:     statestore.State{At: "9f2c1ae0"},
		Residency: app.Residency{State: app.NoDispatcher},
	}, now)
	assert.Contains(t, b.String(), "nothing is in flight")
	assert.NotContains(t, b.String(), "dockhand bump")
}

// A FINDING A READER CANNOT SEE IS A FINDING THAT DID NOT HAPPEN.
//
// Every kind rendered as `"proposes: " + Criterion`, and only one kind
// fills Criterion. An instruction-comment keeps the maintainer's own
// words in Quote, so on cmark — whose Portfile says in as many words
// that any version update requires revbumping its dependents — status
// printed "proposes:" and stopped.
func TestAnInstructionCommentIsShownAndNotSwallowed(t *testing.T) {
	line := proposalLine(record.Finding{
		Kind:   record.KindInstruction,
		Source: "devel/cmark/Portfile",
		Quote:  "# Any version update requires revbumping\n# all ports that link with the library",
	})
	assert.Contains(t, line, "devel/cmark/Portfile")
	assert.Contains(t, line, "requires revbumping")
	assert.NotContains(t, line, "\n", "a status line is one line")
}

// THE CANDIDATES ARE THE ACTIONABLE HALF. A criterion says what moved;
// only the list says what to do about it, and the list was never shown.
func TestAnABIProposalNamesThePortsItProposes(t *testing.T) {
	line := proposalLine(record.Finding{
		Kind:      record.KindABIDependents,
		Criterion: "install name libcmark.0.30.3.dylib → libcmark.0.31.2.dylib",
		Candidates: []record.Candidate{
			{Port: "Aseprite"}, {Port: "nheko"}, {Port: "mkvtoolnix"},
		},
	})
	assert.Contains(t, line, "Aseprite")
	assert.Contains(t, line, "nheko")
	assert.Contains(t, line, "mkvtoolnix")
	assert.Contains(t, line, "install name", "and the criterion still rides along")
}

// A KIND THIS BUILD CANNOT READ STILL PRINTS SOMETHING, for the same
// reason: silence would be indistinguishable from no finding at all.
func TestAnEmptyFindingStillSaysItsKind(t *testing.T) {
	assert.Contains(t, proposalLine(record.Finding{Kind: "something-new"}), "something-new")
}

// A PULL REQUEST WHOSE STATE NOBODY RECORDED YET SAYS LESS, NOT NOTHING.
//
// Seconds after `promote` opens one, the facts cached on the way out
// carry a number and no state, and the line read:
//
//	PR #34573 , as of 1m ago
//
// A hole between the number and the comma is not an improvement on
// saying less. `--refresh` is what fills it in, and does.
func TestAPullRequestWithNoRecordedStateLeavesNoHole(t *testing.T) {
	now := time.Now()
	line := forgeLine("", 34573, now.Add(-time.Minute), now)
	assert.Contains(t, line, "PR #34573,")
	assert.NotContains(t, line, " ,", "no blank where a word would go")

	assert.Contains(t, forgeLine("open", 34573, now.Add(-time.Minute), now), "PR #34573 open,",
		"and a state that IS known is still said")
}

// THE BRANCH LINE SAYS WHAT THE ROAD DID, and for one road it used to
// say something false.
//
// "minted" was internal vocabulary, and `bump-revision --for` never
// mints — its own doc says so; it adds a commit to a branch that already
// stands. The word is the ROAD's to supply because app.Result cannot:
// Realization is an attempt-progress axis, and a bump that creates a
// branch and starts a build reports Started exactly like an accept that
// extends one and starts a build.
func TestTheBranchLineNamesWhatTheRoadActuallyDid(t *testing.T) {
	assert.Equal(t, "Created dockhand/jq-1.8", branchLine(Created, "dockhand/jq-1.8"))
	assert.Equal(t, "Updated dockhand/cmark-0.31.2", branchLine(Updated, "dockhand/cmark-0.31.2"))
	assert.Equal(t, "Created dockhand/jq-1.8", branchLine("", "dockhand/jq-1.8"),
		"a road that did not say is a mint, and a wrong verb is worse than a missing one")
}

// A PASSING RETRY RESOLVES THE FAILURE IT RETRIED.
//
// The standing was the WORST attempt on the tip, with no reference to
// when any of them happened, so an infrastructure error beat a later
// pass forever. Measured on repgrep 0.17.1: three attempts on one sha —
// a first run that errored, a canceled submission, and a retry that
// passed — and status went on saying "errored" while promote --body
// reported the successful verification off the same store.
func TestAPassingRetryIsTheStandingAndNotTheOlderError(t *testing.T) {
	now := time.Now()
	c := record.Change{ID: "chg-1", Tip: "aaaa"}
	atts := []record.Attempt{
		{Sha: "aaaa", Phase: record.Finished, Started: now.Add(-30 * time.Minute),
			Runs: map[string]record.Run{"repgrep": {State: record.Errored}}},
		{Sha: "aaaa", Phase: record.Finished, Started: now.Add(-5 * time.Minute),
			Runs: map[string]record.Run{"repgrep": {State: record.Passed}}},
	}
	text, _, _ := standingOf(c, atts, now)
	assert.Contains(t, text, "passed")
	assert.NotContains(t, text, "errored", "the older attempt is history, not the standing")
}

// AND A BUILD RUNNING NOW IS THE STANDING, whatever happened before it —
// the same defect in its other guise, where a change actively rebuilding
// still read as the failure it was rebuilding after.
func TestAnInFlightBuildOutranksASettledFailure(t *testing.T) {
	now := time.Now()
	c := record.Change{ID: "chg-1", Tip: "aaaa"}
	atts := []record.Attempt{
		{Sha: "aaaa", Phase: record.Finished, Started: now.Add(-30 * time.Minute),
			Runs: map[string]record.Run{"repgrep": {State: record.Failed}}},
		{Sha: "aaaa", Phase: record.Active, Lease: "lease-1", Started: now.Add(-1 * time.Minute)},
	}
	text, _, _ := standingOf(c, atts, now)
	assert.Contains(t, text, "building")
}

// A FORMER TIP'S WORK STILL SAYS NOTHING, which is the one part of the
// old rule that was never in question.
func TestWorkOnAFormerTipIsNotTheStanding(t *testing.T) {
	now := time.Now()
	c := record.Change{ID: "chg-1", Tip: "bbbb"}
	atts := []record.Attempt{{Sha: "aaaa", Phase: record.Finished, Started: now,
		Runs: map[string]record.Run{"repgrep": {State: record.Failed}}}}
	text, _, _ := standingOf(c, atts, now)
	assert.Equal(t, "no verification asked for", text)
}
