package workflow

import (
	"strings"
	"testing"

	"github.com/herbygillot/dockhand/internal/record"
	"github.com/stretchr/testify/require"
)

func validSpec(action record.Action) record.JobSpec {
	platform := record.Platform{OS: "darwin", Version: "25", Architecture: "arm64"}
	source := record.Source{Tree: record.ObjectID(strings.Repeat("a", 40)), Commit: record.ObjectID(strings.Repeat("b", 40)), Base: record.ObjectID(strings.Repeat("b", 40))}
	spec := record.JobSpec{Action: action, Source: source, Targets: []record.Target{{Name: "fixture", Portfile: "devel/fixture/Portfile"}}, Destination: record.VerificationComplete, Verification: record.VerificationRequired,
		Build: &record.BuildConfig{Provider: "test", Platform: platform, EnvironmentDigest: "fixture", Tests: record.TestDeclared}}
	if action.Prepares() {
		spec.Preparation = &record.PreparationSpec{SourceBranch: "master", Platform: platform, Author: record.CommitIdentity{Name: "Fixture", Email: "fixture@example.invalid"}}
	}
	return spec
}

// The action-rule table is the one place that says which parts of a spec
// belong to which action; each row is exercised here rather than through a
// complete submission per case.
func TestActionRulesDecideWhatEachActionAccepts(t *testing.T) {
	t.Parallel()
	for _, action := range []record.Action{record.Bump, record.BumpRevision, record.RefreshChecksums, record.Verify, record.Publish, record.Amend, record.Rebase} {
		_, ok := actionRules[action]
		require.True(t, ok, "%s has a rule", action)
	}
	_, err := normalizeSpec(record.JobSpec{Action: "sideways"})
	require.ErrorContains(t, err, `unknown action "sideways"`)

	for _, action := range []record.Action{record.Bump, record.BumpRevision, record.RefreshChecksums} {
		_, err := normalizeSpec(validSpec(action))
		require.NoError(t, err, action)
	}
	_, err = normalizeSpec(validSpec(record.Verify))
	require.NoError(t, err)

	bump := validSpec(record.Bump)
	bump.Version = "1.2.3"
	bump.Preparation.SharedRelease = true
	_, err = normalizeSpec(bump)
	require.NoError(t, err, "a bump selects a version and may share its release")

	revision := validSpec(record.BumpRevision)
	revision.Version = "1.2.3"
	_, err = normalizeSpec(revision)
	require.ErrorContains(t, err, "only bump accepts a nonempty version")
	revision = validSpec(record.BumpRevision)
	revision.Preparation.SharedRelease = true
	_, err = normalizeSpec(revision)
	require.ErrorContains(t, err, "shared release applies only to bump")

	verify := validSpec(record.Verify)
	verify.Destination = record.Published
	_, err = normalizeSpec(verify)
	require.ErrorContains(t, err, "verify must request verification-complete")
	verify = validSpec(record.Verify)
	verify.FreshVerification = true
	_, err = normalizeSpec(verify)
	require.NoError(t, err, "verify may refresh evidence")
	fresh := validSpec(record.Bump)
	fresh.FreshVerification = true
	_, err = normalizeSpec(fresh)
	require.ErrorContains(t, err, "fresh verification requires verify")

	publish := validSpec(record.Publish)
	publish.Destination = record.Published
	_, err = normalizeSpec(publish)
	require.ErrorContains(t, err, "publish requires publication intent")
	publish.KeepFailed = true
	_, err = normalizeSpec(publish)
	require.ErrorContains(t, err, "keeping failed environments requires local verification")

	amend := validSpec(record.Amend)
	_, err = normalizeSpec(amend)
	require.ErrorIs(t, err, ErrInvalidRequest, "amend requires a correction")

	prepared := validSpec(record.Bump)
	prepared.Destination = record.Published
	_, err = normalizeSpec(prepared)
	require.ErrorContains(t, err, "publication destination required")
}
