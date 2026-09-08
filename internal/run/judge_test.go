package run

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/verify"
)

// The judge is pure, so its whole test surface is values in and values
// out. Everything below is one Evidence and what the one interpreter
// made of it.

func member(port string, names ...string) Member {
	if len(names) == 0 {
		names = []string{port}
	}
	return Member{Port: port, Portdir: "/stage/x/" + port, Names: names}
}

// running is the prior state of a member the guest is building, which is
// what Start writes.
func running() record.Run { return record.Run{State: record.Running} }

func evidenceOf(roster []Member, st verify.Status, log string) Evidence {
	prior := map[string]record.Run{}
	for _, m := range roster {
		prior[m.Port] = running()
	}
	return Evidence{
		Spec:    Spec{Content: "sha256:c", Roster: roster},
		Status:  st,
		Log:     log,
		LogRead: log != "",
		Prior:   prior,
		Claim:   "built in a pristine VM",
		At:      time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC),
	}
}

// A PASS IS A PASS, AND ITS LOG IS READ BEFORE THE GUEST GOES BACK. The
// lint line is the only thing a passing log is read for, and reading it
// is why the release is ReleaseAndReport rather than something that
// happens first.
func TestAPassReadsItsLintLineAndGivesTheGuestBack(t *testing.T) {
	e := evidenceOf([]Member{member("jq")},
		verify.Status{State: verify.Passed}, "0 errors and 2 warnings found\n")

	j := Judge(e)
	require.Len(t, j.Runs, 1)
	got := j.Runs["jq"]
	assert.Equal(t, record.Passed, got.State)
	require.NotNil(t, got.Lint, "the runner lints every member it builds, so a pass has a lint record")
	assert.Equal(t, "2 warnings", *got.Lint)
	assert.Equal(t, ReleaseAndReport, j.Disposition)
	assert.Equal(t, "built in a pristine VM", got.Evidence,
		"what a pass proves is the provider's phrase, stamped as the run settles")
	assert.Equal(t, record.ContentID("sha256:c"), got.Content)
}

// LINTED AND SILENT IS NOT UNLINTED. record.Run.Lint is nil when no lint
// ran and non-nil when one did, which is the one field where the shipped
// record had two — and a passing log with no summary in it must produce
// the empty string rather than the nil.
func TestAPassWithNoLintSummaryStillRecordsThatLintRan(t *testing.T) {
	e := evidenceOf([]Member{member("jq")}, verify.Status{State: verify.Passed}, "built ok\n")
	got := Judge(e).Runs["jq"]
	require.NotNil(t, got.Lint)
	assert.Empty(t, *got.Lint)
}

// A --keep-env PASS KEEPS ITS GUEST. Keep by request beside keep by
// rule: the person who started the run asked to look inside a green
// build, and release is decided in exactly one place.
func TestAPassThatAskedToKeepItsEnvironmentKeepsIt(t *testing.T) {
	e := evidenceOf([]Member{member("jq")}, verify.Status{State: verify.Passed}, "0 errors and 0 warnings found\n")
	e.Prior["jq"] = record.Run{State: record.Running, Ask: record.Ask{KeepEnv: true}}

	j := Judge(e)
	assert.Equal(t, record.Passed, j.Runs["jq"].State)
	assert.Equal(t, Keep, j.Disposition, "the ask is answered in the judgment, never overridden after it")
}

// A FAILURE THAT IS THE PORT'S OWN KEEPS THE ENVIRONMENT, because the
// worker is the thing to go and look at.
func TestAFailureKeepsTheEnvironmentAndCarriesItsDiagnosis(t *testing.T) {
	e := evidenceOf([]Member{member("jq")}, verify.Status{State: verify.Failed},
		"Error: Failed to build jq: command execution failed\nError: See /log/main.log\n")

	j := Judge(e)
	got := j.Runs["jq"]
	assert.Equal(t, record.Failed, got.State)
	assert.Equal(t, "Failed to build jq: command execution failed", got.Detail)
	assert.Equal(t, Keep, j.Disposition)
	assert.Empty(t, j.Blamed, "the port blamed itself, and a member is never a stranger")
}

// A PORT DECLINING THE PLATFORM IS NOT A FAILURE, and its guest goes
// back quietly: a correct refusal leaves nothing to debug.
func TestAPortDecliningThePlatformIsUnsupported(t *testing.T) {
	e := evidenceOf([]Member{member("jq")}, verify.Status{State: verify.Failed},
		"Error: jq is known to fail on this platform\n")

	j := Judge(e)
	assert.Equal(t, record.Unsupported, j.Runs["jq"].State)
	assert.Equal(t, ReleaseQuietly, j.Disposition)
}

// A STRANGER'S BREAKAGE IS NOT THIS CHANGE'S. The branch is untested,
// not disproven, the guest goes back — and the stranger's NAME comes out
// on the judgment rather than being looked up inside it, because the one
// interpreter does not read the ports tree.
func TestADependencyFailureBlocksAndNamesTheStrangerOnTheJudgment(t *testing.T) {
	e := evidenceOf([]Member{member("gomuks")}, verify.Status{State: verify.Failed},
		"Error: Failed to build olm: command execution failed\n")

	j := Judge(e)
	got := j.Runs["gomuks"]
	assert.Equal(t, record.Blocked, got.State)
	assert.Contains(t, got.Detail, "dependency olm")
	assert.Equal(t, ReleaseQuietly, j.Disposition)
	assert.Equal(t, []string{"olm"}, j.Blamed,
		"whether anybody maintains olm is a fact about the tree, and the caller is the one that may look")
}

// A SUBPORT'S FAILURE IS ITS MEMBER'S OWN. The match is against
// Member.Names, and a reader comparing ports alone would find no member,
// blame a stranger, and hand back the environment that would have proved
// it.
func TestAFailureInASubportIsTheMembersOwn(t *testing.T) {
	e := evidenceOf([]Member{member("py-foo", "py-foo", "py312-foo")},
		verify.Status{State: verify.Failed},
		"Error: Failed to build py312-foo: command execution failed\n")

	j := Judge(e)
	assert.Equal(t, record.Failed, j.Runs["py-foo"].State)
	assert.Empty(t, j.Blamed, "py312-foo is one of ours")
	assert.Equal(t, Keep, j.Disposition)
}

// A VANISHED JOB IS A FACT ABOUT THE MACHINE. Nothing is released — the
// worker is already gone, which is the only reason the provider cannot
// find the job.
func TestAVanishedJobErrorsWithoutReleasingAnything(t *testing.T) {
	e := evidenceOf([]Member{member("jq")}, verify.Status{}, "")
	e.Vanished = true

	j := Judge(e)
	assert.Equal(t, record.Errored, j.Runs["jq"].State)
	assert.Equal(t, Keep, j.Disposition)
}

// A JOB STILL RUNNING SETTLES NOTHING. An unchanged run written back
// anyway is a state document rewritten per tick per attempt.
func TestARunningJobSettlesNothing(t *testing.T) {
	j := Judge(evidenceOf([]Member{member("jq")}, verify.Status{State: verify.Running}, ""))
	assert.Empty(t, j.Runs)
	assert.Equal(t, Keep, j.Disposition, "an environment nothing was judged on is somebody else's to hand back")
}

// AN INTERRUPT IS EVIDENCE THE JUDGE READS. Every member comes back
// Canceled or Superseded with a quiet release, whatever the provider was
// in the middle of saying — which is why no run.Cancel effect function
// exists.
func TestAnInterruptIsReadByTheJudgeAndNeverWrittenByACaller(t *testing.T) {
	for _, tc := range []struct {
		why  record.InterruptWhy
		want record.RunState
	}{
		{record.InterruptCanceled, record.Canceled},
		{record.InterruptSuperseded, record.Superseded},
	} {
		e := evidenceOf([]Member{member("jq"), member("gdal")},
			verify.Status{State: verify.Failed}, "Error: Failed to build jq: boom\n")
		e.Interrupt = &record.Interrupt{Why: tc.why, Detail: "stopped"}

		j := Judge(e)
		require.Len(t, j.Runs, 2)
		assert.Equal(t, tc.want, j.Runs["jq"].State)
		assert.Equal(t, tc.want, j.Runs["gdal"].State)
		assert.Equal(t, "stopped", j.Runs["jq"].Detail)
		assert.Equal(t, ReleaseQuietly, j.Disposition,
			"nothing about the verdict depends on whether the guest went back")
	}
}

// A COHORT'S MEMBERS ARE JUDGED APART. The runner goes on past a
// failure, so a member that does not depend on what broke is judged on
// its own section exactly as if nothing around it had gone wrong.
func TestACohortJudgesEachMemberOnItsOwnSection(t *testing.T) {
	roster := []Member{member("libwidget"), member("gdal")}
	log := verify.SubjectMarker("libwidget") + "\nError: Failed to build libwidget: boom\n" +
		verify.SubjectMarker("gdal") + "\n0 errors and 0 warnings found\n"
	e := evidenceOf(roster, verify.Status{State: verify.Failed}, log)
	e.Members = []verify.MemberState{
		{Port: "libwidget", Outcome: verify.MemberFailed},
		{Port: "gdal", Outcome: verify.MemberPassed},
	}

	j := Judge(e)
	assert.Equal(t, record.Failed, j.Runs["libwidget"].State)
	assert.Equal(t, record.Passed, j.Runs["gdal"].State)
	assert.Equal(t, Keep, j.Disposition,
		"one member's own breakage keeps the guest for everybody in it")
}

// A MEMBER SKIPPED FOR A FAILED PREREQUISITE IS BLOCKED ON IT BY NAME,
// and the sentence is true of the prerequisite. Its silence in the log
// is expected and never a fault: the guest's own record is what says so.
func TestASkippedMemberIsBlockedOnThePrerequisiteItNames(t *testing.T) {
	roster := []Member{member("libwidget"), member("gdal")}
	log := verify.SubjectMarker("libwidget") + "\nError: Failed to build libwidget: boom\n"
	e := evidenceOf(roster, verify.Status{State: verify.Failed}, log)
	e.Members = []verify.MemberState{
		{Port: "libwidget", Outcome: verify.MemberFailed},
		{Port: "gdal", Outcome: verify.MemberSkipped, Prerequisite: "libwidget"},
	}

	j := Judge(e)
	assert.Equal(t, record.Failed, j.Runs["libwidget"].State)
	got := j.Runs["gdal"]
	assert.Equal(t, record.Blocked, got.State)
	assert.Equal(t, "libwidget", got.Blamed)
	assert.Contains(t, got.Detail, "libwidget fails to build")
}

// A MEMBER THE GUEST SAID NOTHING ABOUT IS ERRORED AND NEVER PASSED. A
// promotion sums the passes over every member, so a pass invented for a
// member nobody built would authorize publishing on evidence that does
// not exist.
func TestAMemberTheGuestNeverAnnouncedIsErroredAndNotPassed(t *testing.T) {
	roster := []Member{member("libwidget"), member("gdal")}
	log := verify.SubjectMarker("libwidget") + "\n0 errors and 0 warnings found\n"
	e := evidenceOf(roster, verify.Status{State: verify.Passed}, log)

	j := Judge(e)
	assert.Equal(t, record.Passed, j.Runs["libwidget"].State)
	assert.Equal(t, record.Errored, j.Runs["gdal"].State)
}

// A WITHHELD MEMBER IS OUT OF THE JUDGMENT ENTIRELY. The log is silent
// about it by construction, and every rule here reads silence as a
// fault, so the submission's own answer stands.
func TestAWithheldMemberKeepsTheAnswerTheSubmissionGaveIt(t *testing.T) {
	roster := []Member{member("libwidget"), member("gdal")}
	e := evidenceOf(roster, verify.Status{State: verify.Passed}, "0 errors and 0 warnings found\n")
	e.Prior["gdal"] = record.Run{State: record.Withheld, Detail: "conflicts with libwidget"}

	j := Judge(e)
	assert.Contains(t, j.Runs, "libwidget")
	assert.NotContains(t, j.Runs, "gdal", "settling must not overwrite an answer with a worse guess")
}

// A MEMBER THAT ALREADY REACHED A TERMINAL VERDICT IS IN THE ROSTER FOR
// THE BLAME AND OUT OF THE WRITE. This attempt did not watch it, and a
// verdict written over it would replace a fact with a guess.
func TestATerminalMemberIsNotRejudged(t *testing.T) {
	roster := []Member{member("libwidget"), member("gdal")}
	e := evidenceOf(roster, verify.Status{State: verify.Passed}, "0 errors and 0 warnings found\n")
	e.Prior["gdal"] = record.Run{State: record.Unsupported, Detail: "declares known_fail on Sequoia"}

	j := Judge(e)
	assert.NotContains(t, j.Runs, "gdal")
}

// AN ERRORED ENVIRONMENT IS A FACT ABOUT THE MACHINE and never a finding
// about the port, so the guest goes back quietly and the provider's own
// account is what the record carries.
func TestAnErroredEnvironmentIsTheMachinesFault(t *testing.T) {
	e := evidenceOf([]Member{member("jq")},
		verify.Status{State: verify.Errored, Detail: "the guest never came up"}, "")

	j := Judge(e)
	assert.Equal(t, record.Errored, j.Runs["jq"].State)
	assert.Equal(t, "the guest never came up", j.Runs["jq"].Detail)
	assert.Equal(t, ReleaseQuietly, j.Disposition)
}

// KEEP WINS OVER EITHER RELEASE, because one guest holds every member: a
// sibling that passed cannot take away the environment a failure is
// keeping.
func TestOneMemberKeepingTheGuestKeepsItForEverybody(t *testing.T) {
	assert.Equal(t, Keep, fold(ReleaseAndReport, Keep))
	assert.Equal(t, Keep, fold(Keep, ReleaseQuietly))
	assert.Equal(t, ReleaseAndReport, fold(ReleaseQuietly, ReleaseAndReport))
	assert.Equal(t, ReleaseQuietly, fold(ReleaseQuietly, ReleaseQuietly))
}

// THE QUESTION THE BUILD NEVER ASKED IS SAID BESIDE THE VERDICT IT PAID
// FOR. run.Plan schedules a member whose preflight could not be read as
// an ordinary build — a machine that could not ask has learned nothing
// about the port — and Plan's own doc promised the cost of the unasked
// question would be stated where a person meets it. Nothing in the tree
// read Preflight.Err at all, so an unreadable Portfile bought a VM and a
// bare FAILED.
func TestAFailureSaysWhenItsPreflightWasNeverRead(t *testing.T) {
	e := evidenceOf([]Member{member("jq")}, verify.Status{State: verify.Failed},
		"--->  Building jq\nError: failed\n")
	e.Unchecked = map[string]string{"jq": "no Tcl evaluator was acquired"}

	got := Judge(e).Runs["jq"]
	assert.Equal(t, record.Failed, got.State)
	assert.Contains(t, got.Detail, "no Tcl evaluator was acquired")
	assert.Contains(t, got.Detail, "known_fail")
}

// AND ONLY ON A VERDICT THE OMISSION COULD HAVE CHANGED. A pass proves
// the port builds whatever its Portfile declares, so the note there
// would be noise on every member of every attempt staged from a tree
// that would not evaluate.
func TestAPassIsNotAnnotatedWithAnUnreadPreflight(t *testing.T) {
	e := evidenceOf([]Member{member("jq")}, verify.Status{State: verify.Passed}, "built ok\n")
	e.Unchecked = map[string]string{"jq": "no Tcl evaluator was acquired"}

	got := Judge(e).Runs["jq"]
	assert.Equal(t, record.Passed, got.State)
	assert.NotContains(t, got.Detail, "known_fail")
}
