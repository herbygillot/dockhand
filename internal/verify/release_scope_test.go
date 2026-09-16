package verify

import (
	"testing"

	"github.com/herbygillot/dockhand/internal/record"
	"github.com/stretchr/testify/require"
)

func TestSharedReleasePlansEveryBuildableSibling(t *testing.T) {
	root := record.Target{Name: "py310-example", Portfile: "python/py-example/Portfile", Subport: "py310-example"}
	sibling := root
	sibling.Name = "py311-example"
	sibling.Subport = sibling.Name
	metadata := root
	metadata.Name = "py-example"
	metadata.Subport = ""
	revision := record.Revision{ID: "revision", ChangeID: "change", Source: record.Source{Tree: "tree"}, Scope: &record.ReleaseScope{Affected: []record.ReleaseMember{{Target: root}, {Target: sibling, NeedsXcode: true}, {Target: metadata, MetadataOnly: true}}}}
	job := record.Job{ID: "job", ChangeID: "change", Phase: record.PhaseVerification, Spec: record.JobSpec{Action: record.Verify, InputRevision: revision.ID, Source: revision.Source, Targets: []record.Target{root}, Verification: record.VerificationRequired, Destination: record.VerificationComplete}}
	config := record.BuildConfig{Provider: "tart", Platform: record.Platform{OS: "darwin", Version: "25", Architecture: "arm64"}, EnvironmentDigest: "image", Tests: record.TestDeclared}
	plan, builds, err := PlanWithConfig(job, revision, config)
	require.NoError(t, err)
	require.Len(t, builds, 2)
	require.Len(t, plan.Targets, 2)
	require.True(t, plan.Targets[0].Root)
	require.False(t, plan.Targets[1].Root)
	require.True(t, builds[1].Config.NeedsXcode)
	for _, build := range builds {
		require.Empty(t, build.Preinstall)
		require.Equal(t, revision.Source, build.Source)
	}
	config.Provider = "github"
	_, _, err = PlanWithConfig(job, revision, config)
	require.ErrorContains(t, err, "isolated local verification")
}
