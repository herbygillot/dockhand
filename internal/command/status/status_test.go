package status

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/branchstate"
	"github.com/herbygillot/dockhand/internal/command/cmdtest"
	"github.com/herbygillot/dockhand/internal/command/view"
	"github.com/herbygillot/dockhand/internal/engine"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/refusal"
	"github.com/herbygillot/dockhand/internal/store"
	"github.com/herbygillot/dockhand/internal/term"
	"github.com/herbygillot/dockhand/internal/testsupport"
)

// A check made before tidy, the order the design gives, checked the files
// tidy then committed: status credits it to the commit, as submit does.
func TestStatusCreditsACheckOfTheCommittedFilesToTheCommit(t *testing.T) {
	t.Parallel()
	w := cmdtest.NewWorld(t)
	cmdtest.Versioned(t, w)
	cmdtest.WithBumper(t)
	cmdtest.WithScript(t, w, "passed")
	started, err := cmdtest.JSONOf(t, "start", "jq-update")
	require.NoError(t, err)
	cmdtest.Setenv(t, "MACPORTS_TREE", cmdtest.Dig(t, started.Result, "branch", "worktree").(string))
	_, _, err = cmdtest.Dockhand(t, "update", "jq")
	require.NoError(t, err)
	_, _, err = cmdtest.Dockhand(t, "check")
	require.NoError(t, err)
	_, _, err = cmdtest.Dockhand(t, "tidy")
	require.NoError(t, err)

	out, _, err := cmdtest.Dockhand(t, "status")
	require.NoError(t, err)
	require.Contains(t, out, "  Checks   passed for this commit\n")
	require.Contains(t, cmdtest.Flat(out), "Next: fork macports/macports-ports on GitHub and add it as a Git remote, then dockhand submit --branch jq-update\n",
		"no remote pushes to a fork yet (the M1's rerun, D-S6)")
	testsupport.Git(t, w.Clone, "remote", "add", "github-fork", "https://github.com/ada/macports-ports.git")
	out, _, err = cmdtest.Dockhand(t, "status")
	require.NoError(t, err)
	require.Contains(t, out, "Next: dockhand submit --branch jq-update\n", "one does")

	// Edits on top of the commit are what a check of the working files
	// checked, not the commit.
	dir := cmdtest.Dig(t, started.Result, "branch", "worktree").(string)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "textproc/jq/Portfile"), []byte("name jq\nversion 1.8.1\n# more\n"), 0o644))
	_, _, err = cmdtest.Dockhand(t, "check")
	require.NoError(t, err)
	out, _, err = cmdtest.Dockhand(t, "status")
	require.NoError(t, err)
	require.Contains(t, out, "  Checks   passed for snapshot 2\n")
}

func TestStatusFollowsABranchThroughItsWork(t *testing.T) {
	t.Parallel()
	w := cmdtest.NewWorld(t)
	cmdtest.Versioned(t, w)
	cmdtest.WithBumper(t)
	cmdtest.WithScript(t, w, "passed")
	_, _, err := cmdtest.Dockhand(t, "start", "jq-update")
	require.NoError(t, err)
	dir := filepath.Join(w.Home, "Source", "macports-branches", "jq-update")

	out, _, err := cmdtest.Dockhand(t, "status")
	require.NoError(t, err)
	require.Contains(t, out, "Working (1)\n  ⋯ jq-update  nothing yet\n")
	require.Contains(t, out, "serve: not running · queue: empty")

	cmdtest.Setenv(t, "MACPORTS_TREE", dir)
	_, _, err = cmdtest.Dockhand(t, "update", "jq")
	require.NoError(t, err)
	_, _, err = cmdtest.Dockhand(t, "check")
	require.NoError(t, err)

	cmdtest.Setenv(t, "MACPORTS_TREE", w.Clone)
	out, _, err = cmdtest.Dockhand(t)
	require.NoError(t, err, "bare dockhand in a ports checkout is status")
	require.Contains(t, out, "Needs attention (1)\n  ✓ jq-update  snapshot 1 passed; commit it for review\n               dockhand tidy --branch jq-update\n")
	require.NotContains(t, out, "Working", "a branch that needs you is in that group alone")
	out, _, err = cmdtest.Dockhand(t, "status", "-v")
	require.NoError(t, err)
	require.Contains(t, out, "jq-update  1      edits, uncommitted  passed for snapshot 1  —\n", "-v's table of every branch")

	require.NoError(t, os.WriteFile(filepath.Join(dir, "textproc/jq/Portfile"), []byte("name jq\nversion 1.8.1\n# more\n"), 0o644))
	out, _, err = cmdtest.Dockhand(t, "status", "--attention")
	require.Equal(t, 3, term.ExitCode(err), "attention needed exits 3")
	require.Equal(t, "  ! jq-update  snapshot 1 passed on command; the files have changed since, with no check there\n               dockhand check --branch jq-update --on command\n", out)

	cmdtest.Setenv(t, "MACPORTS_TREE", dir)
	_, _, err = cmdtest.Dockhand(t, "tidy")
	require.ErrorContains(t, err, "review this plan in an interactive terminal", "the hand edit is not dockhand's")
	_, _, err = cmdtest.Dockhand(t, "tidy", "--squash", "--message", "jq: update to 1.8.1")
	require.NoError(t, err)
	_, _, err = cmdtest.Dockhand(t, "check")
	require.NoError(t, err)
	out, _, err = cmdtest.Dockhand(t, "status")
	require.NoError(t, err, "inside a worktree, status is the branch's")
	require.Contains(t, out, "jq-update in ~/Source/macports-branches/jq-update\n  Ports    jq (not yet evaluated)\n  Work     1 commit above master ")
	require.Contains(t, out, "\n  Checks   passed for this commit\n           jq  command ✓\n  PR       —\nNext: fork macports/macports-ports on GitHub and add it as a Git remote, then dockhand submit\n  --branch jq-update\n")

	out, _, err = cmdtest.Dockhand(t, "status", "--port", "fd")
	require.NoError(t, err)
	require.Equal(t, "No open branch touches fd yet. dockhand update fd starts one.\n", out)
}

func TestQueueWaitCancelAndLogs(t *testing.T) {
	t.Parallel()
	w := cmdtest.NewWorld(t)
	cmdtest.Versioned(t, w)
	cmdtest.WithBumper(t)
	cmdtest.WithScript(t, w, "passed")
	_, _, err := cmdtest.Dockhand(t, "start", "jq-update")
	require.NoError(t, err)
	cmdtest.Setenv(t, "MACPORTS_TREE", filepath.Join(w.Home, "Source", "macports-branches", "jq-update"))
	_, _, err = cmdtest.Dockhand(t, "update", "jq")
	require.NoError(t, err)
	_, _, err = cmdtest.Dockhand(t, "logs")
	require.ErrorContains(t, err, "jq-update has no check yet: dockhand check")
	// A port's log, in a worktree whose branch has no check, says so,
	// rather than to name a check outside the worktree (the rc5 rerun's
	// A10).
	_, _, err = cmdtest.Dockhand(t, "logs", "jq")
	require.ErrorContains(t, err, "jq-update has no check yet, so no log of jq: dockhand check runs one")
	require.NotContains(t, err.Error(), "outside its branch's worktree")

	_, _, err = cmdtest.Dockhand(t, "check", "-d")
	require.NoError(t, err)
	_, _, err = cmdtest.Dockhand(t, "check", "-d")
	require.ErrorContains(t, err, "check-1 is already queued for these files")
	out, _, err := cmdtest.Dockhand(t, "status", "--all", "-v")
	require.NoError(t, err)
	require.Contains(t, out, "\nChecks queued and running\n  RUN      BRANCH     SOURCE      ON       STATE   DETAIL\n  check-1  jq-update  snapshot 1  command  queued  nothing is running the queue; dockhand wait check-1 runs it here, dockhand serve --drain runs the queue, cancel ends it\n\nserve: not running · queue: 1 run\n")
	_, _, err = cmdtest.Dockhand(t, "queue")
	require.EqualError(t, err, "queue is part of status now: dockhand status lists the checks queued and running")
	_, _, err = cmdtest.Dockhand(t, "watch")
	require.EqualError(t, err, "watch is part of status now: dockhand status --watch keeps it current")
	out, _, err = cmdtest.Dockhand(t, "check", "-d", "--replace")
	require.NoError(t, err)
	require.Contains(t, out, "Replaced check-1, canceled before it started.\ncheck-2 was queued", "it was only queued (the hugo exercise's certigo run, finding 5)")

	// --follow prints a target's log as the driver copies it, until its
	// build ends (the gnome-voice session's notes, 2026-10-20): check-2,
	// queued, has a copy of jq's log so far, and following waits on.
	live := filepath.Join(w.Home, ".dockhand", "logs", "check-2", "command-1", "jq.log")
	require.NoError(t, os.MkdirAll(filepath.Dir(live), 0o755))
	require.NoError(t, os.WriteFile(live, []byte("--->  Building jq\n"), 0o644))
	// Following ends when its output has come, or after a generous
	// while, not after a second and a half of wall clock, which a busy Mac
	// spent opening the engine before the first read.
	ctx, stop := context.WithTimeout(t.Context(), 30*time.Second)
	var followedErrs bytes.Buffer
	followed := &cancelOn{want: "--->  Building jq\n", cancel: stop}
	err = cmdtest.RunFor(t, ctx, []string{"logs", "check-2", "jq", "--follow"}, term.Streams{In: strings.NewReader(""), Out: followed, Err: &followedErrs})
	stop()
	require.ErrorIs(t, err, context.Canceled, followedErrs.String())
	require.Equal(t, "--->  Building jq\n", followed.String())
	_, _, err = cmdtest.Dockhand(t, "logs", "check-2", "--follow")
	require.ErrorContains(t, err, "--follow follows one port's log: name it")
	require.NoError(t, os.RemoveAll(filepath.Dir(live)))

	// In the branch's worktree, cancel and wait take its latest check (the
	// hugo exercise's certigo run, finding 4).
	out, _, err = cmdtest.Dockhand(t, "cancel")
	require.NoError(t, err)
	require.Equal(t, "Canceled check-2 before it started.\n", out)
	_, _, err = cmdtest.Dockhand(t, "cancel", "check-2")
	require.ErrorContains(t, err, "check-2 already canceled")

	_, _, err = cmdtest.Dockhand(t, "check", "-d")
	require.NoError(t, err)
	out, errs, err := cmdtest.Dockhand(t, "wait")
	require.NoError(t, err)
	require.Contains(t, errs, "No dockhand serve is running, so check-3 will run here.")
	require.Contains(t, out, "Passed for snapshot 1.")
	out, _, err = cmdtest.Dockhand(t, "wait", "check-3")
	require.NoError(t, err, "a finished run is reported as it ended")
	require.Contains(t, out, "Passed for snapshot 1.")
	_, _, err = cmdtest.Dockhand(t, "wait", "check-2")
	require.ErrorContains(t, err, "check-2 stopped before anything finished", "the check named, not the latest, which finished nothing")

	out, _, err = cmdtest.Dockhand(t, "logs", "check-3")
	require.NoError(t, err)
	require.Regexp(t, `check-3 · passed\n  command, attempt 1, run command_[a-z0-9]{16}: finished\n    jq passed  ~/\.dockhand/logs/check-3/command-1/command\.log\.gz\n`, out, "kept compressed once the check ended (D6)")
	here, _, err := cmdtest.Dockhand(t, "logs")
	require.NoError(t, err)
	require.Equal(t, out, here, "in the branch's worktree, logs is its latest check's")
	// A provider run's ID finds its evidence too.
	id := regexp.MustCompile(`command_[a-z0-9]{16}`).FindString(out)
	out, _, err = cmdtest.Dockhand(t, "logs", id)
	require.NoError(t, err)
	require.Contains(t, out, "check-3 · passed\n  command, attempt 1, run "+id+": finished\n")
	out, _, err = cmdtest.Dockhand(t, "logs", id, "--port", "jq")
	require.NoError(t, err)
	require.Contains(t, cmdtest.Flat(out), cmdtest.Flat("building from\n  "))
	_, _, err = cmdtest.Dockhand(t, "logs", "tart_nosuchrunatall1")
	require.ErrorContains(t, err, "tart_nosuchrunatall1 is neither a check, such as check-42, nor a provider run")
	out, _, err = cmdtest.Dockhand(t, "logs", "check-3", "--port", "jq")
	require.NoError(t, err)
	require.Contains(t, out, "building from ")
	require.NotContains(t, out, "Steps in", "a person's command records no steps, so the log is printed whole")
	whole, _, err := cmdtest.Dockhand(t, "logs", "check-3", "jq", "--full")
	require.NoError(t, err)
	require.Equal(t, out, whole)
	_, _, err = cmdtest.Dockhand(t, "logs", "check-3", "--full")
	require.EqualError(t, err, "--full requires a port name, for example: dockhand logs check-42 jq --full")
	// A build that ended is printed whole, and following ends.
	ended, _, err := cmdtest.Dockhand(t, "logs", "check-3", "jq", "--follow")
	require.NoError(t, err)
	require.Equal(t, out, ended)
	// A port named alone is the branch's latest check's (field testing,
	// 2026-10-02).
	alone, _, err := cmdtest.Dockhand(t, "logs", "jq")
	require.NoError(t, err)
	require.Equal(t, out, alone)
	_, _, err = cmdtest.Dockhand(t, "logs", "check-9")
	require.ErrorContains(t, err, "there is no run check-9")

	cmdtest.Setenv(t, "MACPORTS_TREE", w.Clone)
	_, _, err = cmdtest.Dockhand(t, "logs")
	require.ErrorContains(t, err, "name a check, such as check-42, a provider run, or a branch with -b or -p; in a branch's worktree, logs shows the branch's latest check")
	_, _, err = cmdtest.Dockhand(t, "wait")
	require.EqualError(t, err, "name a check, such as check-42, or a branch with -b or -p; in a branch's worktree, wait follows the branch's latest check")
	_, _, err = cmdtest.Dockhand(t, "cancel")
	require.EqualError(t, err, "name a check, such as check-42, or a branch with -b or -p; in a branch's worktree, cancel stops the branch's latest check")
}

// A port's log, where its provider recorded where each step of the build
// began, lists the steps with their lines and starts at the port's own
// build, saying what it left out: hugo's own phases began near line 46,400
// of 47,000, after its dependencies' (the hugo exercise). --full prints the
// log alone, whole, as it's printed where no steps were recorded; with
// --json, the whole log comes with its steps and the line its own build
// begins on.
func TestLogsForAPortStartsAtItsOwnBuild(t *testing.T) {
	t.Parallel()
	own := "--->  Fetching distfiles for hugo\n--->  Verifying checksums for hugo\n--->  Building hugo\nError: Failed to build hugo\n"
	data := []byte("--->  Deactivating zlib\n--->  Cleaning hugo\n--->  Verifying Portfile for hugo\n" + strings.Repeat("--->  a dependency's line\n", 46000) + own)
	path := filepath.Join(t.TempDir(), "target-3.log")
	result := model.TargetResult{Target: "hugo", Outcome: model.OutcomeFailed, Phase: model.PhaseInstall, Log: path, Steps: []model.LogStep{
		{Name: model.StepDeactivate, Line: 1}, {Name: model.StepClean, Line: 2}, {Name: model.StepLint, Line: 3}, {Name: model.StepDependencies, Line: 4},
		{Name: model.StepFetch, Line: 46004}, {Name: model.StepChecksum, Line: 46005}, {Name: model.StepInstall, Line: 46006},
	}}
	steps := "Steps in " + path + ":\n" +
		"  deactivate    line 1\n" +
		"  clean         line 2\n" +
		"  lint          line 3\n" +
		"  dependencies  line 4\n" +
		"  fetch         line 46004\n" +
		"  checksum      line 46005\n" +
		"  install       line 46006\n"
	var out strings.Builder
	require.NoError(t, view.WritePortLog(&out, cmdtest.TestPlaces(t), result, data, false))
	require.Equal(t, steps+"Showing from line 46004, where the build for hugo begins. Use --full to include the previous 46003 lines, including dependency installation.\n\n"+own, out.String())

	out.Reset()
	require.NoError(t, view.WritePortLog(&out, cmdtest.TestPlaces(t), result, data, true))
	require.Equal(t, string(data), out.String(), "--full prints the log alone")

	viewed := view.PortLogView(model.Run{Number: 48}, result, data)
	require.Equal(t, 46004, viewed.OwnBuild)
	require.Equal(t, view.LogStepJSON{Name: "fetch", Line: 46004}, viewed.Steps[4])
	require.Equal(t, string(data), viewed.Text, "the whole log, which the steps say where to cut")

	// No steps recorded, as before them, or by another provider: the whole
	// log. Steps with no dependencies' step have nothing to leave out; and
	// a log that ends before its own build was to begin is printed whole,
	// saying so.
	out.Reset()
	require.NoError(t, view.WritePortLog(&out, cmdtest.TestPlaces(t), model.TargetResult{Target: "hugo", Log: path}, data, false))
	require.Equal(t, string(data), out.String())
	out.Reset()
	alone := model.TargetResult{Target: "hugo", Log: path, Steps: []model.LogStep{{Name: model.StepClean, Line: 1}, {Name: model.StepFetch, Line: 2}}}
	require.NoError(t, view.WritePortLog(&out, cmdtest.TestPlaces(t), alone, []byte("--->  Cleaning hugo\n--->  Fetching hugo\n"), false))
	require.Equal(t, "Steps in "+path+":\n  clean  line 1\n  fetch  line 2\n\n--->  Cleaning hugo\n--->  Fetching hugo\n", out.String())
	out.Reset()
	require.NoError(t, view.WritePortLog(&out, cmdtest.TestPlaces(t), result, []byte("--->  Deactivating zlib\n"), false))
	require.Equal(t, steps+"The log ends before line 46004, where hugo's own build was recorded to begin, so it's printed whole.\n\n--->  Deactivating zlib\n", out.String())
	require.Zero(t, view.PortLogView(model.Run{Number: 48}, alone, nil).OwnBuild)
}

// A build that recorded its steps but not the one --phase names says
// which it did record, as one that failed at fetch records no install.
func TestAPhaseABuildDidntRecordSaysWhichItDid(t *testing.T) {
	t.Parallel()
	result := model.TargetResult{Target: "jq", Steps: []model.LogStep{{Name: model.StepClean, Line: 1}, {Name: model.StepDependencies, Line: 2}, {Name: model.StepFetch, Line: 9}}}
	require.EqualError(t, view.PhaseMissing("jq", "install", result), "jq's build recorded no install step; it recorded clean, dependencies, and fetch")
}

// A check's logs of a port open where it failed, or failed its tests,
// before where it passed, the latest of those, and name the others:
// check-65's libuv-devel failed its tests on macOS 15 and passed on 26, and
// logs --port showed 26's (the hugo exercise).
func TestLogsForAPortOpenWhereItFailed(t *testing.T) {
	t.Parallel()
	sequoia := model.Environment{Provider: "tart", Platform: model.Platform{OS: "darwin", Version: "24", Architecture: "arm64"}, DeveloperTools: model.DeveloperToolsXcode}
	tahoe := model.Environment{Provider: "tart", Platform: model.Platform{OS: "darwin", Version: "25", Architecture: "arm64"}, DeveloperTools: model.DeveloperToolsXcode}
	build := func(id string, environment model.Environment, result model.TargetResult) engine.ExecutionLogs {
		result.Target, result.Log = "libuv-devel", "/logs/"+id+"/target-2.log"
		return engine.ExecutionLogs{Execution: model.GuestExecution{ID: model.ExecutionID(id), Environment: environment}, Results: []model.TargetResult{result}}
	}
	logs := engine.RunLogs{Executions: []engine.ExecutionLogs{
		build("tart_z4oq", sequoia, model.TargetResult{Outcome: model.OutcomePassed, Tests: model.TestsFailed}),
		build("tart_k7ma", tahoe, model.TargetResult{Outcome: model.OutcomePassed, Tests: model.TestsPassed}),
	}}
	chosen, others, ok := logs.ForPort("libuv-devel")
	require.True(t, ok)
	require.Equal(t, model.ExecutionID("tart_z4oq"), chosen.Execution.ID)
	require.Len(t, others, 1)
	require.Equal(t, model.ExecutionID("tart_k7ma"), others[0].Execution.ID)
	require.Equal(t, "passed, its tests failed", view.PortBuildWords(chosen.Result))

	// A failed build comes before failed tests; and with none, the latest.
	logs.Executions = append(logs.Executions, build("tart_f41l", tahoe, model.TargetResult{Outcome: model.OutcomeFailed, Phase: model.PhaseInstall}))
	chosen, others, _ = logs.ForPort("libuv-devel")
	require.Equal(t, model.ExecutionID("tart_f41l"), chosen.Execution.ID)
	require.Len(t, others, 2)
	require.Equal(t, "failed at install", view.PortBuildWords(chosen.Result))
	logs.Executions = logs.Executions[1:2]
	chosen, others, _ = logs.ForPort("libuv-devel")
	require.Equal(t, model.ExecutionID("tart_k7ma"), chosen.Execution.ID)
	require.Empty(t, others)
	_, _, ok = logs.ForPort("libuv")
	require.False(t, ok)
}

// A check that passed in an environment made again since says so, and
// asks for another check rather than a submit.
func TestStatusSaysAnEnvironmentWasMadeAgain(t *testing.T) {
	t.Parallel()
	arm := model.Environment{Provider: "tart", Platform: model.Platform{OS: "darwin", Version: "25", Architecture: "arm64"}, DeveloperTools: model.DeveloperToolsXcode}
	latest := model.Run{Number: 3, State: model.RunPassed}
	status := engine.BranchStatus{Branch: model.Branch{Name: "dockhand/jq-update"}, Latest: &latest, Current: true,
		Evidence: &engine.Evidence{Targets: []engine.TargetEvidence{
			{Target: model.PlanTarget{Target: model.Target{Name: "jq"}, Role: model.Changed}, Unchecked: true, Outcomes: []engine.Cell{{Kind: engine.CellRemade, Environment: arm}}},
			{Target: model.PlanTarget{Target: model.Target{Name: "oniguruma"}, Role: model.Also}, Unchecked: true, Outcomes: []engine.Cell{{Kind: engine.CellRemade, Environment: arm}}},
		}}}
	rows := view.AttentionFor(status)
	require.Len(t, rows, 1)
	require.Equal(t, "check-3 passed, but since then "+engine.DescribeEnvironment(arm)+" was made again, from another source or with other tools; jq must be built there again", rows[0].What)
	require.Equal(t, "previous check passed; a new check is required", view.CheckState(status), "the table agrees")

	// Where the provider can say what changed, as Tart says a new guest
	// protocol, it's said: the image is the one it was (the s2n-tls run).
	status.Evidence.Targets[0].Outcomes[0].Change = "dockhand has begun to build each target from its source, never from a published archive, and from clean work"
	rows = view.AttentionFor(status)
	require.Equal(t, "check-3 passed, but since then on "+engine.DescribeEnvironment(arm)+", dockhand has begun to build each target from its source, never from a published archive, and from clean work; jq must be built there again", rows[0].What)
	require.Equal(t, "dockhand check --branch jq-update", rows[0].Next, "no environments to name")

	// The check that builds it again builds where this one did, which
	// check.on may not name: check-48 built on macOS 12 and 26, and a
	// bare check built on 26 alone (the hugo exercise's re-checks).
	monterey := model.Environment{Provider: "tart", Platform: model.Platform{OS: "darwin", Version: "21", Architecture: "arm64"}}
	status.Evidence.Plan.Environments = []model.Environment{monterey, arm, {Provider: "github"}}
	rows = view.AttentionFor(status)
	require.Equal(t, "dockhand check --branch jq-update --on tart:12,26 --on github", rows[0].Next)
}

// A passed branch an upstream finding holds says it as submit would treat
// it: paused where serve prepared it, and for a person's own branch, said
// for submit to show, since a person's submission goes on (the direction
// synthesis's third contradiction).
func TestStatusSaysAHoldAsSubmitTreatsIt(t *testing.T) {
	t.Parallel()
	latest := model.Run{Number: 7, State: model.RunPassed}
	held := model.Concern{Origin: model.FromUpstream, Port: "jq", Rule: "license-changed", Path: "COPYING", Class: model.Introduced, Detail: "upstream's COPYING changed"}
	status := engine.BranchStatus{Branch: model.Branch{Name: "dockhand/jq-1.8.2", State: model.BranchOpen}, Head: "c0ffee", Commits: 1, Latest: &latest, Current: true,
		LatestRevision: &model.Revision{Kind: model.RevisionCommit},
		Evidence:       &engine.Evidence{Targets: []engine.TargetEvidence{{Target: model.PlanTarget{Target: model.Target{Name: "jq"}, Role: model.Changed}, Passed: true}}},
		Held:           []model.Concern{held}}
	require.Equal(t, "passed; upstream's COPYING changed; run submit to review it", view.AttentionFor(status)[0].What)
	status.Branch.Origin = model.OriginServe
	require.Equal(t, "passed; submission paused for review: upstream's COPYING changed", view.AttentionFor(status)[0].What)

	// A second is counted beside the first, as serve says them all.
	build := model.Concern{Origin: model.FromUpstream, Port: "jq", Rule: "build-file-changed", Path: "configure.ac", Class: model.Introduced, Detail: "upstream's configure.ac changed"}
	status.Held = append(status.Held, build)
	require.Equal(t, "passed; submission paused for review: upstream's COPYING changed (and 1 more: dockhand status jq-1.8.2)", view.AttentionFor(status)[0].What)
}

// A passed branch whose update chose a release whose tag named another
// commit then than when its check planned it says so, as submit does
// (release-moved): held for a look where serve prepared it, and said for
// a person's own submission to show. Its JSON lists it under moved.
// Whether the tag names another commit now (source-moved) takes the
// network, which status never reads.
func TestStatusSaysAReleaseMovedBeforeItsCheck(t *testing.T) {
	t.Parallel()
	latest := model.Run{Number: 7, State: model.RunPassed}
	detail := "libharbor's git.branch v4 named aaaaaaa when the update was prepared, then to bbbbbbb when check-7 was planned. The check used a different source from the update"
	status := engine.BranchStatus{Branch: model.Branch{Name: "dockhand/libharbor-4", State: model.BranchOpen}, Head: "c0ffee", Commits: 1, Latest: &latest, Current: true,
		LatestRevision: &model.Revision{Kind: model.RevisionCommit},
		Evidence:       &engine.Evidence{Targets: []engine.TargetEvidence{{Target: model.PlanTarget{Target: model.Target{Name: "libharbor"}, Role: model.Changed}, Passed: true}}},
		Moved:          []model.Concern{{Origin: model.FromUpstream, Port: "libharbor", Rule: "release-moved", Subject: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", Class: model.Introduced, Detail: detail}}}
	rows := view.AttentionFor(status)
	require.Len(t, rows, 1)
	require.Equal(t, view.Attention{Kind: view.AttentionNeedsYou, Branch: "libharbor-4", What: "passed; " + detail, Next: "dockhand submit --branch libharbor-4"}, rows[0])

	status.Branch.Origin = model.OriginServe
	require.Equal(t, "passed; submission paused for review: "+detail, view.AttentionFor(status)[0].What)

	viewed := view.BranchView(status)
	require.Equal(t, []view.ConcernJSON{{Port: "libharbor", Rule: "release-moved", Subject: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", Detail: detail}}, viewed.Moved)

	// The branch's own view says it below the release.
	status.Releases = []engine.PortRelease{{Port: "libharbor", Release: model.Release{Version: "4.0", Forge: "github", Repository: "harbor/libharbor", Tag: "v4", Commit: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}}}
	var out strings.Builder
	writeReleases(&out, status)
	require.Equal(t, "  Release  libharbor 4.0, GitHub tag v4 of harbor/libharbor at aaaaaaa\n           ! "+detail+"\n", out.String())

	status.Moved = nil
	require.Equal(t, "passed; waiting for you to submit", view.AttentionFor(status)[0].What, "without it, the row is what it was")
	require.Nil(t, view.BranchView(status).Moved)
}

// A command judges who is alive through one observer session, however
// often it judges: status opened one for its stopped checks and another
// for serve's line on every render, and watch rendered every 30 seconds.
// Each session is a row and two journal events.
func TestStatusOpensOneObserverSession(t *testing.T) {
	t.Parallel()
	w := cmdtest.CheckedBranch(t)
	cmdtest.Setenv(t, "MACPORTS_TREE", w.Clone) // every branch, and serve's line
	started := func() int {
		t.Helper()
		e, err := cmdtest.TestSettings(t).Open(t.Context())
		require.NoError(t, err)
		defer e.Close()
		var n int
		require.NoError(t, e.Store.View(t.Context(), e.Repository, func(r store.Reader) error {
			n, err = r.CountEvents("session.start", time.Time{})
			return err
		}))
		return n
	}
	before := started()
	_, _, err := cmdtest.Dockhand(t, "status")
	require.NoError(t, err)
	require.Equal(t, before+1, started())
}

// A branch set aside asks nothing of its checks: one archived after its
// files moved on from what passed isn't asked to be checked again, as
// status --all asked of duckdb-cxx14 (the hugo exercise's review).
func TestAnArchivedBranchAsksNothingOfItsChecks(t *testing.T) {
	t.Parallel()
	latest := model.Run{Number: 3, State: model.RunPassed}
	revision := model.Revision{Kind: model.RevisionSnapshot, Snapshot: 1}
	status := engine.BranchStatus{Branch: model.Branch{Name: "dockhand/duckdb-cxx14", State: model.BranchOpen}, Latest: &latest, LatestRevision: &revision}
	rows := view.AttentionFor(status)
	require.Len(t, rows, 1, "an open branch is asked")
	require.Equal(t, "snapshot 1 passed; the files have changed since", rows[0].What)
	// However it ended, as submit refuses for these files' missing check.
	latest.State = model.RunFailed
	require.Equal(t, "snapshot 1 failed; the files have changed since", view.AttentionFor(status)[0].What)
	latest.State = model.RunAttention
	require.Equal(t, "snapshot 1 needed attention; the files have changed since", view.AttentionFor(status)[0].What)
	latest.State = model.RunCanceled
	require.Empty(t, view.AttentionFor(status), "a canceled check found nothing")
	latest.State = model.RunPassed
	status.Branch.State = model.BranchArchived
	require.Empty(t, view.AttentionFor(status))

	// Nor does its checks column ask for another (the dogfood run with
	// 3086fdb3); it says the check as it ended.
	status.Evidence = &engine.Evidence{Targets: []engine.TargetEvidence{{Target: model.PlanTarget{Target: model.Target{Name: "duckdb"}, Role: model.Changed}, Unchecked: true}}}
	require.Equal(t, "passed (check-3)", view.CheckState(status))
	status.Branch.State = model.BranchOpen
	require.Equal(t, "previous check passed; a new check is required", view.CheckState(status))
}

// A branch whose work is on master already, landed by another route, asks
// only to be set aside, not to be committed for review (cleaning up
// duckdb-cxx14, finding 1); one with a pull request is its pull request's
// to say.
func TestStatusSaysABranchsWorkIsOnMasterAlready(t *testing.T) {
	t.Parallel()
	branch := model.Branch{Name: "dockhand/duckdb-cxx14", State: model.BranchOpen}
	status := engine.BranchStatus{Branch: branch, OnMaster: model.ObjectID(strings.Repeat("e", 40))}
	require.Equal(t, []view.Attention{{Kind: view.AttentionReady, Branch: "duckdb-cxx14", What: "its changes are on master already, as of eeeeeee", Next: "dockhand archive duckdb-cxx14"}}, view.AttentionFor(status))
	status.Branch.PullRequest = &model.PullRequest{Number: 35100}
	require.NotContains(t, view.AttentionFor(status), view.Attention{Kind: view.AttentionReady, Branch: "duckdb-cxx14", What: "its changes are on master already, as of eeeeeee", Next: "dockhand archive duckdb-cxx14"})
}

// A submission's JSON lists its concerns, a moved tag's among them, with
// where each came from and the rule that raised it, for a person's own
// submission too, where only its text and an unattended submission's held
// said them (batch 22's leftover, batch 32).
func TestSubmitJSONListsItsConcerns(t *testing.T) {
	t.Parallel()
	detail := "libharbor's git.branch v4 named aaaaaaa when the update was prepared, then to bbbbbbb when check-7 was planned. The check used a different source from the update"
	plan := engine.SubmitPlan{Branch: model.Branch{Name: "dockhand/libharbor-4"},
		Moved: []model.Concern{{Origin: model.FromUpstream, Port: "libharbor", Rule: "release-moved", Subject: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", Class: model.Introduced, Detail: detail}}}
	viewed := view.SubmitView(plan)
	require.Equal(t, []view.ConcernJSON{{Origin: "upstream", Port: "libharbor", Rule: "release-moved", Subject: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", Class: string(model.Introduced), Detail: detail}}, viewed.Concerns)
	require.Empty(t, viewed.Held, "held is an unattended submission's")
	require.Equal(t, []view.ConcernJSON{}, view.SubmitView(engine.SubmitPlan{}).Concerns, "an empty list, never null")
}

// submit --json gives each accepted failure's reason as a kind, beside its
// words, where the words alone said it (R3-30).
func TestSubmitJSONSaysWhyEachFailureIsAccepted(t *testing.T) {
	t.Parallel()
	plan := engine.SubmitPlan{Accepted: map[string]engine.AcceptedFailure{
		"py313-tool": {Kind: engine.AcceptedAtBase, Baselines: []string{"check-2"}},
		"py-tool":    {Kind: engine.AcceptedBlocked},
	}}
	require.Equal(t, []view.AcceptedJSON{
		{Port: "py-tool", Kind: "blocked-by-accepted", Words: "blocked only by accepted failures"},
		{Port: "py313-tool", Kind: "at-base", Baselines: []string{"check-2"}, Words: "fails at the base too (check-2)"},
	}, view.SubmitView(plan).Accepted)
	require.Nil(t, view.SubmitView(engine.SubmitPlan{}).Accepted, "absent where nothing is accepted")
}

// One long message sets no other row's width: each wraps at the terminal's
// width beside its branch, its command on the line below (the gnome-voice
// session's notes: commands ended near column 400).
func TestAttentionRowsWrapWithTheirCommandBelow(t *testing.T) {
	t.Parallel()
	var out strings.Builder
	writeAttention(&out, []view.Attention{
		{Kind: view.AttentionFailed, Branch: "gnome-voice", What: "check-482 failed: gnome-voice failed to build on macOS 26 at configure, and the log names the missing dependency", Next: "dockhand logs check-482 gnome-voice"},
		{Kind: view.AttentionNeedsYou, Branch: "jq", What: "snapshot 1 passed", Next: "dockhand tidy --branch jq"},
	}, 60)
	require.Equal(t, ""+
		"  "+view.AttentionFailed.Mark()+" gnome-voice  check-482 failed: gnome-voice failed to\n"+
		"                 build on macOS 26 at configure, and the log\n"+
		"                 names the missing dependency\n"+
		"                 dockhand logs check-482 gnome-voice\n"+
		"  "+view.AttentionNeedsYou.Mark()+" jq           snapshot 1 passed\n"+
		"                 dockhand tidy --branch jq\n", out.String())
}

// Every verb a verdict's Next can name renders as a command line, so a
// new kind can't render as nothing (architecture, 2026-10-22).
func TestEveryNextVerbRenders(t *testing.T) {
	t.Parallel()
	for _, verb := range refusal.Verbs {
		next := branchstate.Next{Verb: verb, Branch: "jq-1.8.2", Args: []string{"macports/macports-ports", "35001"}}
		line := view.RenderNext(next, "")
		require.True(t, strings.HasPrefix(line, "dockhand ") || strings.HasPrefix(line, "open "), "%s renders as %q", verb, line)
	}
	require.Equal(t, "fork macports/macports-ports on GitHub and add it as a Git remote, then dockhand submit --branch jq-1.8.2",
		view.RenderNext(branchstate.Next{Verb: branchstate.VerbSubmit, Branch: "jq-1.8.2", After: branchstate.AfterFork}, ""))
}

// cancelOn is a writer that cancels once what's written holds want.
type cancelOn struct {
	bytes.Buffer
	want   string
	cancel context.CancelFunc
}

func (c *cancelOn) Write(p []byte) (int, error) {
	n, err := c.Buffer.Write(p)
	if strings.Contains(c.String(), c.want) {
		c.cancel()
	}
	return n, err
}

// submit --json gives each block's kind beside its words, where blocking
// listed sentences alone.
func TestSubmitJSONGivesEachBlocksKind(t *testing.T) {
	t.Parallel()
	plan := engine.SubmitPlan{Blocks: []branchstate.Block{{Kind: branchstate.BlockNoTitle}, {Kind: branchstate.BlockFailed, Port: "jq", Run: "check-2"}}}
	require.Equal(t, []view.BlockJSON{
		{Kind: "no-title", Words: "the pull request needs a title, since the branch changes several ports: give one with --title"},
		{Kind: "failed", Port: "jq", Words: "jq did not pass in check-2. Fix the failure, submit a draft with --draft, or run dockhand check --baseline to see whether master fails in the same way"},
	}, view.SubmitView(plan).Blocks)
}
