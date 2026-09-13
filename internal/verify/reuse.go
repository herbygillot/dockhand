package verify

import (
	"bytes"
	"encoding/json"
	"maps"
	"reflect"
	"slices"

	"github.com/herbygillot/dockhand/v2/internal/git"
	"github.com/herbygillot/dockhand/v2/internal/record"
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
	if wanted.Config.VerifierDigest == "" || old.Config.VerifierDigest == "" {
		reject("verifier identity was not recorded")
	} else if wanted.Config.VerifierDigest != old.Config.VerifierDigest {
		reject("verifier implementation differs")
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
	if !slices.Equal(wanted.Inputs, old.Inputs) {
		reject("artifact inputs differ")
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
