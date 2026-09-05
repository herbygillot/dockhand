package verdict

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/verify"
)

// running is a run as a submission leaves it: building, on a named
// platform, carrying what was asked of the build.
//
// It names no job and holds no handle. Both moved to the JobRecord the
// platform names, which is a fact about the guest that every subject in
// the change shares — and a judgment that could reach either would be
// able to write it, which is the thing the split exists to forbid.
func running(linted bool) record.Run {
	return record.Run{
		State:      record.Running,
		Platform:   "Sequoia",
		Linted:     linted,
		FromSource: true,
	}
}

// keepEnv is a run whose submitter asked, with --keep-env, for the
// environment to stand after a pass.
func keepEnv(r record.Run) record.Run {
	r.KeepEnv = true
	return r
}

// A log is a round trip to the guest, so whether to fetch one is a
// decision, not a habit: a failure always needs its log, a pass needs
// one only to corroborate the lint box, and nothing else needs one at
// all.
func TestNeedsLog(t *testing.T) {
	assert.True(t, NeedsLog(verify.Failed, false), "a failure's diagnosis is in its log")
	assert.True(t, NeedsLog(verify.Failed, true))
	assert.True(t, NeedsLog(verify.Passed, true), "the lint box wants corroborating")
	assert.False(t, NeedsLog(verify.Passed, false), "a pass that never linted has nothing to read")
	assert.False(t, NeedsLog(verify.Running, true))
	assert.False(t, NeedsLog(verify.Errored, true))
}

func TestJudgeRunTable(t *testing.T) {
	lintLog := "--->  Verifying Portfile for jq\n--->  0 errors and 2 warnings found.\n--->  Activating jq\n"
	failLog := "--->  Building jq\nError: Failed to build jq: command execution failed\n" +
		"Error: See /opt/local/var/macports/logs/x/main.log for details.\n"
	declineLog := "--->  Skipping jq: known_fail on Monterey\n"
	depLog := "Error: Failed to build olm: command execution failed\n" +
		"Error: Processing of port gomuks failed\n"

	cases := []struct {
		name    string
		in      RunInput
		settled bool
		state   record.RunState
		detail  string
		lint    string
		release ReleaseAction
	}{
		{name: "a job the provider no longer knows",
			in:      RunInput{Run: running(true), Port: "jq", Vanished: true},
			settled: true, state: record.Errored,
			detail:  "job vanished: its worker no longer exists",
			release: KeepWorker},
		{name: "a job still building settles nothing",
			in:      RunInput{Run: running(true), Port: "jq", Status: verify.Status{State: verify.Running}},
			settled: false, state: record.Running, release: KeepWorker},
		{name: "a pass, with the lint line read before the release",
			in: RunInput{Run: running(true), Port: "jq",
				Status: verify.Status{State: verify.Passed, Handle: "fake-1"},
				Log:    lintLog, LogRead: true},
			settled: true, state: record.Passed, lint: "2 warnings", release: ReleaseAndReport},
		{name: "a pass that never linted reads no log",
			in: RunInput{Run: running(false), Port: "jq",
				Status: verify.Status{State: verify.Passed, Handle: "fake-1"}},
			settled: true, state: record.Passed, release: ReleaseAndReport},
		{name: "a pass whose log could not be read keeps the box uncorroborated",
			in: RunInput{Run: running(true), Port: "jq",
				Status: verify.Status{State: verify.Passed, Handle: "fake-1"}, LogRead: false},
			settled: true, state: record.Passed, release: ReleaseAndReport},
		{name: "a pass whose run asked for its environment keeps it (D27)",
			in: RunInput{Run: keepEnv(running(true)), Port: "jq",
				Status: verify.Status{State: verify.Passed, Handle: "fake-1"},
				Log:    lintLog, LogRead: true},
			settled: true, state: record.Passed, lint: "2 warnings", release: KeepWorker},
		{name: "a failure that asked for its environment keeps it as any failure does",
			in: RunInput{Run: keepEnv(running(true)), Port: "jq",
				Status: verify.Status{State: verify.Failed, Handle: "fake-1"},
				Log:    failLog, LogRead: true},
			settled: true, state: record.Failed,
			detail:  "Failed to build jq: command execution failed",
			release: KeepWorker},
		{name: "a failure the port owns keeps its environment",
			in: RunInput{Run: running(true), Port: "jq",
				Status: verify.Status{State: verify.Failed, Handle: "fake-1"},
				Log:    failLog, LogRead: true},
			settled: true, state: record.Failed,
			detail:  "Failed to build jq: command execution failed",
			release: KeepWorker},
		{name: "a failure whose log cannot be read still settles failed",
			in: RunInput{Run: running(true), Port: "jq",
				Status: verify.Status{State: verify.Failed, Handle: "fake-1"}, LogRead: false},
			settled: true, state: record.Failed, release: KeepWorker},
		{name: "a refusal is not a failure",
			in: RunInput{Run: running(true), Port: "jq",
				Status: verify.Status{State: verify.Failed, Handle: "fake-1"},
				Log:    declineLog, LogRead: true},
			settled: true, state: record.Unsupported,
			detail:  "the port declines to build on this platform",
			release: ReleaseQuietly},
		{name: "a neighbour's breakage blocks rather than disproves",
			in: RunInput{Run: running(true), Port: "gomuks",
				Status: verify.Status{State: verify.Failed, Handle: "fake-1"},
				Log:    depLog, LogRead: true},
			settled: true, state: record.Blocked,
			detail:  "dependency olm fails to build; the change itself is untested",
			release: ReleaseQuietly},
		{name: "an unmaintained neighbour says so",
			in: RunInput{Run: running(true), Port: "gomuks",
				Status: verify.Status{State: verify.Failed, Handle: "fake-1"},
				Log:    depLog, LogRead: true, Nomaintainer: true},
			settled: true, state: record.Blocked,
			detail:  "dependency olm (nomaintainer) fails to build; the change itself is untested",
			release: ReleaseQuietly},
		{name: "an environment that could not answer is never a finding about the port",
			in: RunInput{Run: running(true), Port: "jq",
				Status: verify.Status{State: verify.Errored, Detail: "guest agent never came up"}},
			settled: true, state: record.Errored, detail: "guest agent never came up",
			release: ReleaseQuietly},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			j := JudgeRun(tc.in)
			assert.Equal(t, tc.settled, j.Settled, "settled")
			assert.Equal(t, tc.state, j.Run.State, "state")
			assert.Equal(t, tc.detail, j.Run.Detail, "detail")
			assert.Equal(t, tc.lint, j.Run.Lint, "lint evidence")
			assert.Equal(t, tc.release, j.Release, "release")
			// What the submission recorded comes through untouched: a
			// settlement records an outcome, it does not rewrite what
			// was asked for.
			assert.Equal(t, tc.in.Run.Platform, j.Run.Platform, "platform")
			assert.Equal(t, tc.in.Run.Linted, j.Run.Linted, "linted")
			assert.Equal(t, tc.in.Run.FromSource, j.Run.FromSource, "from source")
		})
	}
}

// A run that settled nothing hands its own run back, so a caller that
// writes unconditionally writes the same bytes rather than a zero value
// — and, crucially, learns from Settled that it should not write at all.
func TestJudgeRunLeavesARunningRunAlone(t *testing.T) {
	in := RunInput{Run: running(true), Port: "jq", Status: verify.Status{State: verify.Running}}
	j := JudgeRun(in)
	assert.False(t, j.Settled)
	assert.Equal(t, in.Run, j.Run)
}

// The environment the poll named never reaches the run. A failure asks
// for the guest to be kept and the caller stamps the name on the job,
// once for however many subjects failed in it; a judgment that wrote it
// on the run would have nine copies of one guest's name and no way to
// tell which of them was current.
//
// Stated over the whole status set rather than on the failure alone,
// because the leak this forbids is a field assignment and any branch
// could grow one.
func TestJudgeRunNeverRecordsTheGuest(t *testing.T) {
	for _, st := range []verify.Status{
		{State: verify.Passed, Handle: "fake-1"},
		{State: verify.Failed, Handle: "fake-1"},
		{State: verify.Errored, Handle: "fake-1", Detail: "guest agent never came up"},
	} {
		j := JudgeRun(RunInput{Run: running(true), Port: "jq", Status: st,
			Log: "--->  0 errors and 2 warnings found.\n", LogRead: true})
		require.True(t, j.Settled)
		assert.NotContains(t, j.Run.Detail, "fake-1",
			"%s: the guest's name is the job's, and no run may carry it", st.State)
	}
	// And a failure says keep, which is the whole of how it asks.
	kept := JudgeRun(RunInput{Run: running(true), Port: "jq",
		Status: verify.Status{State: verify.Failed, Handle: "fake-1"}})
	assert.Equal(t, KeepWorker, kept.Release)
}
