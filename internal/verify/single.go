package verify

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/herbygillot/dockhand/v2/internal/record"
)

func ValidateConfig(config record.BuildConfig) error {
	for _, value := range []string{config.Provider, config.Platform.OS, config.Platform.Version, config.Platform.Architecture, config.EnvironmentDigest} {
		if value == "" || !utf8.ValidString(value) || strings.IndexFunc(value, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) >= 0 {
			return fmt.Errorf("verify: provider, platform, and immutable environment digest are required")
		}
	}
	if len(config.ProviderConfig) > 0 && (!json.Valid(config.ProviderConfig) || len(config.ProviderConfig) > 65536 || bytes.TrimSpace(config.ProviderConfig)[0] != '{') {
		return fmt.Errorf("verify: invalid provider configuration")
	}
	if config.Tests != record.TestDeclared && config.Tests != record.TestSkip {
		return fmt.Errorf("verify: an explicit test policy is required")
	}
	return nil
}

func PlanSingle(job record.Job, revision record.Revision) (record.VerificationPlan, record.BuildSpec, error) {
	if job.Spec.Action != record.Verify || job.Spec.Destination != record.VerificationComplete || job.Spec.Verification != record.VerificationRequired || len(job.Spec.Targets) != 1 || job.Spec.Build == nil {
		return record.VerificationPlan{}, record.BuildSpec{}, fmt.Errorf("verify: this cycle requires one verification target and an explicit build configuration")
	}
	if revision.ID == "" || revision.ID != job.Spec.InputRevision || revision.ChangeID != job.ChangeID || revision.Source != job.Spec.Source || revision.Source.Commit == "" {
		return record.VerificationPlan{}, record.BuildSpec{}, fmt.Errorf("verify: an existing committed input revision matching the accepted source is required")
	}
	if err := ValidateConfig(*job.Spec.Build); err != nil {
		return record.VerificationPlan{}, record.BuildSpec{}, err
	}
	target := job.Spec.Targets[0]
	target.Variants = maps.Clone(target.Variants)
	plan := record.VerificationPlan{JobID: job.ID, RevisionID: revision.ID, Targets: []record.VerificationTarget{{ID: record.TargetID("target_" + string(job.ID)), Port: target, Platform: job.Spec.Build.Platform}}}
	build := record.BuildSpec{RevisionID: revision.ID, Source: revision.Source, Target: target, Config: *job.Spec.Build, Inputs: []record.Artifact{}}
	build.Config.ProviderConfig = slices.Clone(build.Config.ProviderConfig)
	return plan, build, nil
}
