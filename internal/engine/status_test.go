package engine

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/branchstate"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/testsupport"
)

// A branch whose files' every change master has, as dockhand last fetched
// it, landed by another route: duckdb-cxx14's C++14 fix was committed to
// master while status still asked to commit it for review (cleaning up
// duckdb-cxx14, finding 1). Status reads the master kept, never fetching;
// a branch master hasn't caught up with, or one that changes nothing,
// isn't on master.
func TestABranchWhoseChangesLandedIsOnMaster(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e := f.open(t)
	branch, err := e.Start(t.Context(), StartRequest{Name: "duckdb-cxx14"})
	require.NoError(t, err)
	status, err := e.BranchStatus(t.Context(), branch)
	require.NoError(t, err)
	require.Empty(t, status.OnMaster, "it changes nothing")

	write(t, branch.Worktree, map[string]string{"textproc/jq/Portfile": "name jq\nversion 1.7.1\nconfigure.cxx_standard 2014\n"})
	status, err = e.BranchStatus(t.Context(), branch)
	require.NoError(t, err)
	require.Empty(t, status.OnMaster, "master doesn't have it")

	write(t, f.upstream, map[string]string{"textproc/jq/Portfile": "name jq\nversion 1.7.1\nconfigure.cxx_standard 2014\n", "devel/other/Portfile": "name other\n"})
	testsupport.Git(t, f.upstream, "add", "-A")
	testsupport.Git(t, f.upstream, "commit", "-q", "-m", "jq: fix build on macOS 12 and older")
	status, err = e.BranchStatus(t.Context(), branch)
	require.NoError(t, err)
	require.Empty(t, status.OnMaster, "status fetches nothing")
	master, err := e.fetchMaster(t.Context())
	require.NoError(t, err)
	status, err = e.BranchStatus(t.Context(), branch)
	require.NoError(t, err)
	require.Equal(t, master, status.OnMaster)

	write(t, branch.Worktree, map[string]string{"textproc/jq/Portfile": "name jq\nversion 1.7.1\nconfigure.cxx_standard 2017\n"})
	status, err = e.BranchStatus(t.Context(), branch)
	require.NoError(t, err)
	require.Empty(t, status.OnMaster, "master has another change")
}

// A running run's progress says what it's building, and the step, as the
// Tart provider words it; logs says so for a target with no log yet (the
// gnome-voice session's notes, 2026-10-20).
func TestARunningRunSaysWhatItsBuilding(t *testing.T) {
	logs := RunLogs{Run: model.Run{State: model.RunRunning}, Progress: "macOS 26 (Tahoe) arm64: building gnome-voice (2 of 3): configure; rust: no archive for macOS 26 arm64; building from source"}
	phase, ok := logs.Building("gnome-voice")
	require.True(t, ok)
	require.Equal(t, "configure", phase)
	_, ok = logs.Building("gnome")
	require.False(t, ok, "a name the building one starts with isn't it")
	logs.Progress = "macOS 26 (Tahoe) arm64: building gnome-voice"
	phase, ok = logs.Building("gnome-voice")
	require.True(t, ok)
	require.Empty(t, phase)
	logs.Run.State = model.RunPassed
	_, ok = logs.Building("gnome-voice")
	require.False(t, ok, "a run that ended builds nothing")
}

// A running target's log is found where the driver copies it as it grows,
// by the target's name, in the newest of the run's build directories, by
// its port's name too (the gnome-voice session's notes, 2026-10-20).
func TestARunningTargetsLogIsFoundWhereTheDriverCopiesIt(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e, _ := f.withPreparer(t)
	branch := twoPortBranch(t, e)
	capture, err := e.Capture(t.Context(), CaptureRequest{Branch: branch, Mode: CaptureHead})
	require.NoError(t, err)
	plan, err := e.PlanCheck(t.Context(), PlanRequest{Revision: capture.Revision, Environments: []model.Environment{tahoeArm}})
	require.NoError(t, err)
	queued, err := e.Enqueue(t.Context(), branch, plan, model.OriginPerson)
	require.NoError(t, err)
	path, err := e.LiveLog(t.Context(), queued.ID, "jq")
	require.NoError(t, err)
	require.Empty(t, path, "nothing copied yet")

	older := filepath.Join(e.LogDirectory(), queued.Name(), "command-1", "jq.log")
	newer := filepath.Join(e.LogDirectory(), queued.Name(), "command-2", "jq.log")
	for _, name := range []string{older, newer} {
		require.NoError(t, os.MkdirAll(filepath.Dir(name), 0o755))
		require.NoError(t, os.WriteFile(name, []byte("building jq\n"), 0o644))
	}
	require.NoError(t, os.Chtimes(older, time.Now().Add(-time.Hour), time.Now().Add(-time.Hour)))
	path, err = e.LiveLog(t.Context(), queued.ID, "jq")
	require.NoError(t, err)
	require.Equal(t, newer, path, "the newest attempt's")
}

// A step of a port's build is in its log from where it began to where the
// next began, the port's own next step or another target's in the same
// log, or to the end; a step run twice is its last run.
func TestStepLinesBoundAStep(t *testing.T) {
	t.Parallel()
	jq := model.TargetResult{Target: "jq", Log: "/logs/guest.log", Steps: []model.LogStep{{Name: model.StepFetch, Line: 10}, {Name: model.StepInstall, Line: 20}, {Name: model.StepFetch, Line: 30}}}
	other := model.TargetResult{Target: "oniguruma", Log: "/logs/guest.log", Steps: []model.LogStep{{Name: model.StepClean, Line: 40}}}
	apart := model.TargetResult{Target: "fd", Log: "/logs/fd.log", Steps: []model.LogStep{{Name: model.StepClean, Line: 35}}}
	execution := model.GuestExecution{ID: "tart_1"}
	logs := RunLogs{Executions: []ExecutionLogs{{Execution: execution, Results: []model.TargetResult{jq, other, apart}}}}
	log := PortLog{Execution: execution, Result: jq}

	from, to, ok := logs.StepLines(log, model.StepInstall)
	require.True(t, ok)
	require.Equal(t, [2]int{20, 30}, [2]int{from, to})
	from, to, ok = logs.StepLines(log, model.StepFetch)
	require.True(t, ok)
	require.Equal(t, [2]int{30, 40}, [2]int{from, to}, "its last run, to where the next target in the log begins")
	_, _, ok = logs.StepLines(log, model.StepTest)
	require.False(t, ok)

	logs.Executions[0].Results = []model.TargetResult{jq}
	from, to, _ = logs.StepLines(log, model.StepFetch)
	require.Equal(t, [2]int{30, 0}, [2]int{from, to}, "to the log's end")
}

// status reads the checkout's remotes once, however many branches it
// says: each branch read them again, five git processes, 3,415 of
// status --all's 7,372 over 683 branches (the M1's rc1 measure).
func TestStatusReadsTheRemotesOnce(t *testing.T) {
	t.Parallel()
	f := setup(t)
	trace := filepath.Join(t.TempDir(), "git.trace")
	f.options.Env = append(slices.Clone(f.options.Env), "GIT_TRACE="+trace)
	e := f.open(t)
	for _, name := range []string{"one", "two", "three"} {
		_, err := e.Start(t.Context(), StartRequest{Name: name})
		require.NoError(t, err)
	}
	require.NoError(t, os.WriteFile(trace, nil, 0o600))
	statuses, err := e.Status(t.Context())
	require.NoError(t, err)
	require.Len(t, statuses, 3)
	data, err := os.ReadFile(trace)
	require.NoError(t, err)
	require.Equal(t, 2, strings.Count(string(data), "git remote get-url"), "origin's fetch and push URLs, once")
}

// A passing check of this Mac's release alone names the check it didn't
// build where the update asks for it: minmax for a floor it raised, a
// variant whose sources it changed, and neither where it raises neither;
// a check that built minmax names none (the docs dig's I47).
func TestAPassingHostCheckNamesTheWiderCheck(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e, _ := f.withPreparer(t)
	branch := committedUpdate(t, e)
	checked(t, e, branch, model.OutcomePassed, model.OutcomePassed)
	status, err := e.BranchStatus(t.Context(), branch)
	require.NoError(t, err)
	status.Evidence.Plan.Profile = "host"
	require.Nil(t, e.wider(t.Context(), status, nil), "an update raising neither")

	raised := status
	raised.Changes = map[string]model.ChangeRecord{"textproc/jq": {Ports: []model.SubportChange{{Port: "jq", Fields: []model.FieldChange{{Field: "platforms", From: "darwin", To: "{darwin >= 20}"}}}}}}
	wider := e.wider(t.Context(), raised, nil)
	require.NotNil(t, wider)
	require.Equal(t, branchstate.Next{Verb: branchstate.VerbCheck, Args: []string{"--build", "minmax"}}, wider.Next)
	raised.Evidence.Plan.Profile = "minmax"
	require.Nil(t, e.wider(t.Context(), raised, nil), "minmax built it")
	raised.Evidence.Plan.Profile = "host"

	portfile := read(t, filepath.Join(branch.Worktree, "textproc/jq/Portfile"))
	write(t, branch.Worktree, map[string]string{"textproc/jq/Portfile": portfile + "\nvariant docs description {Docs} {\n    patchfiles-append docs.patch\n}\n", "textproc/jq/files/docs.patch": "--- a\n+++ b\n"})
	testsupport.Git(t, branch.Worktree, "add", "-A")
	testsupport.Git(t, branch.Worktree, "commit", "-q", "-m", "jq: update to 1.8.1, with docs")
	status, err = e.BranchStatus(t.Context(), branch)
	require.NoError(t, err)
	status.Evidence.Plan.Profile = "host"
	wider = e.wider(t.Context(), status, []string{"textproc/jq/Portfile", "textproc/jq/files/docs.patch"})
	require.NotNil(t, wider)
	require.Equal(t, branchstate.Next{Verb: branchstate.VerbCheck, Args: []string{"--variants", "+docs"}}, wider.Next)
	require.Equal(t, "the update changes the sources of jq's +docs, which a build of the default variants doesn't take", wider.Why)

	// A floor raised beside it: minmax first, then the variant once
	// minmax passed, then nothing once the variant's check passed.
	both := status
	evidence := *status.Evidence
	both.Evidence, both.Changes = &evidence, raised.Changes
	require.Equal(t, []string{"--build", "minmax"}, e.wider(t.Context(), both, []string{"textproc/jq/files/docs.patch"}).Next.Args)
	both.Evidence.Plan.Profile = "minmax"
	require.Equal(t, []string{"--variants", "+docs"}, e.wider(t.Context(), both, []string{"textproc/jq/files/docs.patch"}).Next.Args)
	both.Evidence.Plan.Variants = "+docs"
	require.Nil(t, e.wider(t.Context(), both, []string{"textproc/jq/files/docs.patch"}))

	// A branch that changes more than one port names the one by --only,
	// as --variants needs (variantsTarget).
	two := status
	evidence = *status.Evidence
	two.Evidence = &evidence
	two.Evidence.Plan.Targets = append(slices.Clone(two.Evidence.Plan.Targets), model.PlanTarget{ID: "jq-devel", Target: model.Target{Name: "jq-devel"}, Directory: "textproc/jq", Role: model.Changed})
	require.Equal(t, []string{"--variants", "+docs", "--only", "jq"}, e.wider(t.Context(), two, []string{"textproc/jq/files/docs.patch"}).Next.Args)
}
