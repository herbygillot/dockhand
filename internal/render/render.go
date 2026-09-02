// Package render is where dockhand's published words are composed: the
// pull request body upstream reads, the branch standings status prints,
// and the plan summary an intent shows before it acts. It is the other
// half of the split verdict began — a judgment states what happened, a
// renderer chooses the sentence — and gathering the sentences in one
// package is what makes them reviewable, because every function here is
// values in and a string out, so a phrase can be pinned by a golden
// instead of by a run.
//
// Nothing here looks at anything. Elapsed time arrives as a clock the
// caller already read, a settled record arrives already settled, and a
// drift finding arrives already found. That is not tidiness: these
// bytes are what dockhand tells a reviewer, and a renderer that could
// poll a worker or open a repository would put them back behind I/O,
// where they can only be checked by reproducing the world that
// produced them. .golangci.yml names the edges that would end it.
//
// The promise is about this package's own code, not its import
// closure: render imports plan in order to print one, and plan's
// dependencies reach the Tcl shell. Nothing in these files calls any
// of it.
package render

import (
	"fmt"
	"time"

	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/verdict"
)

// BranchLine is how a branch and its standing share one line: the name
// padded into a fixed column, then a single space, then the standing.
// status and clean both list branches and a reader scans the two
// listings as one, so the column is a single number rather than two
// that happen to agree today.
//
// Thirty-two is measured, not chosen: in status_human.golden
// `dockhand/jq-amended` is nineteen characters followed by fourteen
// spaces, and `dockhand/jq-passed` eighteen followed by fifteen — name
// and gap making thirty-three in every line, which is a pad to
// thirty-two plus the separator. The goldens carry a two-space harness
// indent as well, which is the harness's and not part of this.
const BranchLine = "%-32s %s\n"

// LogTail and FailureTail are how much of a build log the two verbs
// that print one show: the last few thousand bytes, where the error is,
// rather than the configure noise above it. LogTail is what `debug log`
// shows of a console it cannot parse; FailureTail is what verify prints
// under FAILED.
//
// No golden pins either number. Every log fixture in the tree is
// smaller than both cuts, so no test ever takes a truncating branch,
// and these are the two lengths the two verbs already printed rather
// than measured ones. They are named separately because they disagree,
// and collapsing them moves one verb's bytes on a path nothing pins —
// a change to what dockhand prints, which is a decision and not a
// property of a constant. Naming both is what makes the disagreement
// visible enough to rule on.
const (
	LogTail     = 4000
	FailureTail = 2000
)

// RecordLines is the human rendering of a verdict set: one line per
// platform, in the record's stable order.
//
// now is the caller's clock read. A running run's line states how long
// it has been going, and reading the clock in here would make the one
// rendering a golden pins depend on when the test ran.
func RecordLines(n record.Record, now time.Time) []string {
	var lines []string
	for _, plat := range n.Platforms() {
		r := n.Runs[plat]
		// The wire word is the line's own text until a running run
		// replaces it with its elapsed time.
		s := string(r.State)
		if r.State == record.Running {
			s = fmt.Sprintf("verifying (%s)", now.Sub(r.Job.Started).Round(time.Second))
		}
		line := fmt.Sprintf("%s (%s)", s, plat)
		if r.Handle != "" {
			line += " — environment kept: " + r.Handle
		}
		if r.Detail != "" {
			line += " — " + r.Detail
		}
		lines = append(lines, line)
	}
	if len(lines) == 0 {
		return []string{"no runs recorded"}
	}
	return lines
}

// DescribeChange is a branch's standing in lines: the settled record's
// verdicts, or — for a tip no record covers — the drift finding stated
// on its own.
//
// Both halves of that choice arrive as values, because everything
// behind them is I/O: the tip's rev-parse, the note read, the polling
// that settles a still-running run before it can be described, and the
// history walk a drift finding needs. The caller does all of it and
// hands over what it learned; nil means no record covers the tip, which
// is when drift is the whole line.
func DescribeChange(n *record.Record, drift string, now time.Time) []string {
	if n == nil {
		return []string{drift}
	}
	return RecordLines(*n, now)
}

// SummarizeRecord compresses a verdict set to one clause — "passed
// (Sequoia), failed (Sonoma)" — in the record's own stable order.
//
// The words are verdict's. A drift finding cannot be stated without
// them and verdict must stay pure to state it, so they are produced
// there and re-exported here, where a verb looks for the wording it
// prints. The clause has one implementation either way; this is the
// name it answers to on this side of the line.
func SummarizeRecord(r record.Record) string { return verdict.Summarize(r) }
