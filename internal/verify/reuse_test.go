package verify_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/herbygillot/dockhand/v2/internal/record"
	"github.com/herbygillot/dockhand/v2/internal/verify"
	"github.com/stretchr/testify/require"
)

func reusableBuild() record.BuildSpec {
	return record.BuildSpec{Source: record.Source{Tree: record.ObjectID(strings.Repeat("a", 40))}, Target: record.Target{Name: "fixture", Portfile: "devel/fixture/Portfile"}, Config: record.BuildConfig{Provider: "test", Platform: record.Platform{OS: "darwin", Version: "25", Architecture: "arm64"}, EnvironmentDigest: "image:one", VerifierDigest: "runner:one", ProviderConfig: json.RawMessage(`{"a":1,"b":2}`), Tests: record.TestDeclared}}
}
func TestApplicabilityIgnoresProvenanceButRequiresAllBuildInputs(t *testing.T) {
	original := record.Attempt{ID: "attempt", Spec: reusableBuild(), State: record.AttemptFinished, Evidence: &record.Evidence{Verdict: record.VerdictPassed, ObservedAt: time.Now()}}
	wanted := reusableBuild()
	wanted.Source.Commit = record.ObjectID(strings.Repeat("b", 40))
	wanted.Source.Base = record.ObjectID(strings.Repeat("c", 40))
	wanted.RevisionID = "another-revision"
	wanted.Target.Variants = map[string]bool{}
	wanted.Inputs = []record.Artifact{}
	wanted.Config.ProviderConfig = json.RawMessage(`{ "b": 2, "a": 1 }`)
	require.True(t, verify.Applicable(wanted, original).Matches)
	for name, edit := range map[string]func(*record.BuildSpec){
		"tree":              func(v *record.BuildSpec) { v.Source.Tree = record.ObjectID(strings.Repeat("d", 40)) },
		"name":              func(v *record.BuildSpec) { v.Target.Name = "other" },
		"portfile":          func(v *record.BuildSpec) { v.Target.Portfile = "other/fixture/Portfile" },
		"subport":           func(v *record.BuildSpec) { v.Target.Subport = "child" },
		"variants":          func(v *record.BuildSpec) { v.Target.Variants = map[string]bool{"ssl": false} },
		"provider":          func(v *record.BuildSpec) { v.Config.Provider = "other" },
		"platform":          func(v *record.BuildSpec) { v.Config.Platform.Architecture = "x86_64" },
		"image":             func(v *record.BuildSpec) { v.Config.EnvironmentDigest = "image:two" },
		"verifier":          func(v *record.BuildSpec) { v.Config.VerifierDigest = "runner:two" },
		"legacy":            func(v *record.BuildSpec) { v.Config.VerifierDigest = "" },
		"from source":       func(v *record.BuildSpec) { v.Config.FromSource = true },
		"tests":             func(v *record.BuildSpec) { v.Config.Tests = record.TestSkip },
		"provider settings": func(v *record.BuildSpec) { v.Config.ProviderConfig = json.RawMessage(`{"a":1,"b":3}`) },
		"artifacts":         func(v *record.BuildSpec) { v.Inputs = []record.Artifact{{Name: "dependency", Digest: "sha256:one"}} },
	} {
		t.Run(name, func(t *testing.T) {
			changed := wanted
			edit(&changed)
			result := verify.Applicable(changed, original)
			require.False(t, result.Matches)
			require.NotEmpty(t, result.Reasons)
		})
	}
	require.Equal(t, json.RawMessage(`{"a":1,"b":2}`), original.Spec.Config.ProviderConfig)
}
func TestApplicabilityRejectsIncompleteOrNonPassingEvidence(t *testing.T) {
	for _, verdict := range []record.Verdict{record.VerdictUnknown, record.VerdictFailed, record.VerdictBlocked, record.VerdictErrored, record.VerdictUnsupported, record.VerdictCanceled} {
		attempt := record.Attempt{ID: "a", Spec: reusableBuild(), State: record.AttemptFinished, Evidence: &record.Evidence{Verdict: verdict, ObservedAt: time.Now()}}
		require.False(t, verify.Applicable(reusableBuild(), attempt).Matches)
	}
	attempt := record.Attempt{ID: "a", Spec: reusableBuild(), State: record.AttemptRunning, Evidence: &record.Evidence{Verdict: record.VerdictPassed, ObservedAt: time.Now()}}
	require.False(t, verify.Applicable(reusableBuild(), attempt).Matches)
	attempt.State = record.AttemptFinished
	attempt.Evidence.ObservedAt = time.Time{}
	require.False(t, verify.Applicable(reusableBuild(), attempt).Matches)
	attempt.Evidence.ObservedAt = time.Now()
	attempt.Evidence.Steps = []record.StepResult{{Verdict: record.VerdictFailed}}
	require.False(t, verify.Applicable(reusableBuild(), attempt).Matches)
}

func TestBuildRequirementsPreserveAcceptedChoices(t *testing.T) {
	config := reusableBuild().Config
	requirements := record.BuildRequirements{Provider: config.Provider, Platform: config.Platform, FromSource: config.FromSource, Tests: config.Tests}
	require.NoError(t, verify.ValidateRequirements(requirements))
	require.Empty(t, verify.RequirementDifferences(requirements, config))
	require.Contains(t, verify.RequirementDifferences(record.BuildRequirements{Provider: config.Provider, Platform: config.Platform, NeedsXcode: true, FromSource: config.FromSource, Tests: config.Tests}, config), "Xcode requirement differs")
	for name, edit := range map[string]func(*record.BuildConfig){
		"provider":    func(v *record.BuildConfig) { v.Provider = "other" },
		"platform":    func(v *record.BuildConfig) { v.Platform.Version = "24" },
		"from source": func(v *record.BuildConfig) { v.FromSource = !v.FromSource },
		"tests":       func(v *record.BuildConfig) { v.Tests = record.TestSkip },
		"incomplete":  func(v *record.BuildConfig) { v.EnvironmentDigest = "" },
	} {
		t.Run(name, func(t *testing.T) {
			changed := config
			edit(&changed)
			require.NotEmpty(t, verify.RequirementDifferences(requirements, changed))
		})
	}
}
