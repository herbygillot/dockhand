package command

import (
	"bytes"
	"context"
	"errors"
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
	"github.com/herbygillot/dockhand/internal/command/cmdtest"
	"github.com/herbygillot/dockhand/internal/command/view"
	"github.com/herbygillot/dockhand/internal/forge"
	"github.com/herbygillot/dockhand/internal/forge/forgetest"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/term"
	"github.com/herbygillot/dockhand/internal/testsupport"
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
	// workflow is the fork's workflow's state, active when empty.
	workflow string
	// untaken are the jobs GitHub cancelled before any runner took them,
	// which have no runner and no log, in each attempt whose log of them
	// is empty: a rerun's runner takes the job.
	untaken map[string]bool
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
	testsupport.WriteExecutable(t, filepath.Join(fork, "hooks", "post-receive"), "#!/bin/sh\ncat >> pushes\n")
}

// refuseDeletions has the fork refuse to delete a branch, as a fork that
// can't be reached would fail to.
func refuseDeletions(t *testing.T, fork string) {
	t.Helper()
	hook := "#!/bin/sh\nwhile read old new ref; do\n\tcase \"$new\" in\n\t*[!0]*) ;;\n\t*) echo \"no deleting $ref here\" >&2; exit 1 ;;\n\tesac\ndone\n"
	testsupport.WriteExecutable(t, filepath.Join(fork, "hooks", "pre-receive"), hook)
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
			if f.untaken[name] && f.logs[attempt-1][name] == "" {
				jobs = append(jobs, ghactions.RunnerJob{ID: int64(attempt*10 + i), Name: name, Status: "completed", Conclusion: "cancelled", Labels: []string{label}})
				continue
			}
			jobs = append(jobs, ghactions.RunnerJob{ID: int64(attempt*10 + i), Name: name, Status: "completed", Labels: []string{label}, RunnerName: fmt.Sprint("GitHub Actions ", 1000+i)})
		}
	}
	// GitHub doesn't say in what order it lists them.
	slices.Reverse(jobs)
	return jobs, nil
}

// WorkflowState says the fork's workflow's state, active, as Actions
// enabled leave it, unless the test says otherwise.
func (f *fakeActions) WorkflowState(context.Context, string) (string, error) {
	if f.workflow == "" {
		return "active", nil
	}
	return f.workflow, nil
}

func (f *fakeActions) JobLog(_ context.Context, _ string, job int64) ([]byte, error) {
	name := []string{"build (macos-14)", "build (macos-15)"}[job%10]
	if f.untaken[name] && f.logs[job/10-1][name] == "" {
		return nil, errors.New("job log: fetch: HTTP 404")
	}
	return []byte(f.logs[job/10-1][name]), nil
}

// Annotations say why GitHub's runners took no job, for one it cancelled
// before any did.
func (f *fakeActions) Annotations(_ context.Context, _ string, job int64) ([]string, error) {
	name := []string{"build (macos-14)", "build (macos-15)"}[job%10]
	if f.untaken[name] && f.logs[job/10-1][name] == "" {
		return []string{"The job was not acquired by Runner of type hosted even after multiple attempts"}, nil
	}
	return nil, nil
}

func withActions(t *testing.T, f *fakeActions) {
	cmdtest.Stands(t).Actions = f
}

// both is each release's job's log, the same.
func both(log string) map[string]string {
	return map[string]string{"build (macos-14)": log, "build (macos-15)": log}
}

func built(port string, testsFail bool) string {
	log := fmt.Sprintf("2026-09-25T10:00:01.0Z ##[group]Listing subports\n2026-09-25T10:00:01.1Z %s\n2026-09-25T10:00:01.2Z ##[endgroup]\n"+
		"2026-09-25T10:01:00.0Z ##[group]Installing dependencies for %s\n2026-09-25T10:01:30.0Z ##[endgroup]\n"+
		"2026-09-25T10:01:31.0Z ##[group]Installing %s\n2026-09-25T10:02:00.0Z ##[endgroup]\n2026-09-25T10:02:01.0Z ##[group]Testing %s\n", port, port, port, port)
	if testsFail {
		log += "2026-09-25T10:02:30.0Z ##[error]Tests failed for " + port + "\n"
	}
	return log
}

// githubBranch starts jq-update with an update of jq, ready to check, on
// master whose workflow builds on macOS 14 and 15, each its own
// environment.
func githubBranch(t *testing.T) (*fakeActions, cmdtest.World) {
	t.Helper()
	w := cmdtest.NewWorld(t)
	cmdtest.Versioned(t, w)
	require.NoError(t, os.MkdirAll(filepath.Join(w.Upstream, ".github", "workflows"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(w.Upstream, ".github", "workflows", "main.yml"), []byte("jobs:\n  build:\n    strategy:\n      matrix:\n        os: [macos-14, macos-15]\n"), 0o644))
	testsupport.Git(t, w.Upstream, "add", ".github")
	testsupport.Git(t, w.Upstream, "commit", "-q", "-m", "CI")
	cmdtest.WithBumper(t)
	_, _, err := cmdtest.Dockhand(t, "start", "jq-update")
	require.NoError(t, err)
	cmdtest.Setenv(t, "MACPORTS_TREE", filepath.Join(w.Home, "Source", "macports-branches", "jq-update"))
	_, _, err = cmdtest.Dockhand(t, "update", "jq")
	require.NoError(t, err)
	cmdtest.UsePorts(cmdtest.Stands(t), cmdtest.OnePort{})
	g := cmdtest.WithGitHub(t, w)
	recordPushes(t, g.Fork)
	f := &fakeActions{fork: g.Fork}
	withActions(t, f)
	return f, w
}

// checkBranches are the dockhand-check branches on the fork.
func checkBranches(t *testing.T, f *fakeActions) string {
	t.Helper()
	return strings.TrimSpace(testsupport.Git(t, f.fork, "for-each-ref", "--format=%(refname:short)", "refs/heads/dockhand-check/"))
}

func TestGitHubBuildsWithMacPortsWorkflowInYourFork(t *testing.T) {
	t.Parallel()
	f, _ := githubBranch(t)
	f.logs = []map[string]string{{"build (macos-14)": built("jq", false), "build (macos-15)": built("jq", true)}}
	f.conclusion = []string{"success"}

	out, _, err := cmdtest.Dockhand(t, "check", "--on", "github", "--plan")
	require.NoError(t, err)
	require.Contains(t, out, "Provider    github macOS 14 (Sonoma) arm64; github macOS 15 (Sequoia) arm64 · tests where declared\nPushes      this revision to a dockhand-check/ branch in your fork for the MacPorts workflow; removes that branch after reading the run's results\n")
	_, _, err = cmdtest.Dockhand(t, "check", "--on", "github:sonoma")
	require.ErrorContains(t, err, "the github provider builds every release MacPorts' workflow names; to check one release, use tart or local")

	out, errs, err := cmdtest.Dockhand(t, "check", "--on", "github", "-v")
	require.NoError(t, err, errs)
	require.Contains(t, errs, "github: pushing to dockhand-check/")
	require.Contains(t, errs, "github: in progress https://github.com/ada/macports-ports/actions/runs/7")
	require.Contains(t, errs, "github macOS 14 (Sonoma) arm64: jq passed")
	require.Contains(t, errs, "github macOS 15 (Sequoia) arm64: jq passed")
	// Each release's job is its own cell (the M1's suggestion, 2026-10-29).
	require.Regexp(t, `\n  PORT +macOS 14 +macOS 15\n  jq +✓ tests passed( \(\d+s\))? +✓ build passed; tests failed \(advisory\)( \(\d+s\))?\n`, out)
	require.Contains(t, out, "\nPassed.\n", "the check's heading named what it checks")
	require.Equal(t, 0, f.reruns)

	// The commit was pushed to your fork, and its branch removed once the
	// run was read (the sshuttle run's finding 7); each runner's log is
	// kept beside the run's, read by the run's ID, not the branch.
	require.Len(t, f.runs, 1)
	require.Regexp(t, `^dockhand-check/[0-9a-f]{12}$`, f.runs[0].branch)
	require.Contains(t, errs, "github: removed "+f.runs[0].branch+" from ada/macports-ports\n")
	require.Empty(t, checkBranches(t, f))
	logs, _, err := cmdtest.Dockhand(t, "logs", "check-1")
	require.NoError(t, err)
	require.Contains(t, logs, "build-macos-15.log")
	require.Regexp(t, `\n      build \(macos-15\) passed  \S+build-macos-15\.log\.gz\n`, logs, "each runner's part, under the port's result")
	// The workflow run's URL is how the run is named, and found.
	require.Contains(t, logs, "\n    https://github.com/ada/macports-ports/actions/runs/7\n")
	logs, _, err = cmdtest.Dockhand(t, "logs", "https://github.com/ada/macports-ports/actions/runs/7")
	require.NoError(t, err)
	// The run built each release, each its own environment's execution.
	require.Regexp(t, `github macOS 14 \(Sonoma\) arm64, attempt 1, run github_[a-z0-9]{16}: finished`, logs)
	require.Regexp(t, `github macOS 15 \(Sequoia\) arm64, attempt 1, run github_[a-z0-9]{16}: finished`, logs)
}

// A fork whose workflow won't run is said at once, before anything is
// pushed, with its Actions page, where the check waited ten minutes for a
// run that never came, and a retry ten more (the docs dig's I24).
func TestAGitHubCheckRefusesAtOnceWhereTheForksWorkflowWontRun(t *testing.T) {
	t.Parallel()
	f, _ := githubBranch(t)
	f.workflow = "disabled_manually"
	_, errs, err := cmdtest.Dockhand(t, "check", "--on", "github")
	require.Error(t, err)
	require.Contains(t, errs+err.Error(), "ada/macports-ports's main.yml workflow is disabled_manually, so GitHub won't start a check's run; enable the fork's Actions: https://github.com/ada/macports-ports/actions")
	require.Empty(t, f.runs)
	require.Empty(t, checkBranches(t, f), "nothing was pushed")

	// setup's github line reads the same, signed in.
	cmdtest.Setenv(t, "GH_TOKEN", "ghp_"+strings.Repeat("a", 36))
	out, _, _ := cmdtest.Dockhand(t, "setup", "--yes")
	require.Contains(t, cmdtest.Flat(out), "ada/macports-ports's main.yml workflow is disabled_manually")
	f.workflow = "active"
	out, _, _ = cmdtest.Dockhand(t, "setup", "--yes")
	require.Contains(t, cmdtest.Flat(out), "ada/macports-ports's Actions are enabled")
}

// A job GitHub cancelled before any runner took it has no log, so none is
// asked for, where it read "job log: fetch: HTTP 404" (the M1's B9 on
// rc4, while GitHub's macOS runners were down). Its release's environment
// is left unrecorded, with GitHub's word on why, and the others recorded,
// where the whole matrix was tried again three times and never recorded
// (rc8's GitHub rows; the M1's suggestion, 2026-10-29); submit names the
// release, and what checks it. A run where no job ran is tried again, as
// W1's was (Codex's review of 2ad896d2).
func TestAJobNoRunnerTookIsSaid(t *testing.T) {
	t.Parallel()
	f, _ := githubBranch(t)
	f.logs = []map[string]string{{"build (macos-14)": built("jq", false), "build (macos-15)": ""}}
	f.untaken = map[string]bool{"build (macos-15)": true}
	f.conclusion = []string{"cancelled"}
	out, errs, _ := cmdtest.Dockhand(t, "check", "--on", "github", "-v")
	require.NotContains(t, errs+out, "HTTP 404")
	require.Contains(t, errs, `github macOS 15 (Sequoia) arm64: the job never ran: GitHub says "The job was not acquired by Runner of type hosted even after multiple attempts"`)
	require.Regexp(t, `\n  jq +✓ tests passed( \(\d+s\))? +! not run\n`, out, "macOS 14's result is recorded")
	require.Zero(t, f.reruns, "no attempt is spent on it")
	_, _, err := cmdtest.Dockhand(t, "tidy", "-y")
	require.NoError(t, err)
	plan, _, _ := cmdtest.Dockhand(t, "submit", "--plan")
	require.Contains(t, cmdtest.Flat(plan), `✗ jq has changed, but these files haven't been built on github macOS 15 (Sequoia) arm64: the job never ran: GitHub says "The job was not acquired by Runner of type hosted even after multiple attempts"; check that release with Tart, local, or a command provider, or try again later.`)

	f, _ = githubBranch(t)
	f.logs = []map[string]string{{"build (macos-15)": ""}, {"build (macos-15)": ""}, {"build (macos-15)": ""}}
	f.untaken = map[string]bool{"build (macos-15)": true}
	f.conclusion = []string{"cancelled", "cancelled", "cancelled"}
	_, errs, err = cmdtest.Dockhand(t, "check", "--on", "github")
	require.Error(t, err)
	require.Contains(t, errs+err.Error(), "GitHub cancelled macos-15's job before any runner took it, so nothing was built; see https://github.com/ada/macports-ports/actions/runs/")
	require.NotContains(t, errs+err.Error(), "HTTP 404")
}

func TestGitHubRunsAFailureNoPortExplainsAgain(t *testing.T) {
	t.Parallel()
	f, _ := githubBranch(t)
	// The first attempt dies before listing a port; the rerun builds jq.
	f.logs = []map[string]string{both("2026-09-25T10:00:00.0Z ##[error]bootstrap failed\n"), both(built("jq", false))}
	f.conclusion = []string{"failure", "success"}

	_, errs, err := cmdtest.Dockhand(t, "check", "--on", "github", "-v")
	require.NoError(t, err, errs)
	require.Contains(t, errs, "ended failure without building a port")
	require.Contains(t, errs, "unsuccessful jobs again")
	require.Contains(t, errs, "github macOS 14 (Sonoma) arm64: jq passed")
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
	t.Parallel()
	f, _ := githubBranch(t)
	stuck := both("2026-09-25T10:00:00.0Z ##[error]bootstrap failed\n")
	f.logs = []map[string]string{stuck, stuck, stuck}
	f.conclusion = []string{"failure", "failure", "failure"}

	_, errs, err := cmdtest.Dockhand(t, "check", "--on", "github", "-v")
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
	t.Parallel()
	f, _ := githubBranch(t)
	f.logs = []map[string]string{both(built("jq", false))}
	f.conclusion = []string{"success"}
	_, errs, err := cmdtest.Dockhand(t, "check", "--on", "github")
	require.NoError(t, err, errs)
	require.Empty(t, checkBranches(t, f))

	out, errs, err := cmdtest.Dockhand(t, "retry", "check-1", "-v")
	require.NoError(t, err, errs)
	require.Contains(t, out, "check-2 repeats check-1")
	require.Contains(t, errs, "github: pushing to dockhand-check/")
	require.Contains(t, errs, "github: in progress https://github.com/ada/macports-ports/actions/runs/8\n")
	require.NotContains(t, errs, "actions/runs/7", "the earlier check's run isn't this one's")
	require.Contains(t, errs, "github macOS 14 (Sonoma) arm64: jq passed")
	require.Len(t, f.runs, 2)
	require.Equal(t, f.runs[0].branch, f.runs[1].branch)
	require.Empty(t, checkBranches(t, f))

	logs, _, err := cmdtest.Dockhand(t, "logs", "check-2")
	require.NoError(t, err)
	require.Contains(t, logs, "\n    https://github.com/ada/macports-ports/actions/runs/8\n")
	logs, _, err = cmdtest.Dockhand(t, "logs", "check-1")
	require.NoError(t, err)
	require.Contains(t, logs, "\n    https://github.com/ada/macports-ports/actions/runs/7\n")
	logged, _, err := cmdtest.Dockhand(t, "logs", "check-1", "--port", "jq")
	require.NoError(t, err)
	require.Contains(t, logged, "##[group]Installing jq")
	// From the port's own install, by the workflow's headings (D20).
	require.Contains(t, logged, "  dependencies  line 4\n  install       line 6\n  test          line 8\n")
	require.Contains(t, logged, "Showing from line 6, where the build for jq begins")
	require.NotContains(t, logged, "Installing dependencies for jq", "before it")
	// The port named after the check is the one -p names (field testing,
	// 2026-10-02).
	named, _, err := cmdtest.Dockhand(t, "logs", "check-1", "jq")
	require.NoError(t, err)
	require.Equal(t, logged, named)
	_, _, err = cmdtest.Dockhand(t, "logs", "check-1", "jq", "--port", "fd")
	require.EqualError(t, err, "name a check or a branch, not both")
}

// A serve that stops leaves the run going and its branch on the fork, so
// the next driver finds that run, rather than pushing again and starting
// another, and removes the branch once it has read it.
func TestAResumedGitHubCheckReadsTheRunItLeft(t *testing.T) {
	t.Parallel()
	f, _ := githubBranch(t)
	f.logs = []map[string]string{both(built("jq", false)), both(built("jq", false))}
	f.conclusion = []string{"success", "success"}
	_, _, err := cmdtest.Dockhand(t, "check", "--on", "github", "-d")
	require.NoError(t, err)

	ctx := t.Context()
	e, err := cmdtest.TestSettings(t).Open(ctx)
	require.NoError(t, err)
	defer e.Close()
	run, err := e.RunNamed(ctx, "check-1")
	require.NoError(t, err)
	serving, err := e.Session(ctx, model.SessionServe)
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
	_, errs, err := cmdtest.Dockhand(t, "wait", "check-1", "-v")
	require.NoError(t, err, errs)
	require.Contains(t, errs, "github macOS 14 (Sonoma) arm64: jq passed")
	require.Len(t, f.runs, 1, "the run it left, not another")
	require.Equal(t, 0, f.reruns)
	require.Empty(t, checkBranches(t, f))
}

func TestGitHubReadsAFailedPortsPhase(t *testing.T) {
	t.Parallel()
	f, _ := githubBranch(t)
	f.logs = []map[string]string{{"build (macos-14)": built("jq", false), "build (macos-15)": "2026-09-25T10:00:01.0Z ##[group]Listing subports\n2026-09-25T10:00:01.1Z jq\n2026-09-25T10:00:01.2Z ##[endgroup]\n2026-09-25T10:04:00.0Z ##[error]Failed to install jq\n"}}
	f.conclusion = []string{"failure"}

	_, errs, err := cmdtest.Dockhand(t, "check", "--on", "github", "-v")
	require.Error(t, err)
	require.Contains(t, errs, "github macOS 15 (Sequoia) arm64: jq failed at install")
	require.Equal(t, 0, f.reruns)
}

// Tested on gives the macOS release of each of the workflow's runners, as
// the label its job ran on names it in GitHub's jobs API, in one order
// whichever GitHub lists them in; it says nothing of their Xcode, which
// only their logs' text gives (the sshuttle run's finding 8).
func TestTestedOnGivesTheGitHubRunnersReleases(t *testing.T) {
	t.Parallel()
	f, _ := githubBranch(t)
	f.logs = []map[string]string{{"build (macos-14)": built("jq", false), "build (macos-15)": built("jq", false)}}
	f.conclusion = []string{"success"}
	_, errs, err := cmdtest.Dockhand(t, "check", "--on", "github")
	require.NoError(t, err, errs)
	_, _, err = cmdtest.Dockhand(t, "tidy")
	require.NoError(t, err)

	previewed, _, err := cmdtest.Dockhand(t, "submit", "--plan", "-v")
	require.NoError(t, err)
	require.Contains(t, previewed, "\n    ###### Tested on\n\n    macOS 14, 15\n"+
		"    Developer tools not recorded · github: MacPorts' CI workflow in the author's fork (Run ID:\n      https://github.com/ada/macports-ports/actions/runs/7 - checked in check-1)\n")
}

func TestCancelingAGitHubCheckCancelsItsRun(t *testing.T) {
	t.Parallel()
	f, _ := githubBranch(t)
	f.logs = []map[string]string{{"build (macos-14)": built("jq", false)}}
	f.conclusion = []string{"success"}
	asked := false
	f.hold = func() bool {
		if !asked {
			asked = true
			_, _, err := cmdtest.Dockhand(t, "cancel", "check-1")
			require.NoError(t, err)
		}
		return true
	}

	_, errs, err := cmdtest.Dockhand(t, "check", "--on", "github")
	require.Error(t, err, errs)
	require.Equal(t, 1, f.canceled)
	// Its run isn't read, and a retry runs the run again, so the branch
	// is left for clean.
	require.NotEmpty(t, checkBranches(t, f))
}

// A run that builds past its bound is cancelled, and the port it was
// installing when it ended fails, said as timed out, where it read as
// passed, an install marked only where it fails (D16).
func TestAGitHubRunPastItsBoundIsCancelled(t *testing.T) {
	t.Parallel()
	f, w := githubBranch(t)
	config, err := os.OpenFile(filepath.Join(w.Home, ".dockhand", "config.toml"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	require.NoError(t, err)
	_, err = config.WriteString("\n[providers.github]\nbuild_timeout = \"1m\"\n")
	require.NoError(t, err)
	require.NoError(t, config.Close())
	f.logs = []map[string]string{both("2026-09-25T10:00:01.0Z ##[group]Listing subports\n2026-09-25T10:00:01.1Z jq\n2026-09-25T10:00:01.2Z ##[endgroup]\n" +
		"2026-09-25T10:01:31.0Z ##[group]Installing jq\n2026-09-25T10:01:32.0Z checking for gcc... gcc\n")}
	f.conclusion = []string{"success"}
	f.hold = func() bool { return true }

	out, errs, err := cmdtest.Dockhand(t, "check", "--on", "github", "-v")
	require.Error(t, err, errs)
	require.Equal(t, 1, f.canceled)
	require.Contains(t, errs, "built past its 1m0s bound; cancelling it")
	require.Regexp(t, `\n  jq +✗ failed at install( \(\d+s\))? +✗ failed at install( \(\d+s\))?\n`, out)
	logs, _, err := cmdtest.Dockhand(t, "logs", "check-1")
	require.NoError(t, err)
	require.Contains(t, logs, "on build (macos-14): the run exceeded the 1m0s build timeout (providers.github.build_timeout) and was cancelled", errs)
}

// A branch the check couldn't remove from your fork is said once, doesn't
// fail the check, and is left for clean, which removes it once the branch
// is merged.
func TestCleanRemovesTheCheckBranchesLeftInYourFork(t *testing.T) {
	t.Parallel()
	f, w := githubBranch(t)
	f.logs = []map[string]string{both(built("jq", false))}
	f.conclusion = []string{"success"}
	refuseDeletions(t, f.fork)
	_, errs, err := cmdtest.Dockhand(t, "check", "--on", "github", "-v")
	require.NoError(t, err, errs)
	checked := checkBranches(t, f)
	require.NotEmpty(t, checked)
	require.Equal(t, 1, strings.Count(errs, "github: couldn't remove "+checked+" from ada/macports-ports, which dockhand clean removes once the branch is merged: "), errs)
	require.NotContains(t, errs, "github: removed")
	require.NoError(t, os.Remove(filepath.Join(f.fork, "hooks", "pre-receive")))

	_, _, err = cmdtest.Dockhand(t, "tidy")
	require.NoError(t, err)
	_, _, err = cmdtest.Dockhand(t, "submit", "--no-check", "--yes")
	require.NoError(t, err)
	g := cmdtest.Stands(t).Forge(nil).(*forgetest.GitHub)
	g.PRs[34901].State = forge.PullRequestMerged
	cmdtest.Setenv(t, "MACPORTS_TREE", w.Clone)
	_, _, err = cmdtest.Dockhand(t, "status", "--refresh")
	require.NoError(t, err)

	out, _, err := cmdtest.Dockhand(t, "clean")
	require.Equal(t, term.ExitFailure, term.ExitCode(err))
	require.Contains(t, out, "  remove   ada/macports-ports:"+checked+"\n")
	_, _, err = cmdtest.Dockhand(t, "clean", "--yes")
	require.NoError(t, err)
	require.Empty(t, strings.TrimSpace(testsupport.Git(t, f.fork, "for-each-ref", "refs/heads/dockhand-check/")))
}

// MacPorts' workflow reports a port whose build passed and whose tests
// failed; under --tests required that fails the check, as it does on
// Tart. Under --tests skip the plan says the workflow's tests still run.
// (The architecture review of 2026-09-27, finding 1.)
func TestRequiredTestsFailACheckOnGitHub(t *testing.T) {
	t.Parallel()
	f, _ := githubBranch(t)
	f.logs = []map[string]string{{"build (macos-14)": built("jq", false), "build (macos-15)": built("jq", true)}}
	f.conclusion = []string{"success"}
	out, _, err := cmdtest.Dockhand(t, "check", "--on", "github", "--tests", "required")
	require.Error(t, err, "a failing test fails a check that requires tests")
	require.Regexp(t, `\n  jq +✓ tests passed( \(\d+s\))? +✗ failed at test( \(\d+s\))?\n`, out)

	out, _, err = cmdtest.Dockhand(t, "check", "--on", "github", "--tests", "skip", "--plan")
	require.NoError(t, err)
	require.Contains(t, out, "Provider    github macOS 14 (Sonoma) arm64; github macOS 15 (Sequoia) arm64 · tests skipped\n            github runs its workflow's own tests; with --tests skip they run there, and don't count\n")
}

// A GitHub runner's log without the workflow's headings is printed whole,
// and said to be (D20).
func TestAGitHubLogWithoutHeadingsIsPrintedWholeAndSaid(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	result := model.TargetResult{Target: "jq", Log: "build-macos-15.log", Builders: []model.BuilderResult{{Builder: "build (macos-15)"}}}
	require.NoError(t, view.WritePortLog(&out, cmdtest.TestPlaces(t), result, []byte("a log\n"), false))
	require.Equal(t, "No build-step markers were found for jq. Showing the full log.\n\na log\n", out.String())
}
