package command

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/provider/actions"
)

// fakeActions stands in for GitHub Actions in your fork: a push of a
// dockhand-check branch starts one run, which completes on the second look
// with the logs given for its attempt.
type fakeActions struct {
	fork string
	// logs are each attempt's jobs' logs, by job name.
	logs       []map[string]string
	conclusion []string
	run        *actions.Run
	looks      int
	reruns     int
}

func (f *fakeActions) pushed(branch, commit string) bool {
	out, err := exec.Command("git", "--git-dir", f.fork, "rev-parse", "--verify", "-q", "refs/heads/"+branch).Output()
	return err == nil && strings.TrimSpace(string(out)) == commit
}

func (f *fakeActions) Runs(_ context.Context, repository, branch, commit string) ([]actions.Run, error) {
	if repository != "ada/macports-ports" || !f.pushed(branch, commit) {
		return nil, nil
	}
	if f.run == nil {
		f.run = &actions.Run{ID: 7, Attempt: 1, Status: "queued", URL: "https://github.com/ada/macports-ports/actions/runs/7"}
	}
	return []actions.Run{*f.run}, nil
}

func (f *fakeActions) Run(_ context.Context, _ string, id int64) (actions.Run, error) {
	f.looks++
	switch f.run.Status {
	case "queued":
		f.run.Status = "in_progress"
	case "in_progress":
		f.run.Status, f.run.Conclusion = "completed", f.conclusion[f.run.Attempt-1]
	}
	return *f.run, nil
}

func (f *fakeActions) Rerun(_ context.Context, _ string, id int64) error {
	f.reruns++
	f.run.Attempt++
	f.run.Status, f.run.Conclusion = "queued", ""
	return nil
}

func (f *fakeActions) Jobs(_ context.Context, _ string, id int64, attempt int) ([]actions.RunnerJob, error) {
	var jobs []actions.RunnerJob
	for i, name := range []string{"build (macos-14)", "build (macos-15)"} {
		if _, ok := f.logs[attempt-1][name]; ok {
			jobs = append(jobs, actions.RunnerJob{ID: int64(attempt*10 + i), Name: name, Status: "completed"})
		}
	}
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
func githubBranch(t *testing.T) *fakeActions {
	t.Helper()
	w := newWorld(t)
	versioned(t, w)
	withBumper(t)
	_, _, err := dockhand(t, "start", "jq-update")
	require.NoError(t, err)
	t.Setenv("MACPORTS_TREE", filepath.Join(w.home, "src", "macports-branches", "jq-update"))
	_, _, err = dockhand(t, "update", "jq")
	require.NoError(t, err)
	testPortReader = onePort{}
	t.Cleanup(func() { testPortReader = nil })
	g := withGitHub(t, w)
	f := &fakeActions{fork: g.fork}
	withActions(t, f)
	return f
}

func TestGitHubBuildsWithMacPortsWorkflowInYourFork(t *testing.T) {
	f := githubBranch(t)
	f.logs = []map[string]string{{"build (macos-14)": built("jq", false), "build (macos-15)": built("jq", true)}}
	f.conclusion = []string{"success"}

	out, _, err := dockhand(t, "check", "--on", "github", "--plan")
	require.NoError(t, err)
	require.Contains(t, out, "Provider    github · tests declared\nPushes      the revision to a dockhand-check/ branch of your fork, where MacPorts' workflow builds it\n")
	_, _, err = dockhand(t, "check", "--on", "github:sonoma")
	require.ErrorContains(t, err, "takes no releases")

	out, errs, err := dockhand(t, "check", "--on", "github")
	require.NoError(t, err, errs)
	require.Contains(t, errs, "github: pushing to dockhand-check/")
	require.Contains(t, errs, "github: in progress https://github.com/ada/macports-ports/actions/runs/7")
	require.Contains(t, errs, "github: jq passed")
	require.Contains(t, out, "  jq  ✓ built; tests failed (advisory)\n")
	require.Contains(t, out, "Passed for snapshot 1.")
	require.Equal(t, 0, f.reruns)

	// The commit is on your fork, and each runner's log beside the run's.
	branches := strings.TrimSpace(gitRun(t, f.fork, "for-each-ref", "--format=%(refname:short)", "refs/heads/dockhand-check/"))
	require.Regexp(t, `^dockhand-check/[0-9a-f]{12}$`, branches)
	logs, _, err := dockhand(t, "logs", "check-1")
	require.NoError(t, err)
	require.Contains(t, logs, "build-macos-15.log")
}

func TestGitHubRunsAFailureNoPortExplainsAgain(t *testing.T) {
	f := githubBranch(t)
	// The first attempt dies before listing a port; the rerun builds jq.
	f.logs = []map[string]string{{"build (macos-14)": "2026-09-25T10:00:00.0Z ##[error]bootstrap failed\n"}, {"build (macos-14)": built("jq", false)}}
	f.conclusion = []string{"failure", "success"}

	_, errs, err := dockhand(t, "check", "--on", "github")
	require.NoError(t, err, errs)
	require.Contains(t, errs, "ended failure without building a port")
	require.Contains(t, errs, "unsuccessful jobs again")
	require.Contains(t, errs, "github: jq passed")
	require.Equal(t, 1, f.reruns)
}

func TestGitHubReadsAFailedPortsPhase(t *testing.T) {
	f := githubBranch(t)
	f.logs = []map[string]string{{"build (macos-14)": built("jq", false), "build (macos-15)": "2026-09-25T10:00:01.0Z ##[group]Listing subports\n2026-09-25T10:00:01.1Z jq\n2026-09-25T10:00:01.2Z ##[endgroup]\n2026-09-25T10:04:00.0Z ##[error]Failed to install jq\n"}}
	f.conclusion = []string{"failure"}

	_, errs, err := dockhand(t, "check", "--on", "github")
	require.Error(t, err)
	require.Contains(t, errs, "github: jq failed at install")
	require.Equal(t, 0, f.reruns)
}
