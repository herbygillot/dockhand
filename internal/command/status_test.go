package command

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/engine"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/store"
)

// A check made before tidy, the order the design gives, checked the files
// tidy then committed: status credits it to the commit, as submit does.
func TestStatusCreditsACheckOfTheCommittedFilesToTheCommit(t *testing.T) {
	w := newWorld(t)
	versioned(t, w)
	withBumper(t)
	withScript(t, w, "passed")
	started, err := jsonOf(t, "start", "jq-update")
	require.NoError(t, err)
	t.Setenv("MACPORTS_TREE", dig(t, started.Result, "branch", "worktree").(string))
	_, _, err = dockhand(t, "update", "jq")
	require.NoError(t, err)
	_, _, err = dockhand(t, "check")
	require.NoError(t, err)
	_, _, err = dockhand(t, "tidy")
	require.NoError(t, err)

	out, _, err := dockhand(t, "status")
	require.NoError(t, err)
	require.Contains(t, out, "  Checks   passed for this commit\n")
	require.Contains(t, out, "Next: dockhand submit --branch jq-update\n")

	// Edits on top of the commit are what a check of the working files
	// checked, not the commit.
	dir := dig(t, started.Result, "branch", "worktree").(string)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "textproc/jq/Portfile"), []byte("name jq\nversion 1.8.1\n# more\n"), 0o644))
	_, _, err = dockhand(t, "check")
	require.NoError(t, err)
	out, _, err = dockhand(t, "status")
	require.NoError(t, err)
	require.Contains(t, out, "  Checks   passed for snapshot 2\n")
}

func TestStatusFollowsABranchThroughItsWork(t *testing.T) {
	w := newWorld(t)
	versioned(t, w)
	withBumper(t)
	withScript(t, w, "passed")
	_, _, err := dockhand(t, "start", "jq-update")
	require.NoError(t, err)
	dir := filepath.Join(w.home, "Source", "macports-branches", "jq-update")

	out, _, err := dockhand(t, "status")
	require.NoError(t, err)
	require.Contains(t, out, "BRANCH     PORTS  WORK         CHECKS  PR\njq-update  0      nothing yet  —       —\n")
	require.Contains(t, out, "serve: not running · queue: empty")

	t.Setenv("MACPORTS_TREE", dir)
	_, _, err = dockhand(t, "update", "jq")
	require.NoError(t, err)
	_, _, err = dockhand(t, "check")
	require.NoError(t, err)

	t.Setenv("MACPORTS_TREE", w.clone)
	out, _, err = dockhand(t)
	require.NoError(t, err, "bare dockhand in a ports checkout is status")
	require.Contains(t, out, "Needs you\n  · jq-update  snapshot 1 passed; commit it for review  dockhand tidy --branch jq-update\n")
	require.Contains(t, out, "jq-update  1      edits, uncommitted  passed for snapshot 1  —\n")

	require.NoError(t, os.WriteFile(filepath.Join(dir, "textproc/jq/Portfile"), []byte("name jq\nversion 1.8.1\n# more\n"), 0o644))
	out, _, err = dockhand(t, "status", "--attention")
	require.Equal(t, 3, ExitCode(err), "attention needed exits 3")
	require.Equal(t, "  ! jq-update  snapshot 1 passed; the files have changed since  dockhand check --branch jq-update\n", out)

	t.Setenv("MACPORTS_TREE", dir)
	_, _, err = dockhand(t, "tidy")
	require.ErrorContains(t, err, "needs review", "the hand edit is not dockhand's")
	_, _, err = dockhand(t, "tidy", "--squash", "--message", "jq: update to 1.8.1")
	require.NoError(t, err)
	_, _, err = dockhand(t, "check")
	require.NoError(t, err)
	out, _, err = dockhand(t, "status")
	require.NoError(t, err, "inside a worktree, status is the branch's")
	require.Contains(t, out, "jq-update · ~/Source/macports-branches/jq-update\n  Ports    jq\n  Work     1 commit above master ")
	require.Contains(t, out, "  Checks   passed for this commit\n           jq  command ✓\n  PR       —\nNext: dockhand submit --branch jq-update\n")

	out, _, err = dockhand(t, "status", "--port", "fd")
	require.NoError(t, err)
	require.Contains(t, out, "No open branches.")
}

func TestQueueWaitCancelAndLogs(t *testing.T) {
	w := newWorld(t)
	versioned(t, w)
	withBumper(t)
	withScript(t, w, "passed")
	_, _, err := dockhand(t, "start", "jq-update")
	require.NoError(t, err)
	t.Setenv("MACPORTS_TREE", filepath.Join(w.home, "Source", "macports-branches", "jq-update"))
	_, _, err = dockhand(t, "update", "jq")
	require.NoError(t, err)
	_, _, err = dockhand(t, "logs")
	require.ErrorContains(t, err, "jq-update has no check yet: dockhand check")

	_, _, err = dockhand(t, "check", "-d")
	require.NoError(t, err)
	_, _, err = dockhand(t, "check", "-d")
	require.ErrorContains(t, err, "check-1 is already queued for these files")
	out, _, err := dockhand(t, "queue")
	require.NoError(t, err)
	require.Contains(t, out, "serve: not running · queue: 1 run\n\nRUN      BRANCH     SOURCE      ON       STATE   DETAIL\ncheck-1  jq-update  snapshot 1  command  queued  \n")
	out, _, err = dockhand(t, "check", "-d", "--replace")
	require.NoError(t, err)
	require.Contains(t, out, "Canceled check-1 before it started.\ncheck-2 replaces check-1.\n", "it was only queued (the hugo exercise's certigo run, finding 5)")

	// In the branch's worktree, cancel and wait take its latest check (the
	// hugo exercise's certigo run, finding 4).
	out, _, err = dockhand(t, "cancel")
	require.NoError(t, err)
	require.Equal(t, "Canceled check-2 before it started.\n", out)
	_, _, err = dockhand(t, "cancel", "check-2")
	require.ErrorContains(t, err, "check-2 already canceled")

	_, _, err = dockhand(t, "check", "-d")
	require.NoError(t, err)
	out, errs, err := dockhand(t, "wait")
	require.NoError(t, err)
	require.Contains(t, errs, "check-3 runs here, since no dockhand serve is running.")
	require.Contains(t, out, "Passed for snapshot 1.")
	out, _, err = dockhand(t, "wait", "check-3")
	require.NoError(t, err, "a finished run is reported as it ended")
	require.Contains(t, out, "Passed for snapshot 1.")
	_, _, err = dockhand(t, "wait", "check-2")
	require.ErrorContains(t, err, "check-2 stopped before anything finished", "the check named, not the latest, which finished nothing")

	out, _, err = dockhand(t, "logs", "check-3")
	require.NoError(t, err)
	require.Regexp(t, `check-3 · passed\n  command, attempt 1, run command_[a-z0-9]{16}: finished\n    jq passed  ~/\.dockhand/logs/check-3/command-1/command\.log\.gz\n`, out, "kept compressed once the check ended (D6)")
	here, _, err := dockhand(t, "logs")
	require.NoError(t, err)
	require.Equal(t, out, here, "in the branch's worktree, logs is its latest check's")
	// A provider run's ID finds its evidence too.
	id := regexp.MustCompile(`command_[a-z0-9]{16}`).FindString(out)
	out, _, err = dockhand(t, "logs", id)
	require.NoError(t, err)
	require.Contains(t, out, "check-3 · passed\n  command, attempt 1, run "+id+": finished\n")
	out, _, err = dockhand(t, "logs", id, "--port", "jq")
	require.NoError(t, err)
	require.Contains(t, out, "building from ")
	_, _, err = dockhand(t, "logs", "tart_nosuchrunatall1")
	require.ErrorContains(t, err, "tart_nosuchrunatall1 is neither a check, such as check-42, nor a provider run")
	out, _, err = dockhand(t, "logs", "check-3", "--port", "jq")
	require.NoError(t, err)
	require.Contains(t, out, "building from ")
	require.NotContains(t, out, "Steps in", "a person's command records no steps, so the log is printed whole")
	whole, _, err := dockhand(t, "logs", "check-3", "--port", "jq", "--all")
	require.NoError(t, err)
	require.Equal(t, out, whole)
	_, _, err = dockhand(t, "logs", "check-3", "--all")
	require.EqualError(t, err, "--all goes with a port")
	// A port named alone is the branch's latest check's (field testing,
	// 2026-10-02).
	alone, _, err := dockhand(t, "logs", "jq")
	require.NoError(t, err)
	require.Equal(t, out, alone)
	_, _, err = dockhand(t, "logs", "check-9")
	require.ErrorContains(t, err, "there is no run check-9")

	t.Setenv("MACPORTS_TREE", w.clone)
	_, _, err = dockhand(t, "logs")
	require.ErrorContains(t, err, "name a check, such as check-42, or a provider run; in a branch's worktree, logs shows the branch's latest check")
	_, _, err = dockhand(t, "wait")
	require.EqualError(t, err, "name a check, such as check-42; in a branch's worktree, wait follows the branch's latest check")
	_, _, err = dockhand(t, "cancel")
	require.EqualError(t, err, "name a check, such as check-42; in a branch's worktree, cancel stops the branch's latest check")
}

// A port's log, where its provider recorded where each step of the build
// began, lists the steps with their lines and starts at the port's own
// build, saying what it left out: hugo's own phases began near line 46,400
// of 47,000, after its dependencies' (the hugo exercise). --all prints the
// log alone, whole, as it's printed where no steps were recorded; with
// --json, the whole log comes with its steps and the line its own build
// begins on.
func TestLogsForAPortStartsAtItsOwnBuild(t *testing.T) {
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
	require.NoError(t, writePortLog(&out, result, data, false))
	require.Equal(t, steps+"From line 46004, where hugo's own build begins; --all prints the 46003 lines before it too, its dependencies' installs among them.\n\n"+own, out.String())

	out.Reset()
	require.NoError(t, writePortLog(&out, result, data, true))
	require.Equal(t, string(data), out.String(), "--all prints the log alone")

	view := portLogView(model.Run{Number: 48}, result, data)
	require.Equal(t, 46004, view.OwnBuild)
	require.Equal(t, logStepJSON{Name: "fetch", Line: 46004}, view.Steps[4])
	require.Equal(t, string(data), view.Text, "the whole log, which the steps say where to cut")

	// No steps recorded, as before them, or by another provider: the whole
	// log. Steps with no dependencies' step have nothing to leave out; and
	// a log that ends before its own build was to begin is printed whole,
	// saying so.
	out.Reset()
	require.NoError(t, writePortLog(&out, model.TargetResult{Target: "hugo", Log: path}, data, false))
	require.Equal(t, string(data), out.String())
	out.Reset()
	alone := model.TargetResult{Target: "hugo", Log: path, Steps: []model.LogStep{{Name: model.StepClean, Line: 1}, {Name: model.StepFetch, Line: 2}}}
	require.NoError(t, writePortLog(&out, alone, []byte("--->  Cleaning hugo\n--->  Fetching hugo\n"), false))
	require.Equal(t, "Steps in "+path+":\n  clean  line 1\n  fetch  line 2\n\n--->  Cleaning hugo\n--->  Fetching hugo\n", out.String())
	out.Reset()
	require.NoError(t, writePortLog(&out, result, []byte("--->  Deactivating zlib\n"), false))
	require.Equal(t, steps+"The log ends before line 46004, where hugo's own build was recorded to begin, so it's printed whole.\n\n--->  Deactivating zlib\n", out.String())
	require.Zero(t, portLogView(model.Run{Number: 48}, alone, nil).OwnBuild)
}

// A check's logs of a port open where it failed, or failed its tests,
// before where it passed, the latest of those, and name the others:
// check-65's libuv-devel failed its tests on macOS 15 and passed on 26, and
// logs --port showed 26's (the hugo exercise).
func TestLogsForAPortOpenWhereItFailed(t *testing.T) {
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
	chosen, others, ok := portLogs(logs, "libuv-devel")
	require.True(t, ok)
	require.Equal(t, model.ExecutionID("tart_z4oq"), chosen.execution.ID)
	require.Len(t, others, 1)
	require.Equal(t, model.ExecutionID("tart_k7ma"), others[0].execution.ID)
	require.Equal(t, "passed, its tests failed", portBuildWords(chosen.result))

	// A failed build comes before failed tests; and with none, the latest.
	logs.Executions = append(logs.Executions, build("tart_f41l", tahoe, model.TargetResult{Outcome: model.OutcomeFailed, Phase: model.PhaseInstall}))
	chosen, others, _ = portLogs(logs, "libuv-devel")
	require.Equal(t, model.ExecutionID("tart_f41l"), chosen.execution.ID)
	require.Len(t, others, 2)
	require.Equal(t, "failed at install", portBuildWords(chosen.result))
	logs.Executions = logs.Executions[1:2]
	chosen, others, _ = portLogs(logs, "libuv-devel")
	require.Equal(t, model.ExecutionID("tart_k7ma"), chosen.execution.ID)
	require.Empty(t, others)
	_, _, ok = portLogs(logs, "libuv")
	require.False(t, ok)
}

// A check that passed in an environment made again since says so, and
// asks for another check rather than a submit.
func TestStatusSaysAnEnvironmentWasMadeAgain(t *testing.T) {
	arm := model.Environment{Provider: "tart", Platform: model.Platform{OS: "darwin", Version: "25", Architecture: "arm64"}, DeveloperTools: model.DeveloperToolsXcode}
	latest := model.Run{Number: 3, State: model.RunPassed}
	status := engine.BranchStatus{Branch: model.Branch{Name: "dockhand/jq-update"}, Latest: &latest, Current: true,
		Evidence: &engine.Evidence{Targets: []engine.TargetEvidence{
			{Target: model.PlanTarget{Target: model.Target{Name: "jq"}, Role: model.Changed}, Unchecked: true, Outcomes: []engine.Cell{{Kind: engine.CellRemade, Environment: arm}}},
			{Target: model.PlanTarget{Target: model.Target{Name: "oniguruma"}, Role: model.Also}, Unchecked: true, Outcomes: []engine.Cell{{Kind: engine.CellRemade, Environment: arm}}},
		}}}
	rows := attentionFor(status)
	require.Len(t, rows, 1)
	require.Equal(t, "check-3 passed, but since then "+engine.DescribeEnvironment(arm)+" was made again, from another source or with other tools; jq must be built there again", rows[0].what)
	require.Equal(t, "passed, but needs another check", checkState(status), "the table agrees")

	// Where the provider can say what changed, as Tart says a new guest
	// protocol, it's said: the image is the one it was (the s2n-tls run).
	status.Evidence.Targets[0].Outcomes[0].Change = "dockhand has begun to build each target from its source, never from a published archive, and from clean work"
	rows = attentionFor(status)
	require.Equal(t, "check-3 passed, but since then on "+engine.DescribeEnvironment(arm)+", dockhand has begun to build each target from its source, never from a published archive, and from clean work; jq must be built there again", rows[0].what)
	require.Equal(t, "dockhand check --branch jq-update", rows[0].next, "no environments to name")

	// The check that builds it again builds where this one did, which
	// check.on may not name: check-48 built on macOS 12 and 26, and a
	// bare check built on 26 alone (the hugo exercise's re-checks).
	monterey := model.Environment{Provider: "tart", Platform: model.Platform{OS: "darwin", Version: "21", Architecture: "arm64"}}
	status.Evidence.Plan.Environments = []model.Environment{monterey, arm, {Provider: "github"}}
	rows = attentionFor(status)
	require.Equal(t, "dockhand check --branch jq-update --on tart:12,26 --on github", rows[0].next)
}

// A passed branch whose update chose a release whose tag named another
// commit then than when its check planned it says so, as submit does
// (release-moved): held for a look where serve prepared it, and said for
// a person's own submission to show. Its JSON lists it under moved.
// Whether the tag names another commit now (source-moved) takes the
// network, which status never reads.
func TestStatusSaysAReleaseMovedBeforeItsCheck(t *testing.T) {
	latest := model.Run{Number: 7, State: model.RunPassed}
	detail := "libharbor's git.branch v4 named aaaaaaa when its update chose it, and bbbbbbb when check-7 planned it: the check built another source than the update chose"
	status := engine.BranchStatus{Branch: model.Branch{Name: "dockhand/libharbor-4", State: model.BranchOpen}, Head: "c0ffee", Commits: 1, Latest: &latest, Current: true,
		LatestRevision: &model.Revision{Kind: model.RevisionCommit},
		Evidence:       &engine.Evidence{Targets: []engine.TargetEvidence{{Target: model.PlanTarget{Target: model.Target{Name: "libharbor"}, Role: model.Changed}, Passed: true}}},
		Moved:          []model.Concern{{Origin: model.FromUpstream, Port: "libharbor", Rule: "release-moved", Subject: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", Class: model.Introduced, Detail: detail}}}
	rows := attentionFor(status)
	require.Len(t, rows, 1)
	require.Equal(t, attention{mark: "!", branch: "libharbor-4", what: "passed; " + detail, next: "dockhand submit --branch libharbor-4"}, rows[0])

	status.Branch.Origin = model.OriginServe
	require.Equal(t, "passed; held for a look: "+detail, attentionFor(status)[0].what)

	view := branchView(status)
	require.Equal(t, []concernJSON{{Port: "libharbor", Rule: "release-moved", Subject: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", Detail: detail}}, view.Moved)

	// The branch's own view says it below the release.
	status.Releases = []engine.PortRelease{{Port: "libharbor", Release: model.Release{Version: "4.0", Forge: "github", Repository: "harbor/libharbor", Tag: "v4", Commit: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}}}
	var out strings.Builder
	writeReleases(&out, status)
	require.Equal(t, "  Release  libharbor 4.0, GitHub tag v4 of harbor/libharbor at aaaaaaa\n           ! "+detail+"\n", out.String())

	status.Moved = nil
	require.Equal(t, "passed; waiting for you to submit", attentionFor(status)[0].what, "without it, the row is what it was")
	require.Nil(t, branchView(status).Moved)
}

// A command judges who is alive through one observer session, however
// often it judges: status opened one for its stopped checks and another
// for serve's line on every render, and watch rendered every 30 seconds.
// Each session is a row and two journal events.
func TestStatusOpensOneObserverSession(t *testing.T) {
	w := checkedBranch(t)
	t.Setenv("MACPORTS_TREE", w.clone) // every branch, and serve's line
	started := func() int {
		t.Helper()
		e, err := (&settings{}).open(t.Context())
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
	_, _, err := dockhand(t, "status")
	require.NoError(t, err)
	require.Equal(t, before+1, started())
}

// A branch set aside asks nothing of its checks: one archived after its
// files moved on from what passed isn't asked to be checked again, as
// status --all asked of duckdb-cxx14 (the hugo exercise's review).
func TestAnArchivedBranchAsksNothingOfItsChecks(t *testing.T) {
	latest := model.Run{Number: 3, State: model.RunPassed}
	revision := model.Revision{Kind: model.RevisionSnapshot, Snapshot: 1}
	status := engine.BranchStatus{Branch: model.Branch{Name: "dockhand/duckdb-cxx14", State: model.BranchOpen}, Latest: &latest, LatestRevision: &revision}
	rows := attentionFor(status)
	require.Len(t, rows, 1, "an open branch is asked")
	require.Equal(t, "snapshot 1 passed; the files have changed since", rows[0].what)
	status.Branch.State = model.BranchArchived
	require.Empty(t, attentionFor(status))

	// Nor does its checks column ask for another (the dogfood run with
	// 3086fdb3); it says the check as it ended.
	status.Evidence = &engine.Evidence{Targets: []engine.TargetEvidence{{Target: model.PlanTarget{Target: model.Target{Name: "duckdb"}, Role: model.Changed}, Unchecked: true}}}
	require.Equal(t, "passed (check-3)", checkState(status))
	status.Branch.State = model.BranchOpen
	require.Equal(t, "passed, but needs another check", checkState(status))
}

// A branch whose work is on master already, landed by another route, asks
// only to be set aside, not to be committed for review (cleaning up
// duckdb-cxx14, finding 1); one with a pull request is its pull request's
// to say.
func TestStatusSaysABranchsWorkIsOnMasterAlready(t *testing.T) {
	branch := model.Branch{Name: "dockhand/duckdb-cxx14", State: model.BranchOpen}
	status := engine.BranchStatus{Branch: branch, OnMaster: model.ObjectID(strings.Repeat("e", 40))}
	require.Equal(t, []attention{{mark: "·", branch: "duckdb-cxx14", what: "its changes are on master already, as of eeeeeee", next: "dockhand archive duckdb-cxx14"}}, attentionFor(status))
	status.Branch.PullRequest = &model.PullRequest{Number: 35100}
	require.NotContains(t, attentionFor(status), attention{mark: "·", branch: "duckdb-cxx14", what: "its changes are on master already, as of eeeeeee", next: "dockhand archive duckdb-cxx14"})
}

// A submission's JSON lists its concerns, a moved tag's among them, with
// where each came from and the rule that raised it, for a person's own
// submission too, where only its text and an unattended submission's held
// said them (batch 22's leftover, batch 32).
func TestSubmitJSONListsItsConcerns(t *testing.T) {
	detail := "libharbor's git.branch v4 named aaaaaaa when its update chose it, and bbbbbbb when check-7 planned it: the check built another source than the update chose"
	plan := engine.SubmitPlan{Branch: model.Branch{Name: "dockhand/libharbor-4"},
		Moved: []model.Concern{{Origin: model.FromUpstream, Port: "libharbor", Rule: "release-moved", Subject: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", Class: model.Introduced, Detail: detail}}}
	view := submitView(plan)
	require.Equal(t, []concernJSON{{Origin: "upstream", Port: "libharbor", Rule: "release-moved", Subject: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", Class: string(model.Introduced), Detail: detail}}, view.Concerns)
	require.Empty(t, view.Held, "held is an unattended submission's")
	require.Equal(t, []concernJSON{}, submitView(engine.SubmitPlan{}).Concerns, "an empty list, never null")
}
