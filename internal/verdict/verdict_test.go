package verdict

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/herbygillot/dockhand/internal/record"
)

// set builds a verdict set from platform-to-state pairs, which is all
// the tally judgments read.
func set(states map[string]record.RunState) record.Record {
	r := record.Record{Schema: 2, Sha: "cafe", Port: "jq", Runs: map[string]record.Run{}}
	for plat, s := range states {
		r.Runs[plat] = record.Run{State: s}
	}
	return r
}

// Promotable and Weigh answer the same question in two shapes, and they
// are held together here rather than by one calling the other: they are
// read in different places, and a divergence should fail rather than
// propagate.
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
		{"queued", map[string]record.RunState{"Sequoia": record.Deferred}, record.Neutral},
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
	assert.Empty(t, Summarize(record.Record{}), "an empty set has no clause")
	assert.Equal(t, "passed (Sequoia)",
		Summarize(set(map[string]record.RunState{"Sequoia": record.Passed})))
	// Platform order is the record's own, which is sorted — the clause
	// reads the same however the map was built.
	assert.Equal(t, "failed (Monterey), passed (Sequoia), blocked (Sonoma)",
		Summarize(set(map[string]record.RunState{
			"Sequoia": record.Passed, "Sonoma": record.Blocked, "Monterey": record.Failed})))
}
