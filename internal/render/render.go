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
// RUN, in the record's stable order.
//
// Per run and not per platform, which is what the two-map split makes
// different: a platform is one guest and a run is what one subject
// concluded inside it, so a cohort has more lines than it has
// environments. A cohort's lines name their subject; a single change's
// do not, because the branch on the line above is already its name.
//
// now is the caller's clock read. A running run's line states how long
// it has been going, and reading the clock in here would make the one
// rendering a golden pins depend on when the test ran.
func RecordLines(n record.Record, now time.Time) []string {
	named := verdict.Names(n)
	refs := verdict.Runs(n)
	names := namesTheGuest(refs)
	var lines []string
	for i, ref := range refs {
		// The wire word is the line's own text until a running run
		// replaces it with its elapsed time. A running run whose
		// platform names no job is a mangled record — a submission
		// writes both in one write — so it keeps the bare word rather
		// than reporting a build that has been going since year one.
		s := string(ref.Run.State)
		if ref.Run.State == record.Running && ref.Submitted {
			s = fmt.Sprintf("verifying (%s)", now.Sub(ref.Job.Job.Started).Round(time.Second))
		}
		line := fmt.Sprintf("%s (%s)", s, ref.Platform)
		if named {
			line = ref.Port + ": " + line
		}
		if names[i] {
			line += " — environment kept: " + ref.Job.Handle
		}
		if ref.Run.Detail != "" {
			line += " — " + ref.Run.Detail
		}
		lines = append(lines, line)
	}
	if len(lines) == 0 {
		return []string{unrun(n)}
	}
	return lines
}

// namesTheGuest picks the lines that name a still-standing
// environment, by their index among the runs.
//
// One line per platform, because there is one guest per platform: a
// cohort that printed the name beside each of nine verdicts would be
// describing nine environments that do not exist. And the failure's
// line, because a failure is the only thing that keeps one — putting
// the handle on a neighbour's pass would offer a reader an environment
// as the outcome of a build that did not keep it.
//
// Both halves of the guest's answer are read. A handle outlives the
// release that gave it back — it is what a person deletes by hand when
// the provider refused — so a note naming one is not by itself an
// environment anybody can still enter.
func namesTheGuest(refs []verdict.RunRef) map[int]bool {
	chosen := map[string]int{}
	for i, ref := range refs {
		if ref.Job.Handle == "" || ref.Job.Released {
			continue
		}
		was, seen := chosen[ref.Platform]
		if !seen || (refs[was].Run.State != record.Failed && ref.Run.State == record.Failed) {
			chosen[ref.Platform] = i
		}
	}
	out := make(map[int]bool, len(chosen))
	for _, i := range chosen {
		out[i] = true
	}
	return out
}

// unrun is the standing of a record that exists and holds no verdict.
//
// Schema 3 bears the record at mint, so this is now an ordinary shape
// rather than the impossible one it used to be: a branch minted with
// --no-verify has a note from the moment it exists and will never have
// a run, because nobody asked for one and the drain steps over it. That
// is a different fact from a change whose verdict simply has not
// started, and a reader who cannot tell them apart will wait all
// afternoon for a build nothing is going to run.
func unrun(n record.Record) string {
	if n.Destination == record.ToBranch {
		return "unverified — minted with --no-verify; `dockhand verify` starts a run"
	}
	return "unverified"
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
//
// Nil is now a narrower thing than it was. A minted branch bears its
// record straight away, so a tip with no note is one this build did not
// mint or one whose note a peer removed — and a change that was minted
// and never verified reaches RecordLines with no runs instead, which
// says so in its own words rather than through a drift finding about a
// history it has none of.
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
