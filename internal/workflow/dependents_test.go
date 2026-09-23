package workflow_test

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/herbygillot/dockhand/internal/publish"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/state"
	"github.com/herbygillot/dockhand/internal/state/sqlite"
	"github.com/herbygillot/dockhand/internal/verify"
	"github.com/herbygillot/dockhand/internal/workflow"
	"github.com/stretchr/testify/require"
)

type discoverFunc func(context.Context, record.Source, record.BuildConfig, []record.Target) (verify.Coverage, error)

func (fn discoverFunc) Discover(ctx context.Context, s record.Source, b record.BuildConfig, roots []record.Target) (verify.Coverage, error) {
	return fn(ctx, s, b, roots)
}

func dependentCoverage(source record.Source, config record.BuildConfig, roots []record.Target) verify.Coverage {
	result := verify.Coverage{Source: source, Platform: config.Platform}
	for i, target := range append(append([]record.Target{}, roots...), record.Target{Name: "downstream", Portfile: "devel/downstream/Portfile"}, record.Target{Name: "other", Portfile: "devel/other/Portfile"}) {
		item := verify.CoverageTarget{Target: target, Root: i < len(roots), Evaluation: &verify.TargetEvaluation{Source: source, Target: target, Platform: config.Platform, NeedsXcode: target.Name == "other"}}

		if !item.Root {
			item.Reasons = []string{roots[0].Name + ": depends_lib"}
			item.IndexedDependencies = []string{roots[0].Name, "external"}
		}
		result.Targets = append(result.Targets, item)
	}
	return result
}

func TestDependentCoverageResumesAndGatesPublication(t *testing.T) {
	t.Parallel()
	for _, scenario := range []string{"passed", "failed", "unread", "unevaluated", "all-unevaluated"} {
		t.Run(scenario, func(t *testing.T) {
			f, hosting, request := combinedFixture(t, record.BumpRevision)
			request.Spec.IncludeDependents = true
			override := *request.Spec.Build
			override.EnvironmentDigest = "sha256:xcode-image"
			override.ProviderConfig = []byte(`{"Image":"xcode-image"}`)
			request.Spec.TargetBuilds = map[string]record.BuildConfig{"other": override}
			var discovered atomic.Int64
			f.engine.Dependents = discoverFunc(func(ctx context.Context, source record.Source, config record.BuildConfig, roots []record.Target) (verify.Coverage, error) {
				discovered.Add(1)
				require.NoError(t, f.store.Update(ctx, f.repository, func(context.Context, state.Tx) error { return nil }), "discovery must run outside the writer")
				require.NotEqual(t, request.Spec.Source.Tree, source.Tree, "discover the prepared tree")
				coverage := dependentCoverage(source, config, roots)
				if scenario == "unread" {
					coverage.Problems = []string{"reverse index unread: hidden depends_lib"}
				}
				if scenario == "unevaluated" {
					coverage.Targets[1].Problem = "evaluation unavailable"
					coverage.Targets[1].Evaluation = nil
				}
				if scenario == "all-unevaluated" {
					for i := range coverage.Targets {
						coverage.Targets[i].Problem = "evaluation unavailable"
						coverage.Targets[i].Evaluation = nil
					}
				}
				return coverage, nil
			})
			id := prepareCombined(t, f, request)
			f.run(t, id)
			before := f.status(t, id)
			require.Empty(t, before.Jobs[0].Attempts)
			require.Len(t, before.Jobs[0].Plan.Targets, 3)
			require.Equal(t, override, before.Jobs[0].Job.Spec.TargetBuilds["other"])
			reopened, err := sqlite.Open(t.Context(), f.store.Path(), sqlite.Options{})
			require.NoError(t, err)
			defer reopened.Close()
			engine := *f.engine
			engine.State = state.Bind(reopened, f.registration)
			engine.Dependents = nil
			f.engine = &engine
			running := map[string]verify.Request{}
			var submitted []verify.Request
			f.provider.submit = func(_ context.Context, r verify.Request) (verify.Submission, error) {
				if len(running) > 0 {
					return verify.Submission{State: verify.AtCapacity}, nil
				}
				submitted = append(submitted, r)
				run := admitted(r.ID)
				running[run.Run.RunID] = r
				return run, nil
			}
			f.provider.observe = func(_ context.Context, run record.ProviderRun) (verify.Observation, error) {
				r := running[run.RunID]
				delete(running, run.RunID)
				result := verify.Observation{Run: run, State: record.AttemptFinished, Verdict: record.VerdictPassed, ObservedAt: f.now()}
				if scenario == "failed" && r.Spec.Target.Name == "downstream" {
					result.Verdict = record.VerdictFailed
					result.Failure = &record.Failure{Kind: "dependency", Package: "external", Attribution: "unknown", Phase: "build", Detail: "external dependency failed"}
				}
				return result, nil
			}
			for i := 0; i < 60; i++ {
				status := f.status(t, id)
				if status.Jobs[0].Job.State.Terminal() {
					break
				}
				f.run(t, id)
			}
			status := f.status(t, id)
			job := status.Jobs[0].Job
			require.True(t, job.State.Terminal(), "%+v", job)
			require.Equal(t, int64(1), discovered.Load())
			for _, r := range submitted {
				if r.Spec.Target.Name == request.Spec.Targets[0].Name {
					require.Empty(t, r.Spec.Preinstall)
				} else {
					require.Equal(t, request.Spec.Targets, r.Spec.Preinstall)
				}
				if r.Spec.Target.Name == "other" {
					require.True(t, r.Spec.Config.NeedsXcode)
					require.Equal(t, "sha256:xcode-image", r.Spec.Config.EnvironmentDigest)
					require.JSONEq(t, `{"Image":"xcode-image"}`, string(r.Spec.Config.ProviderConfig))
				} else {
					require.Equal(t, request.Spec.Build.EnvironmentDigest, r.Spec.Config.EnvironmentDigest)
				}
			}
			if scenario == "passed" {
				require.Equal(t, record.JobCompleted, job.State)
				require.Equal(t, 1, hosting.writes)
				require.Contains(t, status.Jobs[0].Publications[0].Spec.Desired.Body, "Dependent verification")
				require.Contains(t, status.Jobs[0].Publications[0].Spec.Desired.Body, "downstream: passed")
			} else {
				require.Zero(t, hosting.writes)
				if scenario == "failed" {
					require.Equal(t, record.JobFailed, job.State)
				} else {
					require.Equal(t, record.JobNeedsAttention, job.State)
				}
				if scenario != "all-unevaluated" {
					_, err := f.engine.BindPublication(t.Context(), workflow.PublicationRequest{ID: "standalone", Branch: job.Prepared.Branch, Options: publish.Options{}})
					require.Error(t, err, "standalone publication must not bypass failed cohort coverage")
				}
			}
		})
	}
}

func TestDependentDiscoveryClaimCanBeReplacedWithoutAdoptingStalePlan(t *testing.T) {
	t.Parallel()
	f, _, request := combinedFixture(t, record.BumpRevision)
	request.Spec.IncludeDependents = true
	entered, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int64
	f.engine.Dependents = discoverFunc(func(ctx context.Context, source record.Source, config record.BuildConfig, roots []record.Target) (verify.Coverage, error) {
		if calls.Add(1) == 1 {
			close(entered)
			<-release
		}
		return dependentCoverage(source, config, roots), nil
	})
	id := prepareCombined(t, f, request)
	first := startCycle(t.Context(), f.engine, id)
	receive(t, entered)
	require.Empty(t, f.run(t, id).Advanced)
	f.advance(2 * time.Minute)
	f.run(t, id)
	close(release)
	result := receive(t, first)
	require.NoError(t, result.err)
	require.NotEmpty(t, result.result.Problems)
	require.Contains(t, fmt.Sprint(result.result.Problems), "claim")
	require.Len(t, f.status(t, id).Jobs[0].Plan.Targets, 3)
}
