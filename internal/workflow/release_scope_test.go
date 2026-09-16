package workflow_test

import (
	"context"
	"testing"

	"github.com/herbygillot/dockhand/internal/publish"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/state"
	"github.com/herbygillot/dockhand/internal/state/sqlite"
	"github.com/herbygillot/dockhand/internal/verify"
	"github.com/herbygillot/dockhand/internal/workflow"
	"github.com/herbygillot/dockhand/internal/workflow/preparation"
	"github.com/stretchr/testify/require"
)

func TestSharedReleaseResumesIsolatedCoverageAndGatesPublication(t *testing.T) {
	for _, scenario := range []string{"passed", "failed", "canceled"} {
		t.Run(scenario, func(t *testing.T) {
			f, hosting, request := combinedFixture(t, record.Bump)
			request.Spec.Preparation.SharedRelease = true
			original := f.engine.Preparer
			root := request.Spec.Targets[0]
			sibling := root
			sibling.Name = "fixture-sibling"
			sibling.Subport = sibling.Name
			metadata := root
			metadata.Name = "fixture-meta"
			metadata.Subport = metadata.Name
			scope := &record.ReleaseScope{Input: record.ReleaseInput{Portfile: root.Portfile, Before: "1", After: "2"}, Affected: []record.ReleaseMember{{Target: root}, {Target: sibling}, {Target: metadata, MetadataOnly: true}}}
			f.engine.Preparer = prepareFunc(func(ctx context.Context, r preparation.Request) (preparation.Result, error) {
				result, err := original.Prepare(ctx, r)
				result.Scope = scope
				return result, err
			})
			id := prepareCombined(t, f, request)
			f.run(t, id)
			before := f.status(t, id)
			require.Len(t, before.Jobs[0].Plan.Targets, 2)
			require.Len(t, before.Revisions[0].Scope.Affected, 3)
			reopened, err := sqlite.Open(t.Context(), f.store.Path(), sqlite.Options{})
			require.NoError(t, err)
			defer reopened.Close()
			engine := *f.engine
			engine.State = reopened
			engine.Preparer = nil
			f.engine = &engine
			f.provider.observe = func(_ context.Context, run record.ProviderRun) (verify.Observation, error) {
				verdict := record.VerdictPassed
				status := f.status(t, id)
				for _, attempt := range status.Jobs[0].Attempts {
					if attempt.Run.RunID == run.RunID && attempt.Spec.Target.Name == sibling.Name && scenario == "failed" {
						verdict = record.VerdictFailed
					}
				}
				return verify.Observation{Run: run, State: record.AttemptFinished, Verdict: verdict, ObservedAt: f.now()}, nil
			}
			if scenario == "canceled" {
				f.cancel(t, id)
			}
			for range 50 {
				if f.status(t, id).Jobs[0].Job.State.Terminal() {
					break
				}
				f.run(t, id)
			}
			status := f.status(t, id)
			job := status.Jobs[0].Job
			require.True(t, job.State.Terminal(), "%+v", job)
			for _, attempt := range status.Jobs[0].Attempts {
				require.Empty(t, attempt.Spec.Preinstall, "siblings can conflict and must run independently")
			}
			if scenario == "passed" {
				bound, err := f.engine.BindPublication(t.Context(), workflow.PublicationRequest{ID: "standalone", Branch: job.Prepared.Branch, Options: publish.Options{}})
				require.NoError(t, err)
				require.Len(t, bound.Branch.Scope.BuildTargets(), 2)
				err = f.store.Update(t.Context(), f.repository, func(ctx context.Context, tx state.Tx) error {
					revision, err := tx.Revision(ctx, job.ResultRevision)
					if err != nil {
						return err
					}
					revision.Previous = revision.ID
					revision.ID = "dropped_scope"
					revision.Scope.Affected = revision.Scope.Affected[:1]
					return tx.PutRevision(ctx, revision)
				})
				require.ErrorIs(t, err, state.ErrInvalid)
				require.Equal(t, record.JobCompleted, job.State)
				require.Equal(t, 1, hosting.writes)
				require.Contains(t, status.Jobs[0].Publications[0].Spec.Desired.Body, "Shared-release verification")
				require.Contains(t, status.Jobs[0].Publications[0].Spec.Desired.Body, "fixture-sibling: passed")
			} else {
				require.Zero(t, hosting.writes)
				_, err := f.engine.BindPublication(t.Context(), workflow.PublicationRequest{ID: "standalone", Branch: job.Prepared.Branch, Options: publish.Options{}})
				require.Error(t, err, "standalone publication cannot bypass incomplete shared coverage")

				rootOnly := f.request("root-only")
				rootOnly.Spec.InputRevision = ""
				rootOnly.Spec.Source = job.Prepared.Source
				rootOnly.Spec.Targets = []record.Target{root}
				rootOnly.Spec.Build = request.Spec.Build
				rootOnly.Spec.FreshVerification = true
				attempt := completeVerification(t, f, rootOnly, record.VerdictPassed)
				require.True(t, verify.Applicable(attempt.Spec, attempt).Matches)
				_, err = f.engine.BindPublication(t.Context(), workflow.PublicationRequest{ID: "after-root-only", Branch: job.Prepared.Branch, Options: publish.Options{}})
				require.ErrorContains(t, err, "missing shared-release target", "a later standalone pass cannot substitute for sibling coverage")
				require.Zero(t, hosting.writes)
			}
		})
	}
}
