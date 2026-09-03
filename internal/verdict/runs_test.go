package verdict

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/verify"
)

// The walk's order is the record's own: subjects in build order,
// platforms sorted within each. Two renderings of one record name its
// runs identically because of this, and a reader meets a cohort in the
// order the commits happen in rather than alphabetically.
func TestRunsWalkSubjectsInBuildOrderAndPlatformsSorted(t *testing.T) {
	r := record.Record{
		Subjects: []record.Subject{{Port: "libwidget"}, {Port: "aardvark"}},
		Runs: map[string]record.Run{
			record.RunKey("libwidget", "Sonoma"):  {State: record.Passed, Platform: "Sonoma"},
			record.RunKey("libwidget", "Sequoia"): {State: record.Passed, Platform: "Sequoia"},
			record.RunKey("aardvark", "Sonoma"):   {State: record.Blocked, Platform: "Sonoma"},
		},
	}
	var got []string
	for _, ref := range Runs(r) {
		got = append(got, ref.Port+"@"+ref.Platform)
	}
	assert.Equal(t, []string{
		"libwidget@Sequoia", "libwidget@Sonoma", // the headline first, whatever it sorts as
		"aardvark@Sonoma",
	}, got)
}

// The guest comes back with the run, and Submitted says whether there
// was one. A queued run has no job, and the zero JobRecord a map hands
// back for a missing key would otherwise read as a guest that started
// at the zero time — which is how a report comes to say a build has
// been running since year one.
func TestRunsReportWhetherAGuestExists(t *testing.T) {
	job := record.JobRecord{Job: verify.Job{Provider: "fake", ID: "fake-1"}, Test: true}
	r := record.Record{
		Subjects: []record.Subject{{Port: "jq"}},
		Jobs:     map[string]record.JobRecord{"Sequoia": job},
		Runs: map[string]record.Run{
			record.RunKey("jq", "Sequoia"): {State: record.Running, Platform: "Sequoia"},
			record.RunKey("jq", "Sonoma"):  {State: record.Queued, Platform: "Sonoma"},
		},
	}
	refs := Runs(r)
	require.Len(t, refs, 2, "the queued run is part of the answer")

	assert.Equal(t, "Sequoia", refs[0].Platform)
	assert.True(t, refs[0].Submitted)
	assert.Equal(t, job, refs[0].Job)

	assert.Equal(t, "Sonoma", refs[1].Platform)
	assert.False(t, refs[1].Submitted, "nothing was submitted for a queued run")
	assert.Zero(t, refs[1].Job)
}

// One guest, N verdicts. The whole point of the split is that the job
// is reached once per platform and every subject's run points at the
// same one, so a reader counting environments counts platforms and not
// runs.
func TestRunsShareOneGuestAcrossSubjects(t *testing.T) {
	job := record.JobRecord{Job: verify.Job{Provider: "fake", ID: "fake-1"}}
	r := record.Record{
		Subjects: []record.Subject{{Port: "libwidget"}, {Port: "widget-tools"}},
		Jobs:     map[string]record.JobRecord{"Sequoia": job},
		Runs: map[string]record.Run{
			record.RunKey("libwidget", "Sequoia"):    {State: record.Failed, Platform: "Sequoia"},
			record.RunKey("widget-tools", "Sequoia"): {State: record.Blocked, Platform: "Sequoia"},
		},
	}
	refs := Runs(r)
	require.Len(t, refs, 2)
	assert.Equal(t, refs[0].Job, refs[1].Job, "two verdicts, one environment")
	assert.Equal(t, []string{"Sequoia"}, r.Platforms(), "one guest is one platform")
}

// Naming the subject is a property of the change and not of the line.
// A cohort whose second member has no runs yet must not print its first
// member's lines as though the change were about that member alone.
func TestNamesFollowsTheSubjectsAndNotTheRuns(t *testing.T) {
	assert.False(t, Names(record.Record{}), "nothing to distinguish")
	assert.False(t, Names(set(map[string]record.RunState{"Sequoia": record.Passed})))

	cohort := record.Record{
		Subjects: []record.Subject{{Port: "libwidget"}, {Port: "widget-tools"}},
		Runs: map[string]record.Run{
			record.RunKey("libwidget", "Sequoia"): {State: record.Running, Platform: "Sequoia"},
		},
	}
	assert.True(t, Names(cohort), "one member has run; the change is still a cohort")
}

// A run whose port no subject names is not reached. The ledger writes a
// subject for every port it records a run against, so such a run is a
// mangled record — and inventing a subject for it here would put a
// different answer in the report from the one every acting verb sees.
func TestRunsSkipARunNoSubjectClaims(t *testing.T) {
	r := record.Record{
		Subjects: []record.Subject{{Port: "jq"}},
		Runs: map[string]record.Run{
			record.RunKey("jq", "Sequoia"):      {State: record.Passed, Platform: "Sequoia"},
			record.RunKey("stranger", "Sonoma"): {State: record.Failed, Platform: "Sonoma"},
		},
	}
	refs := Runs(r)
	require.Len(t, refs, 1)
	assert.Equal(t, "jq", refs[0].Port)
}
