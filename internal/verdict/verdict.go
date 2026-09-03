// Package verdict is what dockhand's records mean. It holds the
// judgments: whether a verdict set clears promote's gate, what a
// finished run's log says the run became, what an unnoted tip's drift
// amounts to, whether a branch is done, whether a platform is worth
// building on at all.
//
// Nothing here looks at anything. Every fact a judgment weighs — a poll
// status, a build log, a PR's state, whether a dependency's Portfile
// says nomaintainer, which releases a provider offers — arrives as a
// value from the caller that did the reading. That is not tidiness: it
// is what lets the settle corpus run as a table test with no repository,
// no worker and no clock behind it, and it is why a judgment can be
// changed with confidence. The property lives in the import block, where
// one convenient call would end it quietly, so .golangci.yml names the
// edges that would.
//
// The decisions are stated, not worded. A judgment returns what happened
// and why; a verb chooses the sentence. The exceptions are the strings
// that ride a record rather than a terminal — a run's Detail, which is
// written to the note and quoted back by the PR body — and the refusals,
// which are errors with their own exit bands. Those bytes are the
// judgment, so they are produced here.
package verdict

import (
	"strings"

	"github.com/herbygillot/dockhand/internal/record"
)

// Promotable is promote's gate: at least one platform passed, none
// failed, and every subject of the change answered for. The rule is
// stated on the record, where the wire format that carries it lives;
// this presents it as the judgment it is, so a caller asking "may this
// be published" reads that question rather than a field query that
// happens to answer it.
func Promotable(r record.Record) bool { return r.Promotable() }

// Weigh tallies a verdict set to a single weight: Positive when some
// platform says the change works and none says it does not, Negative
// the moment one does, and Neutral when nothing in the set is evidence
// either way — every run still queued, or every one a refusal to test.
//
// A set is a tally and not a vote, which is why one Negative ends it:
// a platform that failed is the question review will ask, and no number
// of passes elsewhere answers it.
//
// A weight is about the RUNS and a promotion is about the CHANGE, so
// the two agree exactly while a change has one subject and no further:
// a cohort whose headline passed and whose dependent nothing built
// weighs positive and is not promotable, because the gate also asks
// whether every member was answered for and the tally has no idea how
// many members there are. Where they do overlap they are held together
// by a test rather than by one calling the other, since they are read
// in different places and a divergence should fail rather than
// propagate.
func Weigh(r record.Record) record.Weight {
	w := record.Neutral
	for _, run := range r.Runs {
		switch run.State.Weight() {
		case record.Negative:
			// One disproof settles the set; nothing later can lift it.
			return record.Negative
		case record.Positive:
			w = record.Positive
		case record.Neutral:
		}
	}
	return w
}

// Summarize compresses a verdict set to one clause — "passed (Sequoia),
// failed (Sonoma)" — in the record's own stable order. It is the drift
// lines' phrasing, and it exists here because Drift cannot state a
// drift finding without it and must stay pure to do so.
//
// One part per RUN and not per platform, which is what the two-map
// split makes different: a platform is a guest and a run is what one
// subject concluded inside it, so a cohort on two platforms has more
// verdicts than platforms. A cohort's parts name their subject,
// because "failed (Sonoma)" about a change with nine members is a
// sentence a reader cannot act on.
//
// The wire word is the clause's text: a state renders as the byte
// sequence the notes and the goldens already carry.
//
// A record with no runs answers "unverified" rather than nothing.
// Schema 3 bears the record at mint, so a verdict set with nothing in
// it is now an ordinary shape — every --no-verify branch has one, and
// so does every mint on a machine with no verify provider — and every
// caller here reads the clause into the middle of a sentence. An empty
// string would put a reader's answer between a space and a bracket:
// "no environment to reach ()". The word is the same one DriftBehind
// ends on when nothing anywhere covers a tip, because it is the same
// fact.
func Summarize(r record.Record) string {
	named := Names(r)
	var parts []string
	for _, ref := range Runs(r) {
		part := string(ref.Run.State) + " (" + ref.Platform + ")"
		if named {
			part = ref.Port + " " + part
		}
		parts = append(parts, part)
	}
	if len(parts) == 0 {
		return "unverified"
	}
	return strings.Join(parts, ", ")
}
