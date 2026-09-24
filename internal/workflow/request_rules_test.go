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
	unverified := validSpec(record.Publish)
	unverified.Destination, unverified.Verification, unverified.Build = record.Published, record.VerificationSkipped, nil
	unverified.Publication = &record.PublicationSpec{Forge: "fixture", Repository: "macports/macports-ports", HeadRepository: "author/ports", HeadBranch: "candidate", BaseBranch: "master", PushURL: "https://example.invalid/ports.git", BaseURL: "https://example.invalid/base.git", LockDirectory: "/locks", Unverified: true, Desired: record.PublicationContent{Head: unverified.Source.Commit, Title: "fixture: update"}}
	_, err = normalizeSpec(unverified)
	require.NoError(t, err, "an explicitly unverified publication needs no build or evidence")
	unverified.Publication.Unverified = false
	_, err = normalizeSpec(unverified)
	require.Error(t, err, "skipped verification must be declared on the publication")
	unverified.Publication.Unverified, unverified.Publication.EvidenceAttempt = true, "attempt"
	_, err = normalizeSpec(unverified)
	require.Error(t, err, "an unverified publication cites no evidence")
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

// Further build platforms belong to a verification alone: local builds of
// the same targets under the same policies, each on a platform of its own,
// without dependent coverage, which is planned on one platform.
func TestPlatformBuildsAreAVerificationsOwn(t *testing.T) {
	t.Parallel()
	with := func(action record.Action, change func(*record.JobSpec, *record.BuildConfig)) error {
		spec := validSpec(action)
		further := *spec.Build
		further.Platform.Version = "23"
		if change != nil {
			change(&spec, &further)
		}
		spec.PlatformBuilds = []record.BuildConfig{further}
		_, err := normalizeSpec(spec)
		return err
	}
	require.NoError(t, with(record.Verify, nil))
	require.ErrorContains(t, with(record.Bump, nil), "further build platforms")
	require.ErrorContains(t, with(record.Verify, func(spec *record.JobSpec, _ *record.BuildConfig) { spec.IncludeDependents = true }), "without dependents")
	require.ErrorContains(t, with(record.Verify, func(spec *record.JobSpec, further *record.BuildConfig) {
		spec.Build.Provider, further.Provider = "github", "github"
	}), "local verification")
	require.ErrorContains(t, with(record.Verify, func(_ *record.JobSpec, further *record.BuildConfig) { further.Tests = record.TestSkip }), "build policies")
	require.ErrorContains(t, with(record.Verify, func(_ *record.JobSpec, further *record.BuildConfig) { further.Platform.Version = "25" }), "named twice")
	require.ErrorContains(t, with(record.Verify, func(_ *record.JobSpec, further *record.BuildConfig) { further.EnvironmentDigest = "" }), "environment digest")
}

// A resolution builds where it was asked to: on the evaluated platform when
// nothing was named, and on exactly the named platforms, in order, otherwise.
func TestResolvedBuildsMatchTheNamedPlatforms(t *testing.T) {
	t.Parallel()
	tahoe := record.Platform{OS: "darwin", Version: "25", Architecture: "arm64"}
	sonoma := record.Platform{OS: "darwin", Version: "23", Architecture: "arm64"}
	on := func(platform record.Platform) record.BuildConfig { return record.BuildConfig{Platform: platform} }

	require.NoError(t, resolvedPlatforms(tahoe, nil, on(tahoe), nil))
	require.Error(t, resolvedPlatforms(tahoe, nil, on(sonoma), nil), "an unnamed build is on the evaluated platform")
	require.Error(t, resolvedPlatforms(tahoe, nil, on(tahoe), []record.BuildConfig{on(sonoma)}), "and on it alone")
	require.Error(t, resolvedPlatforms(tahoe, []record.Platform{sonoma}, on(sonoma), nil), "named platforms add to the evaluated one (decision 4)")
	require.Error(t, resolvedPlatforms(tahoe, []record.Platform{sonoma, tahoe}, on(sonoma), []record.BuildConfig{on(tahoe)}), "which is built first")
	require.NoError(t, resolvedPlatforms(tahoe, []record.Platform{tahoe, sonoma}, on(tahoe), []record.BuildConfig{on(sonoma)}))
	require.Error(t, resolvedPlatforms(tahoe, []record.Platform{tahoe, sonoma}, on(sonoma), []record.BuildConfig{on(tahoe)}), "in the order requested")
	require.Error(t, resolvedPlatforms(tahoe, []record.Platform{tahoe, sonoma}, on(tahoe), nil), "every requested platform is built")
}
