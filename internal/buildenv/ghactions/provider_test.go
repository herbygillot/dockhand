package ghactions

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/buildenv"
	"github.com/herbygillot/dockhand/internal/model"
)

// A port's result is its runners' together, and keeps each runner's part:
// failed where any failed, naming it, and passed where every runner that
// built it passed. A runner that listed the subports without it didn't
// build it, as MacPorts' workflow leaves a port off a macOS it doesn't
// support; a runner that stopped before listing, or never reached it,
// leaves no verdict yet.
func TestAPortsVerdictIsItsRunnersTogether(t *testing.T) {
	passed := map[string]*Built{"jq": {Listed: true, Installing: true, Installed: true, Tested: true}}
	failed := map[string]*Built{"jq": {Listed: true, Installing: true, Installed: true, Install: true}}
	unreached := map[string]*Built{"jq": {Listed: true}}
	elsewhere := map[string]*Built{"harbor": {Listed: true, Installing: true, Installed: true}}
	cut := map[string]*Built{"jq": {Listed: true, Installing: true}}
	on := func(name, log string, built map[string]*Built) runner {
		return runner{job: RunnerJob{Name: name}, log: log, built: built, listing: true}
	}

	result, ok := verdict("jq", []runner{on("macos-14", "14.log", passed), on("macos-15", "15.log", failed)})
	require.True(t, ok)
	require.Equal(t, model.TargetResult{Outcome: model.OutcomeFailed, Phase: model.PhaseInstall, Tests: model.TestsNone, Log: "15.log", Detail: "on macos-15",
		Builders: []model.BuilderResult{
			{Builder: "macos-14", Outcome: model.OutcomePassed, Tests: model.TestsPassed, Log: "14.log"},
			{Builder: "macos-15", Outcome: model.OutcomeFailed, Phase: model.PhaseInstall, Tests: model.TestsNone, Log: "15.log"},
		}}, result)

	result, ok = verdict("jq", []runner{on("macos-14", "14.log", passed), on("macos-15", "15.log", passed)})
	require.True(t, ok)
	require.Equal(t, model.OutcomePassed, result.Outcome)
	require.Equal(t, model.TestsPassed, result.Tests)
	require.Equal(t, "14.log", result.Log)
	require.Len(t, result.Builders, 2)

	result, ok = verdict("jq", []runner{on("macos-14", "14.log", elsewhere), on("macos-15", "15.log", passed)})
	require.True(t, ok, "a runner that listed the subports without it didn't build it")
	require.Equal(t, model.OutcomePassed, result.Outcome)
	require.Equal(t, "15.log", result.Log)
	require.Equal(t, model.BuilderResult{Builder: "macos-14", Outcome: model.OutcomeNotRun}, result.Builders[0])

	_, ok = verdict("jq", []runner{on("macos-14", "14.log", passed), on("macos-15", "15.log", unreached)})
	require.False(t, ok, "a runner that listed it and never reached it leaves no verdict")
	_, ok = verdict("jq", []runner{on("macos-14", "14.log", passed), on("macos-15", "15.log", cut)})
	require.False(t, ok, "an install that never ended isn't a pass, where it had read as one")
	capped := on("macos-15", "15.log", cut)
	capped.capped = "GitHub ended the job at its 6h0m0s cap on a job"
	result, ok = verdict("jq", []runner{on("macos-14", "14.log", passed), capped})
	require.True(t, ok)
	require.Equal(t, model.OutcomeFailed, result.Outcome)
	require.Equal(t, model.PhaseInstall, result.Phase)
	require.Equal(t, "on macos-15: GitHub ended the job at its 6h0m0s cap on a job", result.Detail, "what it was installing when its time ended failed")
	_, ok = verdict("jq", []runner{on("macos-14", "14.log", passed), {job: RunnerJob{Name: "macos-15"}, built: map[string]*Built{}}})
	require.False(t, ok, "a runner that stopped before listing the subports leaves no verdict")
	_, ok = verdict("jq", []runner{on("macos-14", "14.log", elsewhere)})
	require.False(t, ok, "no runner built it")
	_, ok = verdict("jq", nil)
	require.False(t, ok)

	require.Equal(t, "build-macos-14.log", logName("build (macos-14)"))
	require.Equal(t, "job.log", logName("()"))
}

// A runner's macOS release is what the labels its job ran on name, as
// GitHub's jobs API gives them: macos-15 is macOS 15, whatever the
// runner's size or architecture. A label that names no release says
// nothing, rather than a guess at what GitHub means by it now, and so do
// labels naming two, and a job no runner took.
func TestARunnersLabelsNameItsRelease(t *testing.T) {
	ran := func(labels ...string) RunnerJob { return RunnerJob{Labels: labels, RunnerName: "GitHub Actions 1000"} }
	for labels, want := range map[string]string{
		"macos-15":                             "15",
		"macos-26":                             "26",
		"macOS-14":                             "14",
		"macos-15-intel":                       "15",
		"macos-14-xlarge":                      "14",
		"macos-10.15":                          "10.15",
		"macos-latest":                         "",
		"macos-latest-large":                   "",
		"self-hosted macOS":                    "",
		"ubuntu-24.04":                         "",
		"macos-14 macos-15":                    "",
		"macos-15 self-hosted macos-15-xlarge": "15",
	} {
		require.Equal(t, want, ran(strings.Fields(labels)...).Release(), labels)
	}
	require.Empty(t, ran().Release())
	require.Empty(t, RunnerJob{Labels: []string{"macos-15"}}.Release(), "no runner took it")
}

// What the runners were is reported once their jobs are listed, in one
// order whichever GitHub lists them in, a skipped job left out; nothing is
// where no runner's labels name a release.
func TestTheRunnersReleasesAreReported(t *testing.T) {
	build := &observing{}
	require.NoError(t, observe(build, []RunnerJob{
		{Name: "macos-26", Labels: []string{"macos-26"}, RunnerName: "GitHub Actions 3"},
		{Name: "macos-14", Labels: []string{"macos-14"}, RunnerName: "GitHub Actions 1"},
		{Name: "macos-latest", Labels: []string{"macos-latest"}, RunnerName: "GitHub Actions 2"},
		{Name: "macos-13", Labels: []string{"macos-13"}, Conclusion: "skipped"},
	}))
	require.Equal(t, []model.Observed{{Builders: []model.BuilderObserved{{Builder: "macos-14", MacOS: "14"}, {Builder: "macos-26", MacOS: "26"}, {Builder: "macos-latest"}}}}, build.observed)

	build = &observing{}
	require.NoError(t, observe(build, []RunnerJob{{Name: "macos-latest", Labels: []string{"macos-latest"}, RunnerName: "GitHub Actions 2"}}))
	require.Empty(t, build.observed)
}

// observing is a build that keeps what it was told the environment is.
type observing struct {
	buildenv.Build
	observed []model.Observed
}

func (b *observing) Observe(observed model.Observed) error {
	b.observed = append(b.observed, observed)
	return nil
}

// A job that ran for a bound, but a minute, was ended for its time: one
// GitHub ended at its cap, or one dockhand cancelled for its bound, which a
// driver that restarted reads as that rather than running it again (batch
// 26's leftover). One that never started ran for nothing.
func TestAJobEndedForItsTime(t *testing.T) {
	started := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	job := func(ran time.Duration) RunnerJob { return RunnerJob{Started: started, Completed: started.Add(ran)} }
	require.True(t, ranFor(job(6*time.Hour), JobCap))
	require.True(t, ranFor(job(JobCap-30*time.Second), JobCap))
	require.False(t, ranFor(job(2*time.Hour), JobCap))
	require.True(t, ranFor(job(2*time.Hour), 2*time.Hour), "a shorter bound of dockhand's")
	require.False(t, ranFor(RunnerJob{}, JobCap))
}
