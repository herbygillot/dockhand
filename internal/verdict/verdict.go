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

// Promotable is promote's gate: some run proved the change, the
// headline passed somewhere and failed nowhere, and every dependent
// reached an outcome. The rule is stated on the record, where the wire
// format that carries it lives; this presents it as the judgment it is,
// so a caller asking "may this be published" reads that question rather
// than a field query that happens to answer it.
//
// Nothing tallies beside it. A Weigh used to stand here, summing a
// verdict set's runs to one of positive, negative or neutral, held in
// agreement with the gate by a test rather than by one calling the
// other. A weight was about the RUNS and a promotion is about the
// CHANGE, and the two were equal only while a change had one subject —
// and since the dependents became best effort (D24) they are not merely
// unequal at a cohort but opposed: a dependent that failed weighs
// negative and is promotable. Nothing read the tally — the ledger, the
// machine gate and the attention rows all ask the record — and the test
// that pinned the agreement ran over single-subject fixtures, which is
// exactly where the disagreement could not be seen. A gate-adjacent
// function nothing calls is a comment with a compiler, and one that
// contradicts the gate is a wrong comment; so the tally is gone, the
// rule has one home, and what its test pinned about a single subject
// is pinned against this instead.
func Promotable(r record.Record) bool { return r.Promotable() }

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
