package verdict

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/herbygillot/dockhand/internal/record"
)

// subject is the port every one-subject fixture here is about.
const subject = "jq"

// set builds a one-subject verdict set from platform-to-state pairs,
// which is all the tally judgments read.
//
// The runs are keyed and stamped the way the ledger writes them —
// RunKey(port, release), with the platform on the run as well as in the
// key — because a fixture that keyed by release alone would be testing
// a note shape nothing writes, and every projection here reaches a run
// through its subject.
func set(states map[string]record.RunState) record.Record {
	r := record.Record{
		Schema:   record.Schema,
		Sha:      "cafe",
		Subjects: []record.Subject{{Port: subject, Names: []string{subject}}},
		Runs:     map[string]record.Run{},
	}
	for plat, s := range states {
		runOn(r, plat, record.Run{State: s})
	}
	return r
}

// runOn puts one run on one platform of the fixture's subject.
func runOn(r record.Record, plat string, run record.Run) {
	run.Platform = plat
	r.Runs[record.RunKey(subject, plat)] = run
}

// Promotable and Weigh answer the same question in two shapes, and they
// are held together here rather than by one calling the other: they are
// read in different places, and a divergence should fail rather than
// propagate.
//
// Every record here has one subject, which is where the two are equal.
// A weight is a tally over the runs and a promotion also asks whether
// every member was answered for, so at a cohort the gate is the
// stricter of the two — TestPromotableSumsOverEveryMember is where that
// difference is stated.
func TestPromotableAndWeighAgree(t *testing.T) {
	cases := []struct {
		name   string
		states map[string]record.RunState
		weight record.Weight
	}{
		{"nothing recorded", nil, record.Neutral},
		{"one pass", map[string]record.RunState{"Sequoia": record.Passed}, record.Positive},
		{"one failure", map[string]record.RunState{"Sequoia": record.Failed}, record.Negative},
		{"a pass and a failure: the failure settles it",
			map[string]record.RunState{"Sequoia": record.Passed, "Sonoma": record.Failed}, record.Negative},
		// A port declining a platform is often the change working, and a
		// dependency breaking left the change untested rather than
		// disproven. Neither argues against publication.
		{"a pass beside a refusal",
			map[string]record.RunState{"Sequoia": record.Passed, "Sonoma": record.Unsupported}, record.Positive},
		{"a pass beside a blocked run",
			map[string]record.RunState{"Sequoia": record.Passed, "Sonoma": record.Blocked}, record.Positive},
		{"a refusal alone proves nothing",
			map[string]record.RunState{"Sequoia": record.Unsupported}, record.Neutral},
		{"still running", map[string]record.RunState{"Sequoia": record.Running}, record.Neutral},
		{"queued", map[string]record.RunState{"Sequoia": record.Queued}, record.Neutral},
		{"claimed but not yet started",
			map[string]record.RunState{"Sequoia": record.Submitting}, record.Neutral},
		{"canceled and superseded say nothing either",
			map[string]record.RunState{"Sequoia": record.Canceled, "Sonoma": record.Superseded}, record.Neutral},
		{"an errored environment is a fact about the machine",
			map[string]record.RunState{"Sequoia": record.Errored}, record.Neutral},
		{"a failure among refusals still stops it",
			map[string]record.RunState{"Sequoia": record.Unsupported, "Sonoma": record.Failed}, record.Negative},
		{"a word this build cannot read is not evidence",
			map[string]record.RunState{"Sequoia": "invented"}, record.Neutral},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := set(tc.states)
			assert.Equal(t, tc.weight, Weigh(r), "weight")
			assert.Equal(t, tc.weight == record.Positive, Promotable(r),
				"promotable is exactly a positive tally")
		})
	}
}

func TestSummarize(t *testing.T) {
	assert.Equal(t, "passed (Sequoia)",
		Summarize(set(map[string]record.RunState{"Sequoia": record.Passed})))
	// Platform order is the record's own, which is sorted — the clause
	// reads the same however the map was built.
	assert.Equal(t, "failed (Monterey), passed (Sequoia), blocked (Sonoma)",
		Summarize(set(map[string]record.RunState{
			"Sequoia": record.Passed, "Sonoma": record.Blocked, "Monterey": record.Failed})))
}

// A record holding no verdict is now an ordinary shape: schema 3 bears
// the record at mint, so every --no-verify branch has one and so does
// every mint on a machine with no verify provider. The clause is read
// into the middle of a sentence — "no environment to reach (%s)" — and
// an empty one leaves the reader a pair of brackets around nothing.
func TestSummarizeAnswersAWordForARecordWithNoRuns(t *testing.T) {
	assert.Equal(t, "unverified", Summarize(record.Record{}))
	assert.Equal(t, "unverified", Summarize(record.Record{
		Schema: record.Schema, Sha: "cafe",
		Subjects:    []record.Subject{{Port: subject, Names: []string{subject}}},
		Destination: record.ToBranch,
	}), "a change bound to the branch alone will never hold one")
}

// A queued run names a platform no job does. The summary must still
// report it: a change whose only run is waiting for a slot is exactly
// when a reader asks what is happening, and a clause built from the
// submitted platforms alone would answer with silence.
func TestSummarizeReportsAQueuedRunWithNoJob(t *testing.T) {
	r := set(map[string]record.RunState{"Sequoia": record.Queued})
	assert.Empty(t, r.Platforms(), "nothing was submitted")
	assert.Equal(t, "queued (Sequoia)", Summarize(r))
}

// A cohort's verdicts name their subject. One is not enough to act on
// otherwise: "failed (Sonoma)" about a nine-member change says a
// failure happened and not which port to go and look at.
func TestSummarizeNamesEachSubjectOfACohort(t *testing.T) {
	r := record.Record{
		Schema: record.Schema, Sha: "cafe",
		Subjects: []record.Subject{{Port: "libwidget"}, {Port: "widget-tools"}},
		Runs: map[string]record.Run{
			record.RunKey("libwidget", "Sequoia"):    {State: record.Failed, Platform: "Sequoia"},
			record.RunKey("widget-tools", "Sequoia"): {State: record.Blocked, Platform: "Sequoia"},
		},
	}
	// Build order, headline first — not sorted, because the order is
	// the order the change happens in.
	assert.Equal(t, "libwidget failed (Sequoia), widget-tools blocked (Sequoia)", Summarize(r))
}
