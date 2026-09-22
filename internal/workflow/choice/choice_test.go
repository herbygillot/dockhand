package choice_test

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/progress"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/verify/tart"
	"github.com/herbygillot/dockhand/internal/workflow/choice"
	"github.com/stretchr/testify/require"
)

type localBuild struct {
	calls   int
	err     error
	options tart.BuildOptions
}

func (b *localBuild) BuildConfig(_ context.Context, platform record.Platform, options tart.BuildOptions) (record.BuildConfig, error) {
	b.calls++
	b.options = options
	return record.BuildConfig{Provider: "tart", Platform: platform, Tests: options.Tests, NeedsXcode: options.NeedsXcode}, b.err
}

func (b *localBuild) BuildConfigForImage(ctx context.Context, platform record.Platform, options tart.BuildOptions, image string) (record.BuildConfig, error) {
	value, err := b.BuildConfig(ctx, platform, options)
	value.EnvironmentDigest = image
	return value, err
}

type remoteBuild struct {
	calls int
	err   error
}

func (r *remoteBuild) BuildConfig(_ context.Context, platform record.Platform, needsXcode bool) (record.BuildConfig, error) {
	r.calls++
	return record.BuildConfig{Provider: "github", Platform: platform, Tests: record.TestWorkflow, NeedsXcode: needsXcode}, r.err
}

var platform = record.Platform{OS: "darwin", Version: "25", Architecture: "arm64"}

func TestAutomaticProviderSelection(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name, provider string
		localError     error
		xcode          bool
		want           string
		problem        bool
	}{
		{name: "local ready", provider: "auto", want: "tart"},
		{name: "local Xcode ready", provider: "auto", xcode: true, want: "tart"},
		{name: "no image", provider: "auto", localError: tart.ErrImageUnavailable, want: "github"},
		{name: "no Xcode image", provider: "auto", localError: tart.ErrImageUnavailable, xcode: true, want: "github"},
		{name: "no binary", provider: "auto", localError: tart.ErrExecutableUnavailable, want: "github"},
		{name: "explicit github", provider: "github", want: "github"},
		{name: "explicit tart unavailable", provider: "tart", localError: tart.ErrImageUnavailable, problem: true},
		{name: "local inspection failed", provider: "auto", localError: os.ErrPermission, problem: true},
		{name: "another tool missing", provider: "auto", localError: exec.ErrNotFound, problem: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			local, remote := &localBuild{err: test.localError}, &remoteBuild{}
			providers := choice.Providers{Name: test.provider, Local: local, Remote: remote}
			useXcode := "no"
			if test.xcode {
				useXcode = "yes"
			}
			snapshot := macports.Snapshot{Target: record.Target{Name: "fixture"}, Runtime: macports.Runtime{BaseVersion: "2.11.6"}, Ports: map[string]macports.PortInfo{"fixture": {Options: map[string]string{"use_xcode": useXcode}}}}
			var messages []string
			ctx := progress.WithReporter(t.Context(), func(u progress.Update) { messages = append(messages, u.Message) })
			result, err := providers.Resolver(platform, choice.Options{Preserve: true})(ctx, snapshot)
			if test.problem {
				require.True(t, err != nil || result.Problem != "")
				require.Zero(t, remote.calls)
				return
			}
			require.NoError(t, err)
			require.Equal(t, test.want, result.Build.Provider)
			if test.want == "tart" {
				require.Zero(t, remote.calls, "local selection must not contact GitHub verification")
				require.Equal(t, record.TestDeclared, result.Build.Tests)
				require.Equal(t, test.xcode, local.options.NeedsXcode)
				require.Equal(t, "2.11.6", local.options.HostMacPortsVersion, "the host Base version reaches the provider so it can warn on skew")
			} else {
				require.Positive(t, remote.calls)
				require.Equal(t, record.TestWorkflow, result.Build.Tests)
				require.Equal(t, test.xcode, result.Build.NeedsXcode)
			}
			if errors.Is(test.localError, tart.ErrImageUnavailable) {
				require.Contains(t, strings.Join(messages, "\n"), "dockhand setup")
				if test.xcode {
					require.Contains(t, strings.Join(messages, "\n"), "--xcode")
				}
			}
			if test.provider == "github" {
				require.Zero(t, local.calls)
			} else {
				require.Equal(t, 1, local.calls)
			}
		})
	}
}

// GitHub takes only its workflow's policies; a local test policy, a
// from-source build, or a variant choice is refused there, and under auto a
// preparation preserves that refusal as a problem rather than failing.
func TestRemoteRefusesLocalPolicies(t *testing.T) {
	t.Parallel()
	snapshot := macports.Snapshot{Target: record.Target{Name: "fixture", Variants: map[string]bool{"debug": true}}, Ports: map[string]macports.PortInfo{"fixture": {Options: map[string]string{"use_xcode": "no"}}}}
	providers := choice.Providers{Name: "github", Local: &localBuild{}, Remote: &remoteBuild{}}
	_, err := providers.Resolver(platform, choice.Options{})(t.Context(), snapshot)
	require.ErrorContains(t, err, "select --provider tart for local policies")
	providers = choice.Providers{Name: "auto", Local: &localBuild{err: tart.ErrImageUnavailable}, Remote: &remoteBuild{}}
	result, err := providers.Resolver(platform, choice.Options{FromSource: true, Preserve: true})(t.Context(), macports.Snapshot{Target: record.Target{Name: "fixture"}, Ports: map[string]macports.PortInfo{"fixture": {Options: map[string]string{"use_xcode": "no"}}}})
	require.NoError(t, err)
	require.Contains(t, result.Problem, "GitHub verification could not be configured")
	require.Nil(t, result.Build)
}

// An explicit Tart choice that cannot be served is preserved as an evidence
// requirement for a preparation, so the branch is still made, and fails a
// verification, which has nothing to preserve.
func TestLocalFailurePreservedOrFailed(t *testing.T) {
	t.Parallel()
	snapshot := macports.Snapshot{Target: record.Target{Name: "fixture"}, Ports: map[string]macports.PortInfo{"fixture": {Options: map[string]string{"use_xcode": "yes"}}}}
	providers := choice.Providers{Name: "tart", Local: &localBuild{err: tart.ErrImageUnavailable}, Remote: &remoteBuild{}}
	result, err := providers.Resolver(platform, choice.Options{Tests: record.TestSkip, Preserve: true})(t.Context(), snapshot)
	require.NoError(t, err)
	require.Nil(t, result.Build)
	require.Equal(t, &record.BuildRequirements{Provider: "tart", Platform: platform, NeedsXcode: true, CapabilitiesRequired: true, Tests: record.TestSkip}, result.Requirements)
	require.Contains(t, result.Problem, "no suitable prepared image")
	_, err = providers.Resolver(platform, choice.Options{})(t.Context(), snapshot)
	require.ErrorIs(t, err, tart.ErrImageUnavailable)
}

func TestTargetImagesBoundAtIntake(t *testing.T) {
	t.Parallel()
	local := &localBuild{}
	providers := choice.Providers{Name: "tart", Local: local, TargetImages: map[string]string{"child": "xcode-image"}}
	evaluation := macports.Snapshot{Target: record.Target{Name: "root"}, Ports: map[string]macports.PortInfo{"root": {Options: map[string]string{"use_xcode": "no"}}}}
	resolve := providers.Resolver(platform, choice.Options{Tests: record.TestSkip, Preserve: true})
	result, err := resolve(t.Context(), evaluation)
	require.NoError(t, err)
	require.Equal(t, "xcode-image", result.TargetBuilds["child"].EnvironmentDigest)
	require.Equal(t, result.Build.Platform, result.TargetBuilds["child"].Platform)
	require.Equal(t, record.TestSkip, result.TargetBuilds["child"].Tests)
	providers.TargetImages = map[string]string{"root": "xcode-image"}
	_, err = providers.Resolver(platform, choice.Options{Tests: record.TestSkip, Preserve: true})(t.Context(), evaluation)
	require.ErrorContains(t, err, "use --image")
	providers.TargetImages = map[string]string{"child": "missing"}
	local.err = tart.ErrImageUnavailable
	_, err = providers.Resolver(platform, choice.Options{Tests: record.TestSkip, Preserve: true})(t.Context(), evaluation)
	require.Error(t, err, "explicit image choices cannot become unspecified evidence requirements")
}
