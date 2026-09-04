package render

// The ruled attention order, held to the ruling one band at a time.
//
// The goldens next door pin what the order looks like; these pin why
// each branch is where it is. Both are needed and neither substitutes:
// a golden re-recorded from a wrong sort is still a passing golden, and
// a band table that never renders proves nothing about the report.

import (
	"bytes"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/verdict"
)

// attentionClock is the pass's clock for every fixture here, so that an
// age is a constant and not a stopwatch.
var attentionClock = time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)

// openPR is a branch with an open pull request created the given
// duration before the clock.
func openPR(branch string, age time.Duration, tier Tier, n *record.Record) BranchReport {
	return BranchReport{
		Branch:      branch,
		Note:        n,
		Tier:        tier,
		PRCreatedAt: attentionClock.Add(-age),
		Retire: verdict.Reconciliation{Promoted: true,
			PR: verdict.PRFact{Found: true, Number: 77, Open: true, URL: "https://x/77"}},
	}
}

// noted builds a record in one state, which is all any band asks of one.
func noted(state record.RunState) *record.Record {
	n := templateNote(map[string]record.Run{"Testos": {State: state, Platform: "Testos"}}, nil)
	return &n
}

// attentionSample holds one branch per band, listed in the refname
// order git would have enumerated them in — so what the ordering does
// is exactly the difference between the input and the output.
func attentionSample() Report {
	held := noted(record.Passed)
	held.Hold = &record.Hold{Reason: "waiting on the ticket", At: attentionClock.Add(-time.Hour)}
	superseded := noted(record.Passed)
	superseded.SupersededBy = "dockhand/jq-1.9"
	closed := BranchReport{
		Branch: "dockhand/jq-ended-closed",
		Note:   noted(record.Passed),
		Retire: verdict.Reconciliation{Promoted: true,
			PR: verdict.PRFact{Found: true, Number: 78, URL: "https://x/78"}},
	}
	return Report{
		Repository: "/checkout/ports",
		Now:        attentionClock,
		Branches: []BranchReport{
			closed,
			{Branch: "dockhand/jq-ended-superseded", Note: superseded},
			openPR("dockhand/jq-expired-new", 80*time.Hour, TierOpenmaintainer, noted(record.Passed)),
			openPR("dockhand/jq-expired-old", 30*24*time.Hour, TierNomaintainer, noted(record.Passed)),
			{Branch: "dockhand/jq-failed", Note: noted(record.Failed)},
			{Branch: "dockhand/jq-held", Note: held},
			openPR("dockhand/jq-inwindow", 41*time.Hour, TierMaintained, noted(record.Passed)),
			{Branch: "dockhand/jq-passed", Note: noted(record.Passed)},
			{Branch: "dockhand/jq-queued", Note: noted(record.Queued)},
			{Branch: "dockhand/jq-running", Note: running("Testos", attentionClock.Add(-90*time.Second))},
		},
	}
}

func names(bs []BranchReport) []string {
	out := make([]string, len(bs))
	for i, b := range bs {
		out[i] = b.Branch
	}
	return out
}

// RULING: the band table. Failures; open PRs past the 72-hour window
// with their age; queued; passed-unpromoted; held; rejected and
// superseded as quiet end states; the rest.
func TestTheBandTableIsTheRuledOrder(t *testing.T) {
	assert.Equal(t, []string{
		"dockhand/jq-failed",
		"dockhand/jq-expired-old",
		"dockhand/jq-expired-new",
		"dockhand/jq-queued",
		"dockhand/jq-passed",
		"dockhand/jq-held",
		"dockhand/jq-ended-closed",
		"dockhand/jq-ended-superseded",
		"dockhand/jq-inwindow",
		"dockhand/jq-running",
	}, names(attentionSample().Ordered()))
}

// Each band claims what the ruling says it claims, stated one fact at a
// time so a table that drifts says which row drifted.
func TestEachBandClaimsItsOwnFact(t *testing.T) {
	promotable := noted(record.Passed)
	held := noted(record.Passed)
	held.Hold = &record.Hold{Reason: "stop", At: attentionClock}
	superseded := noted(record.Passed)
	superseded.SupersededBy = "dockhand/jq-1.9"
	heldAndQueued := noted(record.Queued)
	heldAndQueued.Hold = &record.Hold{Reason: "stop", At: attentionClock}
	replacedAndQueued := noted(record.Queued)
	replacedAndQueued.SupersededBy = "dockhand/jq-1.9"

	for _, c := range []struct {
		name string
		want band
		b    BranchReport
	}{
		{"a failed run", bandFailure, BranchReport{Note: noted(record.Failed)}},
		{"an open PR past its window", bandExpired, openPR("b", 73*time.Hour, TierUnknown, promotable)},
		{"a queued run", bandQueued, BranchReport{Note: noted(record.Queued)}},
		{"passed and never pushed", bandUnpromoted, BranchReport{Note: promotable}},
		{"held", bandHeld, BranchReport{Note: held}},
		// A queue the drain will never start. bandQueued promises the
		// reader an hour of machine time is about to be spent and a narrow
		// window to say otherwise; on a held branch the drain refuses at
		// the walk and again under the submit lock, so the run stays queued
		// and reprints itself every pass. Leading the report with the one
		// branch guaranteed not to move is the opposite of the point.
		{"held with a run still queued", bandHeld, BranchReport{Note: heldAndQueued}},
		{"superseded with a run still queued", bandEnded, BranchReport{Note: replacedAndQueued}},
		{"a PR closed without merging", bandEnded, BranchReport{Note: promotable,
			Retire: verdict.Reconciliation{Promoted: true, PR: verdict.PRFact{Found: true, Number: 1}}}},
		{"superseded by a newer sibling", bandEnded, BranchReport{Note: superseded}},
		{"an open PR inside its window", bandRest, openPR("b", time.Hour, TierUnknown, promotable)},
		{"nothing in particular", bandRest, BranchReport{Note: noted(record.Running)}},
		{"no record at all", bandRest, BranchReport{Drift: "unverified"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			assert.Equal(t, c.want, bandOf(c.b, attentionClock))
		})
	}
}

// A branch answering to several bands is claimed by the first one the
// ruling lists. The ruled sequence is a priority list, and a failing
// branch sorted by its hold rather than its failure would be sorted by
// the less urgent of two true things.
func TestTheFirstMatchingBandClaimsTheBranch(t *testing.T) {
	n := noted(record.Failed)
	n.Hold = &record.Hold{Reason: "stop", At: attentionClock}
	n.SupersededBy = "dockhand/jq-1.9"
	b := openPR("dockhand/jq-everything", 30*24*time.Hour, TierNomaintainer, n)
	assert.Equal(t, bandFailure, bandOf(b, attentionClock))
}

// The one place the claim order and the display order part company: a
// held or superseded branch that also passed and was never pushed is
// not listed as ready to promote, because the hold and the supersede
// are each an answer to exactly that question.
func TestAHoldOrASupersedeOutranksPassedUnpromoted(t *testing.T) {
	held := noted(record.Passed)
	held.Hold = &record.Hold{Reason: "waiting on the ticket", At: attentionClock}
	require.True(t, held.Promotable(), "the fixture is only interesting if both bands are true of it")
	assert.Equal(t, bandHeld, bandOf(BranchReport{Note: held}, attentionClock))

	replaced := noted(record.Passed)
	replaced.SupersededBy = "dockhand/jq-1.9"
	require.True(t, replaced.Promotable())
	assert.Equal(t, bandEnded, bandOf(BranchReport{Note: replaced}, attentionClock))

	assert.Less(t, int(bandUnpromoted), int(bandHeld),
		"claimed earlier, still listed later: the ruled sequence puts passed-unpromoted above held")
}

// The same divergence one arm higher, and the stronger case of the two.
//
// bandQueued sits above passed-unpromoted because a queue is an hour of
// the machine about to be spent and a person who wanted it stopped has a
// narrow window to say so. On a held or superseded branch that hour is
// never spent: the drain refuses both at the walk and again under the
// submit lock, so the run sits queued and reprints "the verification is
// withheld" every pass, forever. The band would be promoting the one
// branch in the report guaranteed not to move.
//
// The exception is bandExpired, which is still claimed first: that pull
// request really is waiting on reviewers whatever the local hold says,
// and StillnessLines prints the hold under it either way.
func TestAHoldOrASupersedeOutranksAQueueTheDrainWillNotStart(t *testing.T) {
	held := noted(record.Queued)
	held.Hold = &record.Hold{Reason: "waiting on the ticket", At: attentionClock}
	require.True(t, held.AnyState(record.Queued), "the fixture is only interesting if both bands are true of it")
	assert.Equal(t, bandHeld, bandOf(BranchReport{Note: held}, attentionClock))

	replaced := noted(record.Queued)
	replaced.SupersededBy = "dockhand/jq-1.9"
	assert.Equal(t, bandEnded, bandOf(BranchReport{Note: replaced}, attentionClock))

	assert.Less(t, int(bandQueued), int(bandHeld),
		"claimed earlier, still listed later, exactly as held-over-unpromoted is")

	// And the expired band keeps its place ahead of both: the wait is the
	// forge's, not this machine's.
	assert.Equal(t, bandExpired,
		bandOf(openPR("dockhand/jq-1.8", 73*time.Hour, TierUnknown, held), attentionClock))
}

// Inside the expired band the oldest pull request leads: those branches
// are in a band because of how long they have waited, so ordering them
// by name would discard the fact that put them there.
func TestTheExpiredBandIsOldestFirst(t *testing.T) {
	rep := Report{Now: attentionClock, Branches: []BranchReport{
		openPR("dockhand/aaa", 80*time.Hour, TierUnknown, nil),
		openPR("dockhand/zzz", 200*time.Hour, TierUnknown, nil),
		openPR("dockhand/mmm", 400*time.Hour, TierUnknown, nil),
	}}
	assert.Equal(t, []string{"dockhand/mmm", "dockhand/zzz", "dockhand/aaa"}, names(rep.Ordered()))
}

// Everywhere else the enumeration order survives, which is what makes
// most of a re-recorded golden's line moves explainable as "the bands
// above it left" rather than as a second, unstated sort.
func TestOutsideTheExpiredBandTheEnumerationOrderSurvives(t *testing.T) {
	rep := Report{Now: attentionClock, Branches: []BranchReport{
		{Branch: "dockhand/zzz", Note: noted(record.Running)},
		{Branch: "dockhand/aaa", Note: noted(record.Running)},
		{Branch: "dockhand/mmm", Note: noted(record.Running)},
	}}
	assert.Equal(t, []string{"dockhand/zzz", "dockhand/aaa", "dockhand/mmm"}, names(rep.Ordered()))
}

// An unknown creation time is never expired. The zero time is the year
// one, so a bare comparison would make a pull request nobody could date
// the most urgent thing in the namespace.
func TestAnUnknownCreationTimeIsNeverExpired(t *testing.T) {
	b := BranchReport{Branch: "dockhand/jq-undated", Note: noted(record.Passed),
		Retire: verdict.Reconciliation{Promoted: true,
			PR: verdict.PRFact{Found: true, Number: 77, Open: true}}}
	assert.True(t, b.PRCreatedAt.IsZero())
	assert.False(t, b.windowElapsed(attentionClock))
	assert.Equal(t, bandRest, bandOf(b, attentionClock))
	assert.Nil(t, WindowLines(b, attentionClock))
}

// RULING: the window is 72 hours for every tier. The tier decides what
// the elapsed window MEANS, never how long it is — see ReviewWindow for
// why a tier-length window is the reading that was rejected.
func TestTheWindowIsTheSameLengthForEveryTier(t *testing.T) {
	for _, tier := range []Tier{TierUnknown, TierNomaintainer, TierOpenmaintainer, TierMaintained} {
		t.Run(string(tier), func(t *testing.T) {
			assert.False(t, openPR("b", ReviewWindow-time.Minute, tier, nil).windowElapsed(attentionClock),
				"one minute short of the window is inside it whatever the tier says")
			assert.True(t, openPR("b", ReviewWindow, tier, nil).windowElapsed(attentionClock),
				"the window elapses at 72 hours whatever the tier says")
		})
	}
}

// The two forms of the window sentence, with the arithmetic pinned
// against a clock that does not move. The cmd goldens normalize this
// figure away because their pass reads the real clock; this is where
// the number itself is held.
func TestTheWindowSentenceStatesTheAge(t *testing.T) {
	inside := WindowLines(openPR("b", 41*time.Hour, TierMaintained, nil), attentionClock)
	require.Len(t, inside, 1)
	assert.Equal(t, "PR #77 — 41h into the 72-hour review window (maintained)", inside[0])

	past := WindowLines(openPR("b", 5*24*time.Hour, TierNomaintainer, nil), attentionClock)
	require.Len(t, past, 2, "past the window the draft follows the age")
	assert.Equal(t, "PR #77 — open 5d, the 72-hour review window elapsed 2d ago (nomaintainer)", past[0])

	unknown := WindowLines(openPR("b", 41*time.Hour, TierUnknown, nil), attentionClock)
	require.Len(t, unknown, 1)
	assert.Equal(t, "PR #77 — 41h into the 72-hour review window", unknown[0],
		"an unread tier is named by silence, never guessed at")
}

// A merged or closed pull request has no window left to be inside or
// past, and the branch's own line already says which it is.
func TestOnlyAnOpenPullRequestHasAWindow(t *testing.T) {
	for _, pr := range []verdict.PRFact{
		{Found: true, Number: 1, Merged: true},
		{Found: true, Number: 1},
		{},
	} {
		b := BranchReport{PRCreatedAt: attentionClock.Add(-30 * 24 * time.Hour),
			Retire: verdict.Reconciliation{Promoted: true, PR: pr}}
		assert.Nil(t, WindowLines(b, attentionClock))
	}
}

// The sort is a projection and never a mutation: `clean` renders the
// same Report through Sweep, and a listing it never asked to have
// reordered must come back in the order it was enumerated in.
func TestOrderingLeavesTheReportAlone(t *testing.T) {
	rep := attentionSample()
	before := names(rep.Branches)
	_ = rep.Ordered()
	assert.Equal(t, before, names(rep.Branches))

	var out, errb bytes.Buffer
	rep.Sweep(&out, &errb)
	var got []string
	for _, line := range bytes.Split(bytes.TrimSuffix(out.Bytes(), []byte("\n")), []byte("\n")) {
		got = append(got, string(bytes.Fields(line)[0]))
	}
	assert.Equal(t, before, got, "the sweep lists the namespace, not the attention order")
}

// A band whose members are indistinguishable from the branches around
// them is a sort with nothing to show for itself, so the two facts the
// ordering newly sorts on are the two it newly says out loud.
func TestTheStillnessBandsAreLegibleOnTheLine(t *testing.T) {
	held := noted(record.Passed)
	held.Hold = &record.Hold{Reason: "waiting on the ticket", At: attentionClock}
	assert.Equal(t, []string{"held: waiting on the ticket — `dockhand unhold dockhand/jq-1.8` releases it"},
		StillnessLines(held, "dockhand/jq-1.8"))

	quiet := noted(record.Passed)
	quiet.Hold = &record.Hold{At: attentionClock}
	assert.Equal(t, []string{"held: no reason given — `dockhand unhold dockhand/jq-1.8` releases it"},
		StillnessLines(quiet, "dockhand/jq-1.8"),
		"the pointer is the hold and the reason is optional; a held branch with nothing written on it still stops")

	replaced := noted(record.Passed)
	replaced.SupersededBy = "dockhand/jq-1.9"
	assert.Equal(t, []string{"superseded by dockhand/jq-1.9 — `dockhand clean --superseded` removes it"},
		StillnessLines(replaced, "dockhand/jq-1.8"))

	assert.Nil(t, StillnessLines(noted(record.Passed), "dockhand/jq-1.8"))
	assert.Nil(t, StillnessLines(nil, "dockhand/jq-1.8"))
}

// The superseded band names the branch-level fact — the field a newer
// sibling wrote and `clean --superseded` retires — and not the run
// state of the same name, which says something else entirely: that the
// branch moved out from under a running job. A change is quiet because
// something replaced it, not because a build lost its footing.
func TestTheSupersededBandIsTheBranchFactAndNotTheRunState(t *testing.T) {
	movedUnderTheRun := noted(record.Superseded)
	require.Empty(t, movedUnderTheRun.SupersededBy)
	assert.Equal(t, bandRest, bandOf(BranchReport{Note: movedUnderTheRun}, attentionClock))

	replaced := noted(record.Passed)
	replaced.SupersededBy = "dockhand/jq-1.9"
	assert.Equal(t, bandEnded, bandOf(BranchReport{Note: replaced}, attentionClock))
}

// The order as a reader meets it. Every other test here checks one
// rule; this one is the only place all seven bands, both window
// sentences and the follow-up draft are rendered together against a
// clock that does not move — which is what the cmd goldens cannot do,
// since their pass reads the real one.
func TestAttentionOrderRendering(t *testing.T) {
	var out, errb bytes.Buffer
	attentionSample().Text(&out, &errb)
	checkReport(t, "report_attention", &out, &errb)
}

// humanAge truncates rather than rounds: these figures are read as "at
// least this long", and a tool asking somebody to act should not
// overstate the case it is making.
func TestHumanAgeTruncates(t *testing.T) {
	for _, c := range []struct {
		d    time.Duration
		want string
	}{
		{0, "0h"},
		{59 * time.Minute, "0h"},
		{time.Hour, "1h"},
		{47*time.Hour + 59*time.Minute, "47h"},
		{48 * time.Hour, "2d"},
		{71 * time.Hour, "2d"},
		{30 * 24 * time.Hour, "30d"},
		{-time.Hour, "0h"},
	} {
		assert.Equal(t, c.want, humanAge(c.d), c.d.String())
	}
}
