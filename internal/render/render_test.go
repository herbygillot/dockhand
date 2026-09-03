package render

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/verify"
)

// The branch column is what status and clean line their listings up
// with, so the constant is checked against a name the S0 goldens
// actually carry: `dockhand/jq-amended` there is followed by fourteen
// spaces, nineteen and fourteen making the thirty-three that a pad to
// thirty-two plus one separator produces.
func TestBranchLinePadsToTheMeasuredColumn(t *testing.T) {
	line := fmt.Sprintf(BranchLine, "dockhand/jq-amended", "passed (Testos)")
	assert.Equal(t, "dockhand/jq-amended"+strings.Repeat(" ", 14)+"passed (Testos)\n", line)
	assert.Equal(t, 33, strings.Index(line, "passed"))

	// A name past the column is not truncated; it takes its one space
	// and the standing follows, which is what keeps a long branch
	// readable rather than clipped.
	long := "dockhand/a-very-long-branch-name-past-the-column"
	assert.Equal(t, long+" standing\n", fmt.Sprintf(BranchLine, long, "standing"))
}

func TestRecordLinesStatesOneRunPerLine(t *testing.T) {
	n := templateNote(map[string]record.Run{
		"Sonoma":  {State: record.Passed},
		"Sequoia": {State: record.Unsupported, Detail: "declares known_fail on Sequoia"},
	}, nil)
	assert.Equal(t, []string{
		"unsupported (Sequoia) — declares known_fail on Sequoia",
		"passed (Sonoma)",
	}, RecordLines(n, time.Now()))
}

// A cohort has more verdicts than it has environments, so its lines
// name the member each one is about — and the kept environment is
// named once, because there is one of it however many members failed
// inside it.
func TestRecordLinesNameEachSubjectOfACohortAndTheGuestOnce(t *testing.T) {
	n := record.Record{
		Subjects: []record.Subject{{Port: "libwidget"}, {Port: "widget-tools"}},
		Jobs: map[string]record.JobRecord{
			"Testos": {Job: verify.Job{ID: "fake-1"}, Handle: "dockhand-worker-failed"},
		},
		Runs: map[string]record.Run{
			record.RunKey("libwidget", "Testos"): {State: record.Failed, Platform: "Testos",
				Detail: "Failed to build libwidget"},
			record.RunKey("widget-tools", "Testos"): {State: record.Blocked, Platform: "Testos",
				Detail: "libwidget failed first"},
		},
	}
	assert.Equal(t, []string{
		"libwidget: failed (Testos) — environment kept: dockhand-worker-failed — Failed to build libwidget",
		"widget-tools: blocked (Testos) — libwidget failed first",
	}, RecordLines(n, time.Now()))
}

// And it is the FAILURE that names it, wherever the failure sits in
// build order. Only a failure keeps an environment, so hanging the
// handle on a neighbour's pass would offer the reader a VM as the
// outcome of a build that did not keep one.
func TestRecordLinesNameTheGuestOnTheFailureThatKeptIt(t *testing.T) {
	n := record.Record{
		Subjects: []record.Subject{{Port: "libwidget"}, {Port: "widget-tools"}},
		Jobs: map[string]record.JobRecord{
			"Testos": {Job: verify.Job{ID: "fake-1"}, Handle: "dockhand-worker-failed"},
		},
		Runs: map[string]record.Run{
			record.RunKey("libwidget", "Testos"):    {State: record.Passed, Platform: "Testos"},
			record.RunKey("widget-tools", "Testos"): {State: record.Failed, Platform: "Testos"},
		},
	}
	assert.Equal(t, []string{
		"libwidget: passed (Testos)",
		"widget-tools: failed (Testos) — environment kept: dockhand-worker-failed",
	}, RecordLines(n, time.Now()))
}

// A failed run keeps its environment, and the line says so before it
// says why: the handle is the thing the reader acts on. The handle is
// the guest's and is read off the job.
func TestRecordLinesNamesAKeptEnvironmentThenTheDetail(t *testing.T) {
	n := templateNote(map[string]record.Run{
		"Testos": {State: record.Failed, Detail: "Failed to build jq"},
	}, map[string]guest{"Testos": {Handle: "dockhand-worker-failed"}})
	assert.Equal(t,
		[]string{"failed (Testos) — environment kept: dockhand-worker-failed — Failed to build jq"},
		RecordLines(n, time.Now()))
}

// A handle outlives the release that gave it back: it names what a
// person deletes by hand when the provider refused. So the flag and
// not the name is what says an environment is still there to enter.
func TestRecordLinesDoNotOfferAReleasedEnvironment(t *testing.T) {
	n := templateNote(map[string]record.Run{
		"Testos": {State: record.Failed, Detail: "Failed to build jq"},
	}, map[string]guest{"Testos": {Handle: "dockhand-worker-failed", Released: true}})
	assert.Equal(t, []string{"failed (Testos) — Failed to build jq"}, RecordLines(n, time.Now()))
}

// The elapsed time comes off the caller's clock, so the line is a
// function of its inputs and not of when the test ran.
func TestRecordLinesTimesARunningRunFromTheGivenClock(t *testing.T) {
	now := time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)
	n := templateNote(map[string]record.Run{"Testos": {State: record.Running}},
		map[string]guest{"Testos": {Started: now.Add(-90*time.Second - 400*time.Millisecond)}})
	assert.Equal(t, []string{"verifying (1m30s) (Testos)"}, RecordLines(n, now))
}

// The clock is read against the guest's start, so a running run whose
// platform names no job keeps the bare wire word. A submission writes
// both halves in one write, so such a record is mangled — and the
// reading that costs least is the one that does not report a build as
// having been going since year one.
func TestRecordLinesDoNotTimeARunWithNoGuest(t *testing.T) {
	n := record.Record{
		Subjects: []record.Subject{{Port: "jq"}},
		Runs: map[string]record.Run{
			record.RunKey("jq", "Testos"): {State: record.Running, Platform: "Testos"},
		},
	}
	assert.Equal(t, []string{"running (Testos)"}, RecordLines(n, time.Now()))
}

// The record is born at mint, so a change with no runs is now an
// ordinary shape rather than an impossible one — and the two ways to
// reach it are different facts. A branch nobody asked a verdict of will
// never gain a run, because the drain steps over it; a change whose
// verdict has not started yet will.
func TestRecordLinesSayWhichKindOfUnverifiedThisIs(t *testing.T) {
	assert.Equal(t, []string{"unverified"}, RecordLines(record.Record{}, time.Now()))
	assert.Equal(t, []string{"unverified"},
		RecordLines(record.Record{Destination: record.ToVerdict}, time.Now()))
	assert.Equal(t,
		[]string{"unverified — minted with --no-verify; `dockhand verify` starts a run"},
		RecordLines(record.Record{Destination: record.ToBranch}, time.Now()))
}

// A tip no record covers is described by the drift finding alone; a
// noted one by its verdicts. Both arrive as values, because reaching
// either takes a repository.
func TestDescribeChangeFallsBackToTheDriftSentence(t *testing.T) {
	assert.Equal(t,
		[]string{"tip unverified; passed (Testos) at 44cfc9eea250, 1 commit(s) behind"},
		DescribeChange(nil, "tip unverified; passed (Testos) at 44cfc9eea250, 1 commit(s) behind", time.Now()))

	n := templateNote(map[string]record.Run{"Testos": {State: record.Passed}}, nil)
	assert.Equal(t, []string{"passed (Testos)"}, DescribeChange(&n, "ignored", time.Now()))
}

func TestSummarizeRecordCompressesTheSetToOneClause(t *testing.T) {
	n := templateNote(map[string]record.Run{
		"Sequoia": {State: record.Passed},
		"Sonoma":  {State: record.Failed},
	}, nil)
	assert.Equal(t, "passed (Sequoia), failed (Sonoma)", SummarizeRecord(n))
}
