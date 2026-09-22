package view_test

import (
	"testing"
	"time"

	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/workflow/view"
	"github.com/stretchr/testify/require"
)

func TestTookReadsLikeAClock(t *testing.T) {
	t.Parallel()
	at := time.Date(2026, 9, 22, 22, 0, 0, 0, time.UTC)
	for _, test := range []struct {
		later time.Duration
		want  string
	}{
		{300 * time.Millisecond, "under 1s"},
		{8 * time.Second, "8s"},
		{2*time.Minute + 14*time.Second, "2m14s"},
		{31 * time.Minute, "31m00s"},
		{62 * time.Minute, "1h02m"},
		{-time.Second, ""},
	} {
		require.Equal(t, test.want, view.Took(at, at.Add(test.later)), test.later.String())
	}
}

// The phases come from the record and only the finished ones appear, so a
// running job lists what is done and a reattached command says the same
// numbers as the one that watched it.
func TestPhasesComeFromTheRecord(t *testing.T) {
	t.Parallel()
	accepted := time.Date(2026, 9, 22, 21, 0, 0, 0, time.UTC)
	integrated := accepted.Add(2*time.Minute + 14*time.Second)
	created := integrated.Add(5 * time.Second)
	observed := created.Add(31*time.Minute + 40*time.Second)
	confirmed := observed.Add(12 * time.Second)
	finished := confirmed.Add(time.Second)
	entry := view.JobStatus{Job: record.Job{AcceptedAt: accepted, State: record.JobActive, Prepared: &record.PreparedChange{Branch: "b", IntegratedAt: &integrated}}}
	require.Equal(t, []view.Phase{{"preparation", "2m14s"}}, view.Phases(entry), "a running job lists what has finished")
	entry.Attempts = []record.Attempt{{CreatedAt: created, Spec: record.BuildSpec{Target: record.Target{Name: "fixture"}}, Evidence: &record.Evidence{Verdict: record.VerdictPassed, ObservedAt: observed}}}
	entry.Publications = []record.PublicationAction{{ConfirmedAt: &confirmed}}
	entry.Job.State, entry.Job.FinishedAt = record.JobCompleted, &finished
	require.Equal(t, []view.Phase{{"preparation", "2m14s"}, {"verification", "31m40s"}, {"publication", "12s"}, {"in all", "34m12s"}}, view.Phases(entry))
	require.Equal(t, "took 34m12s: preparation 2m14s, verification 31m40s, publication 12s", view.PhaseWords(entry))
	running := view.JobStatus{Job: record.Job{AcceptedAt: accepted, State: record.JobActive}, Attempts: []record.Attempt{{CreatedAt: created, Evidence: &record.Evidence{Verdict: record.VerdictUnknown, ObservedAt: observed}}}}
	require.Empty(t, view.Phases(running), "an unknown verdict is not a finished phase")
	require.Empty(t, view.PhaseWords(running))
}
