package command

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/buildenv/ghactions"
	"github.com/herbygillot/dockhand/internal/forge"
	"github.com/herbygillot/dockhand/internal/model"
)

// fakeActions stands in for GitHub Actions in your fork: each push of a
// dockhand-check branch, as the fork's hook records it, starts a run,
// which completes on the second look with the logs given for its attempt.
// As on GitHub, a branch's runs are still listed once the branch is gone.
type fakeActions struct {
	fork string
	// logs are each attempt's jobs' logs, by job name, whichever run.
	logs       []map[string]string
	conclusion []string
	runs       []*fakeRun
	// starting is a push found once, whose run GitHub hasn't started.
	starting bool
	looks    int
	reruns   int
	canceled int
	// hold keeps a run in progress, calling hold at each look, until it
	// returns false.
	hold func() bool
}

// fakeRun is a run, and the branch and commit whose push started it.
type fakeRun struct {
	ghactions.Run
	branch, commit string
}

// recordPushes has the fork record each push it takes, as GitHub sees
// the pushes that start runs.
func recordPushes(t *testing.T, fork string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(fork, "hooks", "post-receive"), []byte("#!/bin/sh\ncat >> pushes\n"), 0o755))
}

// refuseDeletions has the fork refuse to delete a branch, as a fork that
// can't be reached would fail to.
func refuseDeletions(t *testing.T, fork string) {
	t.Helper()
	hook := "#!/bin/sh\nwhile read old new ref; do\n\tcase \"$new\" in\n\t*[!0]*) ;;\n\t*) echo \"no deleting $ref here\" >&2; exit 1 ;;\n\tesac\ndone\n"
	require.NoError(t, os.WriteFile(filepath.Join(fork, "hooks", "pre-receive"), []byte(hook), 0o755))
}

func (f *fakeActions) pushed(branch, commit string) bool {
	out, err := exec.Command("git", "--git-dir", f.fork, "rev-parse", "--verify", "-q", "refs/heads/"+branch).Output()
	return err == nil && strings.TrimSpace(string(out)) == commit
}

// pushes counts the fork's pushes of the commit to the branch.
func (f *fakeActions) pushes(branch, commit string) int {
	data, _ := os.ReadFile(filepath.Join(f.fork, "pushes"))
	pushes := 0
	for _, line := range strings.Split(string(data), "\n") {
		if fields := strings.Fields(line); len(fields) == 3 && fields[1] == commit && fields[2] == "refs/heads/"+branch {
			pushes++
		}
	}
	return pushes
}

func (f *fakeActions) Runs(_ context.Context, repository, branch, commit string) ([]ghactions.Run, error) {
	if repository != "ada/macports-ports" {
		return nil, nil
	}
	var runs []ghactions.Run
	for _, r := range f.runs {
		if r.branch == branch && r.commit == commit {
			runs = append(runs, r.Run)
		}
	}
	for pushed := f.pushes(branch, commit); len(runs) < pushed; {
		// GitHub starts a push's run a little after the push: here, at
		// the second look that finds the push.
		if f.starting = !f.starting; f.starting {
			break
		}
		id := int64(7 + len(f.runs))
		started := &fakeRun{Run: ghactions.Run{ID: id, Attempt: 1, Status: "queued", URL: fmt.Sprintf("https://github.com/ada/macports-ports/actions/runs/%d", id)}, branch: branch, commit: commit}
		f.runs = append(f.runs, started)
		runs = append(runs, started.Run)
	}
	return runs, nil
}

func (f *fakeActions) find(id int64) *fakeRun {
	for _, r := range f.runs {
		if r.ID == id {
			return r
		}
	}
	panic(fmt.Sprintf("no run %d", id))
}

func (f *fakeActions) Run(_ context.Context, _ string, id int64) (ghactions.Run, error) {
	f.looks++
	run := f.find(id)
	switch run.Status {
	case "queued":
		run.Status = "in_progress"
	case "in_progress":
		if f.hold != nil && f.hold() {
			time.Sleep(10 * time.Millisecond)
			break
		}
		run.Status, run.Conclusion = "completed", f.conclusion[run.Attempt-1]
	}
	return run.Run, nil
}

// Rerun is refused once the run's branch is gone: GitHub doesn't document
// what it reruns then, so the provider never asks it to.
func (f *fakeActions) Rerun(_ context.Context, _ string, id int64) error {
	run := f.find(id)
	if !f.pushed(run.branch, run.commit) {
		return fmt.Errorf("run %d's branch %s is gone", id, run.branch)
	}
	f.reruns++
	run.Attempt++
	run.Status, run.Conclusion = "queued", ""
	return nil
}

func (f *fakeActions) Cancel(_ context.Context, repository string, id int64) error {
	f.canceled++
	run := f.find(id)
	run.Status, run.Conclusion = "completed", "cancelled"
	return nil
}

func (f *fakeActions) Jobs(_ context.Context, _ string, id int64, attempt int) ([]ghactions.RunnerJob, error) {
	var jobs []ghactions.RunnerJob
	for i, name := range []string{"build (macos-14)", "build (macos-15)"} {
		if _, ok := f.logs[attempt-1][name]; ok {
			// Each job ran on a runner of the label its name gives.
			label := strings.TrimSuffix(strings.TrimPrefix(name, "build ("), ")")
			jobs = append(jobs, ghactions.RunnerJob{ID: int64(attempt*10 + i), Name: name, Status: "completed", Labels: []string{label}, RunnerName: fmt.Sprint("GitHub Actions ", 1000+i)})
		}
	}
	// GitHub doesn't say in what order it lists them.
	slices.Reverse(jobs)
	return jobs, nil
}

func (f *fakeActions) JobLog(_ context.Context, _ string, job int64) ([]byte, error) {
	name := []string{"build (macos-14)", "build (macos-15)"}[job%10]
	return []byte(f.logs[job/10-1][name]), nil
}

func withActions(t *testing.T, f *fakeActions) {
	testActions = f
	t.Cleanup(func() { testActions = nil })
}

func built(port string, testsFail bool) string {
	log := fmt.Sprintf("2026-09-25T10:00:01.0Z ##[group]Listing subports\n2026-09-25T10:00:01.1Z %s\n2026-09-25T10:00:01.2Z ##[endgroup]\n"+
		"2026-09-25T10:01:31.0Z ##[group]Installing %s\n2026-09-25T10:02:00.0Z ##[endgroup]\n2026-09-25T10:02:01.0Z ##[group]Testing %s\n", port, port, port)
	if testsFail {
		log += "2026-09-25T10:02:30.0Z ##[error]Tests failed for " + port + "\n"
	}
	return log
}

// githubBranch starts jq-update with an update of jq, ready to check.
func githubBranch(t *testing.T) (*fakeActions, world) {
	t.Helper()
	w := newWorld(t)
	versioned(t, w)
	withBumper(t)
	_, _, err := dockhand(t, "start", "jq-update")
	require.NoError(t, err)
	t.Setenv("MACPORTS_TREE", filepath.Join(w.home, "Source", "macports-branches", "jq-update"))
	_, _, err = dockhand(t, "update", "jq")
	require.NoError(t, err)
	testPortReader = onePort{}
	t.Cleanup(func() { testPortReader = nil })
	g := withGitHub(t, w)
	recordPushes(t, g.fork)
	f := &fakeActions{fork: g.fork}
	withActions(t, f)
	return f, w
}

// checkBranches are the dockhand-check branches on the fork.
func checkBranches(t *testing.T, f *fakeActions) string {
	t.Helper()
	return strings.TrimSpace(gitRun(t, f.fork, "for-each-ref", "--format=%(refname:short)", "refs/heads/dockhand-check/"))
}

func TestGitHubBuildsWithMacPortsWorkflowInYourFork(t *testing.T) {
	f, _ := githubBranch(t)
	f.logs = []map[string]string{{"build (macos-14)": built("jq", false), "build (macos-15)": built("jq", true)}}
	f.conclusion = []string{"success"}

	out, _, err := dockhand(t, "check", "--on", "github", "--plan")
	require.NoError(t, err)
	require.Contains(t, out, "Provider    github · tests declared\nPushes      the revision to a dockhand-check/ branch of your fork, where MacPorts' workflow builds it, and removes the branch once the run is read\n")
	_, _, err = dockhand(t, "check", "--on", "github:sonoma")
	require.ErrorContains(t, err, "takes no releases")

	out, errs, err := dockhand(t, "check", "--on", "github")
	require.NoError(t, err, errs)
	require.Contains(t, errs, "github: pushing to dockhand-check/")
	require.Contains(t, errs, "github: in progress https://github.com/ada/macports-ports/actions/runs/7")
	require.Contains(t, errs, "github: jq passed")
	require.Contains(t, out, "  jq  ✓ build passed; tests failed (advisory)\n")
	require.Contains(t, out, "Passed for snapshot 1.")
	require.Equal(t, 0, f.reruns)

	// The commit was pushed to your fork, and its branch removed once the
	// run was read (the sshuttle run's finding 7); each runner's log is
	// kept beside the run's, read by the run's ID, not the branch.
	require.Len(t, f.runs, 1)
	require.Regexp(t, `^dockhand-check/[0-9a-f]{12}$`, f.runs[0].branch)
	require.Contains(t, errs, "github: removed "+f.runs[0].branch+" from ada/macports-ports\n")
	require.Empty(t, checkBranches(t, f))
	logs, _, err := dockhand(t, "logs", "check-1")
	require.NoError(t, err)
	require.Contains(t, logs, "build-macos-15.log")
	require.Regexp(t, `\n      build \(macos-15\) passed  \S+build-macos-15\.log\n`, logs, "each runner's part, under the port's result")
	// The workflow run's URL is how the run is named, and found.
	require.Contains(t, logs, "\n    https://github.com/ada/macports-ports/actions/runs/7\n")
	logs, _, err = dockhand(t, "logs", "https://github.com/ada/macports-ports/actions/runs/7")
	require.NoError(t, err)
	require.Regexp(t, `github, attempt 1, run github_[a-z0-9]{16}: finished`, logs)
}

func TestGitHubRunsAFailureNoPortExplainsAgain(t *testing.T) {
	f, _ := githubBranch(t)
	// The first attempt dies before listing a port; the rerun builds jq.
	f.logs = []map[string]string{{"build (macos-14)": "2026-09-25T10:00:00.0Z ##[error]bootstrap failed\n"}, {"build (macos-14)": built("jq", false)}}
	f.conclusion = []string{"failure", "success"}

	_, errs, err := dockhand(t, "check", "--on", "github")
	require.NoError(t, err, errs)
	require.Contains(t, errs, "ended failure without building a port")
	require.Contains(t, errs, "unsuccessful jobs again")
	require.Contains(t, errs, "github: jq passed")
	require.Equal(t, 1, f.reruns)
	// The first attempt left the branch for the second to run the run
	// again, which the fake refuses without it; the second removed it.
	require.Len(t, f.runs, 1)
	require.Equal(t, 1, strings.Count(errs, "github: removed "+f.runs[0].branch+" from ada/macports-ports\n"))
	require.Empty(t, checkBranches(t, f))
}

// A run that fails without naming a port on every attempt has its branch
// removed once the last attempt has read it: no attempt runs it again.
func TestTheLastAttemptOfAGitHubCheckRemovesItsBranch(t *testing.T) {
	f, _ := githubBranch(t)
	stuck := map[string]string{"build (macos-14)": "2026-09-25T10:00:00.0Z ##[error]bootstrap failed\n"}
	f.logs = []map[string]string{stuck, stuck, stuck}
	f.conclusion = []string{"failure", "failure", "failure"}

	_, errs, err := dockhand(t, "check", "--on", "github")
	require.Error(t, err, errs)
	require.Equal(t, 2, f.reruns)
	require.Len(t, f.runs, 1)
	require.Equal(t, 1, strings.Count(errs, "github: removed "+f.runs[0].branch+" from ada/macports-ports\n"))
	require.Empty(t, checkBranches(t, f))
}

// The branch is gone once its run is read, so a later check of the same
// commit, as retry is, pushes it again and reads the run that push starts,
// not the earlier one GitHub still lists for the branch. The earlier
// check's logs stay readable: they were kept as the run was read.
func TestRetryingAGitHubCheckRunsTheWorkflowAgain(t *testing.T) {
	f, _ := githubBranch(t)
	f.logs = []map[string]string{{"build (macos-14)": built("jq", false)}}
	f.conclusion = []string{"success"}
	_, errs, err := dockhand(t, "check", "--on", "github")
	require.NoError(t, err, errs)
	require.Empty(t, checkBranches(t, f))

	out, errs, err := dockhand(t, "retry", "check-1")
	require.NoError(t, err, errs)
	require.Contains(t, out, "check-2 repeats check-1")
	require.Contains(t, errs, "github: pushing to dockhand-check/")
	require.Contains(t, errs, "github: in progress https://github.com/ada/macports-ports/actions/runs/8\n")
	require.NotContains(t, errs, "actions/runs/7", "the earlier check's run isn't this one's")
	require.Contains(t, errs, "github: jq passed")
	require.Len(t, f.runs, 2)
	require.Equal(t, f.runs[0].branch, f.runs[1].branch)
	require.Empty(t, checkBranches(t, f))

	logs, _, err := dockhand(t, "logs", "check-2")
	require.NoError(t, err)
	require.Contains(t, logs, "\n    https://github.com/ada/macports-ports/actions/runs/8\n")
	logs, _, err = dockhand(t, "logs", "check-1")
	require.NoError(t, err)
	require.Contains(t, logs, "\n    https://github.com/ada/macports-ports/actions/runs/7\n")
	logged, _, err := dockhand(t, "logs", "check-1", "--port", "jq")
	require.NoError(t, err)
	require.Contains(t, logged, "##[group]Installing jq")
}

// A serve that stops leaves the run going and its branch on the fork, so
// the next driver finds that run, rather than pushing again and starting
// another, and removes the branch once it has read it.
func TestAResumedGitHubCheckReadsTheRunItLeft(t *testing.T) {
	f, _ := githubBranch(t)
	f.logs = []map[string]string{{"build (macos-14)": built("jq", false)}, {"build (macos-14)": built("jq", false)}}
	f.conclusion = []string{"success", "success"}
	_, _, err := dockhand(t, "check", "--on", "github", "-d")
	require.NoError(t, err)

	ctx := t.Context()
	e, err := (&settings{}).open(ctx)
	require.NoError(t, err)
	defer e.Close()
	run, err := e.RunNamed(ctx, "check-1")
	require.NoError(t, err)
	serving, err := startSession(ctx, e, model.SessionServe)
	require.NoError(t, err)
	stopping, stop := context.WithCancel(ctx)
	f.hold = func() bool {
		stop()
		return true
	}
	stopped, err := e.Resume(stopping, serving, run.ID)
	require.NoError(t, err)
	require.Equal(t, model.RunRunning, stopped.State, "serve stopping cancels nothing")
	require.NoError(t, serving.End(context.WithoutCancel(ctx)))
	require.Equal(t, 0, f.canceled)
	require.NotEmpty(t, checkBranches(t, f), "the run isn't read, so its branch stays")

	f.hold = nil
	_, errs, err := dockhand(t, "wait", "check-1")
	require.NoError(t, err, errs)
	require.Contains(t, errs, "github: jq passed")
	require.Len(t, f.runs, 1, "the run it left, not another")
	require.Equal(t, 0, f.reruns)
	require.Empty(t, checkBranches(t, f))
}

func TestGitHubReadsAFailedPortsPhase(t *testing.T) {
	f, _ := githubBranch(t)
	f.logs = []map[string]string{{"build (macos-14)": built("jq", false), "build (macos-15)": "2026-09-25T10:00:01.0Z ##[group]Listing subports\n2026-09-25T10:00:01.1Z jq\n2026-09-25T10:00:01.2Z ##[endgroup]\n2026-09-25T10:04:00.0Z ##[error]Failed to install jq\n"}}
	f.conclusion = []string{"failure"}

	_, errs, err := dockhand(t, "check", "--on", "github")
	require.Error(t, err)
	require.Contains(t, errs, "github: jq failed at install")
	require.Equal(t, 0, f.reruns)
}

// Tested on gives the macOS release of each of the workflow's runners, as
// the label its job ran on names it in GitHub's jobs API, in one order
// whichever GitHub lists them in; it says nothing of their Xcode, which
// only their logs' text gives (the sshuttle run's finding 8).
func TestTestedOnGivesTheGitHubRunnersReleases(t *testing.T) {
	f, _ := githubBranch(t)
	f.logs = []map[string]string{{"build (macos-14)": built("jq", false), "build (macos-15)": built("jq", false)}}
	f.conclusion = []string{"success"}
	_, errs, err := dockhand(t, "check", "--on", "github")
	require.NoError(t, err, errs)
	_, _, err = dockhand(t, "tidy")
	require.NoError(t, err)

	previewed, _, err := dockhand(t, "submit", "--plan")
	require.NoError(t, err)
	require.Contains(t, previewed, "\n    ###### Tested on\n\n    macOS 14, 15\n"+
		"    Developer tools not recorded · github: MacPorts' CI workflow in the author's fork (Run ID: https://github.com/ada/macports-ports/actions/runs/7 - checked in check-1)\n")
}

func TestCancelingAGitHubCheckCancelsItsRun(t *testing.T) {
	f, _ := githubBranch(t)
	f.logs = []map[string]string{{"build (macos-14)": built("jq", false)}}
	f.conclusion = []string{"success"}
	asked := false
	f.hold = func() bool {
		if !asked {
			asked = true
			_, _, err := dockhand(t, "cancel", "check-1")
			require.NoError(t, err)
		}
		return true
	}

	_, errs, err := dockhand(t, "check", "--on", "github")
	require.Error(t, err, errs)
	require.Equal(t, 1, f.canceled)
	// Its run isn't read, and a retry runs the run again, so the branch
	// is left for clean.
	require.NotEmpty(t, checkBranches(t, f))
}

// A branch the check couldn't remove from your fork is said once, doesn't
// fail the check, and is left for clean, which removes it once the branch
// is merged.
func TestCleanRemovesTheCheckBranchesLeftInYourFork(t *testing.T) {
	f, w := githubBranch(t)
	f.logs = []map[string]string{{"build (macos-14)": built("jq", false)}}
	f.conclusion = []string{"success"}
	refuseDeletions(t, f.fork)
	_, errs, err := dockhand(t, "check", "--on", "github")
	require.NoError(t, err, errs)
	checked := checkBranches(t, f)
	require.NotEmpty(t, checked)
	require.Equal(t, 1, strings.Count(errs, "github: couldn't remove "+checked+" from ada/macports-ports, which dockhand clean removes once the branch is merged: "), errs)
	require.NotContains(t, errs, "github: removed")
	require.NoError(t, os.Remove(filepath.Join(f.fork, "hooks", "pre-receive")))

	_, _, err = dockhand(t, "tidy")
	require.NoError(t, err)
	_, _, err = dockhand(t, "submit", "--no-check", "--yes")
	require.NoError(t, err)
	g := testForge(nil).(*fakeGitHub)
	g.prs[0].State = forge.PullRequestMerged
	t.Setenv("MACPORTS_TREE", w.clone)
	_, _, err = dockhand(t, "status", "--refresh")
	require.NoError(t, err)

	out, _, err := dockhand(t, "clean")
	require.NoError(t, err)
	require.Contains(t, out, "  remove   ada/macports-ports:"+checked+"\n")
	_, _, err = dockhand(t, "clean", "--yes")
	require.NoError(t, err)
	require.Empty(t, strings.TrimSpace(gitRun(t, f.fork, "for-each-ref", "refs/heads/dockhand-check/")))
}

// MacPorts' workflow reports a port whose build passed and whose tests
// failed; under --tests required that fails the check, as it does on
// Tart. Under --tests skip the plan says the workflow's tests still run.
// (The architecture review of 2026-09-27, finding 1.)
func TestRequiredTestsFailACheckOnGitHub(t *testing.T) {
	f, _ := githubBranch(t)
	f.logs = []map[string]string{{"build (macos-14)": built("jq", false), "build (macos-15)": built("jq", true)}}
	f.conclusion = []string{"success"}
	out, _, err := dockhand(t, "check", "--on", "github", "--tests", "required")
	require.Error(t, err, "a failing test fails a check that requires tests")
	require.Contains(t, out, "  jq  ✗ failed at test\n")

	out, _, err = dockhand(t, "check", "--on", "github", "--tests", "skip", "--plan")
	require.NoError(t, err)
	require.Contains(t, out, "Provider    github · tests skip\n            github runs its workflow's own tests; with --tests skip they run there, and don't count\n")
}
