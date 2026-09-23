package verify

import (
	"testing"

	"github.com/herbygillot/dockhand/internal/record"
	"github.com/stretchr/testify/require"
)

// Every target is its own question on every build platform: the targets on
// Build, then the same targets on each further platform, numbered across the
// plan so no two questions share an identifier.
func TestPlanBuildsEveryTargetOnEveryPlatform(t *testing.T) {
	t.Parallel()
	root := record.Target{Name: "py310-example", Portfile: "python/py-example/Portfile", Subport: "py310-example"}
	sibling := root
	sibling.Name, sibling.Subport = "py311-example", "py311-example"
	revision := record.Revision{ID: "revision", ChangeID: "change", Source: record.Source{Tree: "tree"}, Scope: &record.ReleaseScope{Affected: []record.ReleaseMember{{Target: root}, {Target: sibling}}}}
	tahoe := record.BuildConfig{Provider: "tart", Platform: record.Platform{OS: "darwin", Version: "25", Architecture: "arm64"}, EnvironmentDigest: "tahoe", Tests: record.TestDeclared}
	sonoma := tahoe
	sonoma.Platform.Version, sonoma.EnvironmentDigest = "23", "sonoma"
	job := record.Job{ID: "job", ChangeID: "change", Phase: record.PhaseVerification, Spec: record.JobSpec{Action: record.Verify, InputRevision: revision.ID, Source: revision.Source, Targets: []record.Target{root}, AllSubports: true, Verification: record.VerificationRequired, Destination: record.VerificationComplete, Build: &tahoe, PlatformBuilds: []record.BuildConfig{sonoma}}}

	plan, builds, err := Plan(job, revision)
	require.NoError(t, err)
	require.Len(t, builds, 4)
	require.Len(t, plan.Targets, 4)
	ids := map[record.TargetID]bool{}
	for i, target := range plan.Targets {
		want := tahoe
		if i >= 2 {
			want = sonoma
		}
		require.Equal(t, want.Platform, target.Platform)
		require.Equal(t, want.EnvironmentDigest, builds[i].Config.EnvironmentDigest)
		require.Equal(t, target.Port, builds[i].Target)
		require.Equal(t, i%2 == 0, target.Root)
		ids[target.ID] = true
	}
	require.Len(t, ids, 4)

	job.Spec.PlatformBuilds = nil
	job.Spec.AllSubports = false
	plan, builds, err = Plan(job, revision)
	require.NoError(t, err)
	require.Len(t, builds, 1)
	require.Equal(t, record.TargetID("target_job"), plan.Targets[0].ID, "one build on one platform keeps its identifier")
}
