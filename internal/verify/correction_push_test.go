package verify

import (
	"strings"
	"testing"

	"github.com/herbygillot/dockhand/internal/record"
	"github.com/stretchr/testify/require"
)

// A forge verification pushes the candidate to the contribution's fork branch,
// and refuses to disturb a head it was not told to replace. A correction to a
// published contribution names the pull request's head. One to an unpublished
// contribution names nothing, although a previous verification of that same
// contribution pushed its commit there, so the commit being corrected is what
// the replacement authorizes.
func TestACorrectionAuthorizesReplacingTheCommitItCorrects(t *testing.T) {
	t.Parallel()
	previous := record.ObjectID(strings.Repeat("a", 40))
	published := record.ObjectID(strings.Repeat("b", 40))
	target := record.Target{Name: "jq", Portfile: "sysutils/jq/Portfile"}
	revision := record.Revision{ID: "revision", ChangeID: "change", Source: record.Source{Tree: "tree", Commit: record.ObjectID(strings.Repeat("c", 40))}}
	config := record.BuildConfig{Provider: "github", Platform: record.Platform{OS: "darwin", Version: "25", Architecture: "arm64"}, EnvironmentDigest: "workflow", Tests: record.TestWorkflow}
	build := func(correction *record.CorrectionSpec) record.BuildSpec {
		job := record.Job{ID: "job", ChangeID: "change", Phase: record.PhaseVerification, ResultRevision: revision.ID,
			Prepared: &record.PreparedChange{Branch: "candidate", Source: revision.Source},
			Spec: record.JobSpec{Action: record.Amend, Source: revision.Source, Targets: []record.Target{target},
				Verification: record.VerificationRequired, Destination: record.VerificationComplete,
				Preparation: &record.PreparationSpec{SourceBranch: "master", Correction: correction}}}
		_, builds, err := PlanWithConfig(job, revision, config)
		require.NoError(t, err)
		require.Len(t, builds, 1)
		return builds[0]
	}

	// Published: the pull request's head is what may be replaced.
	require.Equal(t, published, build(&record.CorrectionSpec{ChangeID: "change", RevisionID: revision.ID, Branch: "candidate", PreviousHead: previous, RemoteHead: published}).ReplaceRemoteHead)

	// Unpublished: the commit being corrected, which a previous verification
	// of this contribution is what put on the fork branch.
	require.Equal(t, previous, build(&record.CorrectionSpec{ChangeID: "change", RevisionID: revision.ID, Branch: "candidate", PreviousHead: previous}).ReplaceRemoteHead)

	// Not a correction: nothing is authorized, and an unexpected head stands.
	job := record.Job{ID: "job", ChangeID: "change", Phase: record.PhaseVerification,
		Spec: record.JobSpec{Action: record.Verify, InputRevision: revision.ID, Source: revision.Source, Targets: []record.Target{target},
			Verification: record.VerificationRequired, Destination: record.VerificationComplete}}
	_, builds, err := PlanWithConfig(job, revision, config)
	require.NoError(t, err)
	require.Empty(t, builds[0].ReplaceRemoteHead)
}
