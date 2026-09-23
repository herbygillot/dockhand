package workflow_test

import (
	"context"
	"sync"
	"testing"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/verify"
	"github.com/herbygillot/dockhand/internal/workflow"
	"github.com/stretchr/testify/require"
)

var sonomaPlatform = record.Platform{OS: "darwin", Version: "23", Architecture: "arm64"}

// namedPlatforms resolves one build per named platform, as the provider
// choice does, from the fixture's own build.
func namedPlatforms(f *fixture, platforms ...record.Platform) workflow.BuildResolver {
	return func(context.Context, macports.Snapshot) (workflow.BuildResolution, error) {
		var builds []record.BuildConfig
		for _, platform := range platforms {
			build := *f.request("platform").Spec.Build
			build.Platform = platform
			builds = append(builds, build)
		}
		return workflow.BuildResolution{Build: &builds[0], PlatformBuilds: builds[1:]}, nil
	}
}

// A verification on named platforms evaluates on the host, builds on each
// platform named, and passes only when every one of them does.
func TestNamedPlatformsEachBuildAndAllMustPass(t *testing.T) {
	t.Parallel()
	for _, failing := range []bool{false, true} {
		f, _ := bindingFixture(t)
		f.provider.platforms = []record.Platform{buildPlatform, sonomaPlatform}
		var mu sync.Mutex
		platformOf := map[record.RequestID]record.Platform{}
		f.provider.submit = func(_ context.Context, r verify.Request) (verify.Submission, error) {
			mu.Lock()
			defer mu.Unlock()
			platformOf[r.ID] = r.Spec.Config.Platform
			return admitted(r.ID), nil
		}
		f.provider.observe = func(ctx context.Context, run record.ProviderRun) (verify.Observation, error) {
			mu.Lock()
			platform := platformOf[run.RequestID]
			mu.Unlock()
			verdict := record.VerdictPassed
			if failing && platform == sonomaPlatform {
				verdict = record.VerdictFailed
			}
			return terminal(f, verdict)(ctx, run)
		}

		request := bindRequest(f, "platforms")
		request.Build = record.BuildConfig{}
		request.Platform = buildPlatform
		request.Platforms = []record.Platform{sonomaPlatform, buildPlatform}
		request.ResolveBuild = namedPlatforms(f, sonomaPlatform, buildPlatform)
		bound, err := f.engine.BindVerification(t.Context(), request)
		require.NoError(t, err)
		require.Equal(t, buildPlatform, bound.Evaluation.Platform, "the targets are evaluated on the host")
		require.Equal(t, sonomaPlatform, bound.Request.Spec.Build.Platform)
		require.Len(t, bound.Request.Spec.PlatformBuilds, 1)

		receipt, err := f.engine.Submit(t.Context(), bound.Request)
		require.NoError(t, err)
		// The fixture's capacity is one, so the platforms take turns.
		for range 6 {
			if f.status(t, receipt.JobID).Jobs[0].Job.State.Terminal() {
				break
			}
			f.run(t, receipt.JobID)
		}
		status := f.status(t, receipt.JobID)
		job := status.Jobs[0]
		require.Len(t, job.Attempts, 2)
		built := []record.Platform{}
		for _, attempt := range job.Attempts {
			built = append(built, attempt.Spec.Config.Platform)
		}
		require.ElementsMatch(t, []record.Platform{sonomaPlatform, buildPlatform}, built)
		if failing {
			require.Equal(t, record.JobFailed, job.Job.State, "one failed platform fails the verification")
		} else {
			require.Equal(t, record.JobCompleted, job.Job.State)
		}

		request.Platforms = []record.Platform{sonomaPlatform}
		_, err = f.engine.BindVerification(t.Context(), request)
		require.ErrorContains(t, err, "named platforms", "a resolution must build on exactly what was named")
	}
}

// Named platforms were one verification's choice: a later verification
// that keeps the contribution's recorded settings builds on the evaluated
// platform with the build recorded before them.
func TestNamedPlatformsAreNotRecordedSettings(t *testing.T) {
	t.Parallel()
	f, _, input := combinedFixture(t, record.BumpRevision)
	id := prepareCombined(t, f, input)
	passCombined(t, f, id)

	named := bindRequest(f, "named")
	named.Tracked = continuing(t, f, workflow.ContributionSelector{Target: "fixture"})
	named.Build = record.BuildConfig{}
	named.Platform = buildPlatform
	named.Platforms = []record.Platform{sonomaPlatform}
	named.ResolveBuild = namedPlatforms(f, sonomaPlatform)
	bound, err := f.engine.BindVerification(t.Context(), named)
	require.NoError(t, err)
	require.Equal(t, sonomaPlatform, bound.Request.Spec.Build.Platform)
	_, err = f.engine.Submit(t.Context(), bound.Request)
	require.NoError(t, err)

	recorded := bindRequest(f, "recorded")
	recorded.Tracked = continuing(t, f, workflow.ContributionSelector{Target: "fixture"})
	recorded.Platform = buildPlatform
	recorded.UseRecordedBuild = true
	bound, err = f.engine.BindVerification(t.Context(), recorded)
	require.NoError(t, err)
	require.Equal(t, input.Spec.Build, bound.Request.Spec.Build)
	require.Empty(t, bound.Request.Spec.PlatformBuilds)
}
