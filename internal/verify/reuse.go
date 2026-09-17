package verify

import (
	"bytes"
	"encoding/json"
	"maps"
	"reflect"
	"slices"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/record"
)

// Applicability explains whether a recorded execution covers the requested input.
// Commit, base, and revision IDs describe provenance; the full tree describes bytes.
type Applicability struct {
	Matches bool
	Reasons []string
}

func Applicable(wanted record.BuildSpec, previous record.Attempt) Applicability {
	result := Applicability{Reasons: []string{}}
	reject := func(reason string) { result.Reasons = append(result.Reasons, reason) }
	evidence := previous.Evidence
	if previous.ID == "" || previous.State != record.AttemptFinished || evidence == nil || evidence.Verdict != record.VerdictPassed || evidence.ObservedAt.IsZero() || evidence.Failure != nil {
		reject("previous attempt has no conclusive passing evidence")
	} else {
		for _, step := range evidence.Steps {
			if step.Verdict != record.VerdictPassed {
				reject("previous evidence includes a non-passing step")
				break
			}
		}
	}
	if wanted.Config.Tests == record.TestWorkflow {
		if evidence == nil || evidence.Workflow == nil || evidence.Workflow.Commit != wanted.Source.Commit || evidence.Workflow.Branch != wanted.PushBranch() || evidence.Workflow.Conclusion != "success" {
			reject("matching workflow evidence is missing")
		}
	}
	if wanted.Config.CapabilitiesRequired {
		result.Reasons = append(result.Reasons, environmentEvidenceDifferences(wanted.Config, previous.Spec.Config, evidence)...)
	}
	result.Reasons = append(result.Reasons, InputDifferences(wanted, previous.Spec)...)
	result.Matches = len(result.Reasons) == 0
	return result
}

// InputDifferences compares complete build inputs without interpreting an outcome.
func InputDifferences(wanted, old record.BuildSpec) []string {
	reasons := []string{}
	reject := func(reason string) { reasons = append(reasons, reason) }
	if !git.ValidObjectID(string(wanted.Source.Tree)) || wanted.Source.Tree != old.Source.Tree {
		reject("source tree differs")
	}
	if wanted.Config.Tests == record.TestWorkflow && (wanted.Source.Commit != old.Source.Commit || wanted.PushBranch() != old.PushBranch()) {
		reject("workflow commit or branch differs")
	}
	if wanted.Target.Name != old.Target.Name || wanted.Target.Portfile != old.Target.Portfile || wanted.Target.Subport != old.Target.Subport {
		reject("verification target differs")
	}
	if !maps.Equal(wanted.Target.Variants, old.Target.Variants) {
		reject("variant choices differ")
	}
	if ValidateConfig(wanted.Config) != nil || ValidateConfig(old.Config) != nil {
		reject("build configuration is incomplete")
	}
	if wanted.Config.Provider != old.Config.Provider {
		reject("verification provider differs")
	}
	if wanted.Config.Platform != old.Config.Platform {
		reject("platform differs")
	}
	if wanted.Config.EnvironmentDigest != old.Config.EnvironmentDigest {
		reject("build environment differs")
	}
	if wanted.Config.CapabilitiesRequired != old.Config.CapabilitiesRequired {
		reject("environment capability policy differs")
	}
	if wanted.Config.CapabilityDigest != "" && old.Config.CapabilityDigest != "" && wanted.Config.CapabilityDigest != old.Config.CapabilityDigest {
		reject("environment capability identity differs")
	}
	if wanted.Config.VerifierDigest == "" || old.Config.VerifierDigest == "" {
		reject("verifier identity was not recorded")
	} else if wanted.Config.VerifierDigest != old.Config.VerifierDigest {
		reject("verifier implementation differs")
	}
	if wanted.Config.NeedsXcode != old.Config.NeedsXcode {
		reject("Xcode requirement differs")
	}
	if wanted.Config.FromSource != old.Config.FromSource {
		reject("source-build policy differs")
	}
	if wanted.Config.Tests != old.Config.Tests {
		reject("test policy differs")
	}
	if !sameJSON(wanted.Config.ProviderConfig, old.Config.ProviderConfig) {
		reject("provider settings differ")
	}
	if !reflect.DeepEqual(wanted.Preinstall, old.Preinstall) {
		reject("preinstalled source roots differ")
	}
	if !slices.Equal(wanted.Inputs, old.Inputs) {
		reject("artifact inputs differ")
	}
	return reasons
}

// RequirementDifferences compares accepted evidence-selection requirements
// with the exact configuration retained by an earlier attempt.
func RequirementDifferences(wanted record.BuildRequirements, old record.BuildConfig) []string {
	reasons := []string{}
	if ValidateRequirements(wanted) != nil || ValidateConfig(old) != nil {
		reasons = append(reasons, "build requirements or configuration are incomplete")
	}
	if wanted.Provider != old.Provider {
		reasons = append(reasons, "verification provider differs")
	}
	if wanted.Platform != old.Platform {
		reasons = append(reasons, "platform differs")
	}
	if wanted.NeedsXcode != old.NeedsXcode {
		reasons = append(reasons, "Xcode requirement differs")
	}
	if wanted.CapabilitiesRequired != old.CapabilitiesRequired {
		reasons = append(reasons, "environment capability policy differs")
	}
	if wanted.FromSource != old.FromSource {
		reasons = append(reasons, "source-build policy differs")
	}
	if wanted.Tests != old.Tests {
		reasons = append(reasons, "test policy differs")
	}
	return reasons
}

func environmentEvidenceDifferences(wanted, old record.BuildConfig, evidence *record.Evidence) []string {
	if evidence == nil || evidence.Environment == nil {
		return []string{"environment capability evidence is missing"}
	}
	observed := evidence.Environment
	reasons := []string{}
	if observed.Provider != wanted.Provider || observed.EnvironmentDigest != wanted.EnvironmentDigest {
		reasons = append(reasons, "environment capability evidence identifies a different build environment")
	}
	if observed.CapabilityDigest == "" || wanted.CapabilityDigest != "" && observed.CapabilityDigest != wanted.CapabilityDigest || old.CapabilityDigest != "" && observed.CapabilityDigest != old.CapabilityDigest {
		reasons = append(reasons, "environment capability identity differs")
	}
	capabilities := observed.Capabilities
	if capabilities.Platform != wanted.Platform {
		reasons = append(reasons, "observed environment platform differs")
	}
	if capabilities.MacPortsPrefix == "" || capabilities.MacPortsVersion == "" {
		reasons = append(reasons, "observed MacPorts environment is incomplete")
	}
	if capabilities.DeveloperTools != record.DeveloperToolsCommandLine && capabilities.DeveloperTools != record.DeveloperToolsXcode || wanted.NeedsXcode && capabilities.DeveloperTools != record.DeveloperToolsXcode {
		reasons = append(reasons, "observed developer tools do not satisfy the build")
	}
	return reasons
}

func sameJSON(a, b json.RawMessage) bool {
	if len(a) == 0 || len(b) == 0 {
		return len(a) == 0 && len(b) == 0
	}
	decode := func(raw []byte) (any, error) {
		var value any
		d := json.NewDecoder(bytes.NewReader(raw))
		d.UseNumber()
		err := d.Decode(&value)
		return value, err
	}
	x, xe := decode(a)
	y, ye := decode(b)
	return xe == nil && ye == nil && reflect.DeepEqual(x, y)
}
