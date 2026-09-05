package tart

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/verify"
)

// The record is read back through the provider, from the files the
// runner left, possibly in another process: a settle holds a job value
// and nothing else, and the guest's own files are what make the
// members' outcomes recoverable from it.
func TestMemberStatesReadsTheRunnersRecordInBuildOrder(t *testing.T) {
	g := newFakeGuest(t, "")
	for i, port := range []string{"libfoo", "bar", "baz", "qux"} {
		g.plant(subjectFile(i), verify.SubjectMarker(port)+"\n")
	}
	g.plant("state.0", "passed\n")
	g.plant("state.1", "failed\n")
	g.plant("state.2", "passed\n")
	g.plant("state.3", "skipped\n1\n")

	got, err := g.provider().MemberStates(t.Context(), g.job())
	require.NoError(t, err)

	assert.Equal(t, []verify.MemberState{
		{Port: "libfoo", Outcome: verify.MemberPassed},
		{Port: "bar", Outcome: verify.MemberFailed},
		{Port: "baz", Outcome: verify.MemberPassed},
		{Port: "qux", Outcome: verify.MemberSkipped, Prerequisite: "bar"},
	}, got, "one entry per member, the skip's position translated into the port it names")
}

// A member the runner wrote nothing about is reported as such, and not
// dropped: the record is one entry per member the guest was asked to
// build, and a hole in it is the fact the judge needs — the runner
// finished without writing this member's word, which is a runner fault
// and not a verdict about the port.
func TestMemberStatesKeepsAMemberTheRunnerWroteNothingAbout(t *testing.T) {
	g := newFakeGuest(t, "")
	g.plant(subjectFile(0), verify.SubjectMarker("oniguruma")+"\n")
	g.plant(subjectFile(1), verify.SubjectMarker("jq")+"\n")
	g.plant("state.0", "failed\n")

	got, err := g.provider().MemberStates(t.Context(), g.job())
	require.NoError(t, err)

	assert.Equal(t, []verify.MemberState{
		{Port: "oniguruma", Outcome: verify.MemberFailed},
		{Port: "jq", Outcome: verify.MemberUnreported},
	}, got)
}

// A single-subject job has no per-member record: its runner is the
// frozen one and writes none. That is nothing to report rather than a
// failure to read, the same way a missing baseline is for Manifests.
func TestASoloJobHasNoMemberRecord(t *testing.T) {
	g := newFakeGuest(t, "")
	g.plant("state", "passed\n")
	g.plant("log", "--->  0 errors and 0 warnings found.\n")

	got, err := g.provider().MemberStates(t.Context(), g.job())
	require.NoError(t, err)
	assert.Empty(t, got)
}

// The reader's vocabulary is the runner's three words. A word the
// runner never writes, a skip naming a position outside the record, and
// a skip naming its own position are read as far as they can be and no
// further: the outcome the file states, and no prerequisite invented
// for it.
func TestMemberStatesOfReadsOnlyWhatTheRunnerWrites(t *testing.T) {
	out := verify.SubjectMarker("a") + "\n" + "exploded\n" +
		verify.SubjectMarker("b") + "\n" + "skipped\n7\n" +
		verify.SubjectMarker("c") + "\n" + "skipped\n2\n" +
		verify.SubjectMarker("d") + "\n" + "skipped\nzero\n" +
		verify.SubjectMarker("e") + "\n" + "skipped\n0\n"

	assert.Equal(t, []verify.MemberState{
		{Port: "a", Outcome: verify.MemberUnreported},
		{Port: "b", Outcome: verify.MemberSkipped},
		{Port: "c", Outcome: verify.MemberSkipped},
		{Port: "d", Outcome: verify.MemberSkipped},
		{Port: "e", Outcome: verify.MemberSkipped, Prerequisite: "a"},
	}, memberStatesOf(out))
}

// The guard is Poll's: a job of another provider is a contract error,
// and a worker that is gone is unknown rather than empty.
func TestMemberStatesGuardsTheJobTheWayPollDoes(t *testing.T) {
	g := newFakeGuest(t, "")

	_, err := g.provider().MemberStates(t.Context(), verify.Job{Provider: "fake", ID: g.vm})
	require.ErrorIs(t, err, verify.ErrUnknownJob)

	_, err = g.provider().MemberStates(t.Context(), verify.Job{Provider: "tart", ID: "dockhand-worker-gone"})
	require.ErrorIs(t, err, verify.ErrUnknownJob)
}

// subjectFile is the marker file's name at one position, as launch
// writes it.
func subjectFile(i int) string { return fmt.Sprintf("subject.%d", i) }
