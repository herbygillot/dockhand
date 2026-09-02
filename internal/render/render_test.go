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

func TestRecordLinesStatesOnePlatformPerLine(t *testing.T) {
	n := record.Record{Runs: map[string]record.Run{
		"Sonoma":  {State: record.Passed},
		"Sequoia": {State: record.Unsupported, Detail: "declares known_fail on Sequoia"},
	}}
	assert.Equal(t, []string{
		"unsupported (Sequoia) — declares known_fail on Sequoia",
		"passed (Sonoma)",
	}, RecordLines(n, time.Now()))
}

// A failed run keeps its environment, and the line says so before it
// says why: the handle is the thing the reader acts on.
func TestRecordLinesNamesAKeptEnvironmentThenTheDetail(t *testing.T) {
	n := record.Record{Runs: map[string]record.Run{
		"Testos": {State: record.Failed, Handle: "dockhand-worker-failed", Detail: "Failed to build jq"},
	}}
	assert.Equal(t,
		[]string{"failed (Testos) — environment kept: dockhand-worker-failed — Failed to build jq"},
		RecordLines(n, time.Now()))
}

// The elapsed time comes off the caller's clock, so the line is a
// function of its inputs and not of when the test ran.
func TestRecordLinesTimesARunningRunFromTheGivenClock(t *testing.T) {
	now := time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)
	n := record.Record{Runs: map[string]record.Run{
		"Testos": {State: record.Running, Job: verify.Job{Started: now.Add(-90*time.Second - 400*time.Millisecond)}},
	}}
	assert.Equal(t, []string{"verifying (1m30s) (Testos)"}, RecordLines(n, now))
}

func TestRecordLinesSaysSoWhenTheSetIsEmpty(t *testing.T) {
	assert.Equal(t, []string{"no runs recorded"}, RecordLines(record.Record{}, time.Now()))
}

// A tip no record covers is described by the drift finding alone; a
// noted one by its verdicts. Both arrive as values, because reaching
// either takes a repository.
func TestDescribeChangeFallsBackToTheDriftSentence(t *testing.T) {
	assert.Equal(t,
		[]string{"tip unverified; passed (Testos) at 44cfc9eea250, 1 commit(s) behind"},
		DescribeChange(nil, "tip unverified; passed (Testos) at 44cfc9eea250, 1 commit(s) behind", time.Now()))

	n := record.Record{Runs: map[string]record.Run{"Testos": {State: record.Passed}}}
	assert.Equal(t, []string{"passed (Testos)"}, DescribeChange(&n, "ignored", time.Now()))
}

func TestSummarizeRecordCompressesTheSetToOneClause(t *testing.T) {
	n := record.Record{Runs: map[string]record.Run{
		"Sequoia": {State: record.Passed},
		"Sonoma":  {State: record.Failed},
	}}
	assert.Equal(t, "passed (Sequoia), failed (Sonoma)", SummarizeRecord(n))
}
