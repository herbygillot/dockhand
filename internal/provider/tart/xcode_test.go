package tart

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/engine"
	"github.com/herbygillot/dockhand/internal/model"
)

// xcodeJob is tartJob with libharbor needing Xcode on Tahoe, and tree,
// which needs nothing of it, built too.
func xcodeJob(t *testing.T) engine.Job {
	job := tartJob(t, 1)
	job.Targets[0].NeedsXcode = []model.Platform{tahoe}
	job.Targets = append(job.Targets, model.PlanTarget{ID: "tree", Target: model.Target{Name: "tree", Portfile: "sysutils/tree/Portfile"}})
	job.Plan.Targets = job.Targets
	job.Plan.Environments = []model.Environment{job.Environment}
	return job
}

// A release whose targets need Xcode builds them all in its Xcode image
// when it has one (decision 23).
func TestTargetsThatNeedXcodeBuildInTheXcodeImage(t *testing.T) {
	t.Parallel()
	mac := newMac(guestResults{State: "finished"})
	mac.images = append(mac.images, "dockhand-xcode-tahoe")
	job := xcodeJob(t)
	require.NoError(t, testProvider(mac).Execute(t.Context(), job, &fakeBuild{}))
	vm := "dockhand-check-run-7-tahoe-1"
	require.Equal(t, []string{"clone dockhand-xcode-tahoe " + vm, "start " + vm, "reach " + vm + " as dockhand-xcode-tahoe", "delete " + vm}, mac.events)
	require.Len(t, mac.guest.input.Targets, 3)

	skips, err := testProvider(mac).Skips(t.Context(), job.Plan, job.Environment)
	require.NoError(t, err)
	require.Empty(t, skips)
}

// Without an Xcode image, a target that needs Xcode isn't built with the
// Command Line Tools alone: it's recorded as not run, with a log naming the
// command that makes the image, and so is what depends on it. The rest
// build in the base image.
func TestTargetsThatNeedXcodeAreSkippedWithoutTheXcodeImage(t *testing.T) {
	t.Parallel()
	mac := newMac(guestResults{State: "finished", Targets: []guestResult{{ID: "tree", Outcome: "passed", Log: "target-1.log"}}})
	job := xcodeJob(t)
	build := &fakeBuild{}
	require.NoError(t, testProvider(mac).Execute(t.Context(), job, build))

	require.Len(t, build.results, 3)
	require.Equal(t, model.TargetResult{Target: "libharbor", Outcome: model.OutcomeNotRun, Tests: model.TestsNone, Detail: "needs Xcode", Log: build.results[0].Log}, build.results[0])
	log, err := os.ReadFile(build.results[0].Log)
	require.NoError(t, err)
	require.Equal(t, "libharbor wasn't built on macOS 26 (Tahoe): needs Xcode.\n"+
		"There's no Xcode image for macOS 26 (Tahoe); dockhand providers setup tart tahoe --xcode <Xcode .xip, or a folder of them> makes one.\n", string(log))
	require.Equal(t, model.OutcomeNotRun, build.results[1].Outcome)
	require.Equal(t, "needs libharbor, which isn't built", build.results[1].Detail, "harbor-cli isn't built against an old libharbor")
	require.Equal(t, model.OutcomePassed, build.results[2].Outcome)
	require.Contains(t, build.progress, "libharbor: not built: needs Xcode; there's no Xcode image for macOS 26 (Tahoe); dockhand providers setup tart tahoe --xcode <Xcode .xip, or a folder of them> makes one")

	require.Len(t, mac.guest.input.Targets, 1, "only tree goes to the guest")
	require.Equal(t, "tree", mac.guest.input.Targets[0].ID)
	require.Contains(t, mac.events, "clone dockhand-base-tahoe dockhand-check-run-7-tahoe-1")

	skips, err := testProvider(mac).Skips(t.Context(), job.Plan, job.Environment)
	require.NoError(t, err)
	require.Len(t, skips, 1, "the engine adds the dependents")
	require.Equal(t, engine.Skip{Target: "libharbor", Environment: job.Environment, Reason: "needs Xcode",
		Remedy: "there's no Xcode image for macOS 26 (Tahoe); dockhand providers setup tart tahoe --xcode <Xcode .xip, or a folder of them> makes one"}, skips[0])
}

// With nothing left to build, no VM starts.
func TestNoVMStartsWhenEverythingIsSkipped(t *testing.T) {
	t.Parallel()
	mac := newMac()
	job := xcodeJob(t)
	job.Targets = job.Targets[:2]
	job.Plan.Targets = job.Targets
	build := &fakeBuild{}
	require.NoError(t, testProvider(mac).Execute(t.Context(), job, build))
	require.Len(t, build.results, 2)
	require.Empty(t, mac.events)
}
