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

	"github.com/herbygillot/dockhand/internal/record"
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
	if config.CapabilityDigest != "" && (!config.CapabilitiesRequired || !utf8.ValidString(config.CapabilityDigest) || strings.IndexFunc(config.CapabilityDigest, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) >= 0) {
		return fmt.Errorf("verify: invalid environment capability identity")
	}
	if config.Tests != record.TestDeclared && config.Tests != record.TestRequired && config.Tests != record.TestSkip && config.Tests != record.TestWorkflow {
		return fmt.Errorf("verify: an explicit test policy is required")
	}
	return nil
}

func ValidateRequirements(requirements record.BuildRequirements) error {
	for _, value := range []string{requirements.Provider, requirements.Platform.OS, requirements.Platform.Version, requirements.Platform.Architecture} {
		if value == "" || !utf8.ValidString(value) || strings.IndexFunc(value, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) >= 0 {
			return fmt.Errorf("verify: provider and platform requirements are required")
		}
	}
	if requirements.Tests != record.TestDeclared && requirements.Tests != record.TestRequired && requirements.Tests != record.TestSkip && requirements.Tests != record.TestWorkflow {
		return fmt.Errorf("verify: an explicit test policy is required")
	}
	return nil
}

// Plan freezes one provider build question for every accepted verification target.
func Plan(job record.Job, revision record.Revision) (record.VerificationPlan, []record.BuildSpec, error) {
	if job.Spec.Build == nil {
		if job.Spec.Preparation != nil && job.Spec.Preparation.VerificationProblem != "" {
			return record.VerificationPlan{}, nil, fmt.Errorf("verify: %s; prepared branch is preserved", job.Spec.Preparation.VerificationProblem)
		}
		return record.VerificationPlan{}, nil, fmt.Errorf("verify: no build configuration was selected")
	}
	return PlanWithConfig(job, revision, *job.Spec.Build)
}

// PlanWithConfig applies an exact build configuration to every target in the job.
func PlanWithConfig(job record.Job, revision record.Revision, config record.BuildConfig) (record.VerificationPlan, []record.BuildSpec, error) {
	if job.Phase != record.PhaseVerification || (job.Spec.Destination != record.VerificationComplete && job.Spec.Destination != record.Published) || job.Spec.Verification != record.VerificationRequired || len(job.Spec.Targets) == 0 {
		return record.VerificationPlan{}, nil, fmt.Errorf("verify: this cycle requires verification targets and an explicit build configuration")
	}
	source := job.Spec.Source
	expected := job.Spec.InputRevision
	switch {
	case job.Spec.Action == record.Verify:
	case job.Spec.Action.Prepares():
		if job.ResultRevision == "" || job.Prepared == nil {
			return record.VerificationPlan{}, nil, fmt.Errorf("verify: preparation has not produced a revision")
		}
		expected = job.ResultRevision
		source = job.Prepared.Source
	default:
		return record.VerificationPlan{}, nil, fmt.Errorf("verify: unsupported job action")
	}
	if expected != "" {
		if revision.ID != expected || revision.ChangeID != job.ChangeID || revision.Source != source {
			return record.VerificationPlan{}, nil, fmt.Errorf("verify: revision does not match selected build source")
		}
	} else if revision.ID != "" || job.ChangeID != "" {
		return record.VerificationPlan{}, nil, fmt.Errorf("verify: standalone verification cannot imply a contribution revision")
	}
	if source.Tree == "" {
		return record.VerificationPlan{}, nil, fmt.Errorf("verify: an immutable source tree is required")
	}
	if err := ValidateConfig(config); err != nil {
		return record.VerificationPlan{}, nil, err
	}
	if job.Spec.BuildRequirements != nil {
		if differences := RequirementDifferences(*job.Spec.BuildRequirements, config); len(differences) != 0 {
			return record.VerificationPlan{}, nil, fmt.Errorf("verify: selected evidence does not satisfy accepted requirements: %s", strings.Join(differences, "; "))
		}
	}
	targets, err := job.Spec.RequiredTargets(revision.Scope)
	if err != nil {
		return record.VerificationPlan{}, nil, err
	}
	if revision.Scope != nil {
		rootPresent := false
		for _, target := range targets {
			if record.CompareTargets(target, job.Spec.Targets[0]) == 0 {
				rootPresent = true
			}
		}
		if !rootPresent {
			return record.VerificationPlan{}, nil, fmt.Errorf("verify: initiating port is not a buildable member of the recorded shared release")
		}
		if len(targets) > 1 && config.Provider == ProviderGitHub {
			return record.VerificationPlan{}, nil, fmt.Errorf("verify: shared-release coverage requires isolated local verification; select --provider tart and a prepared image")
		}
		if len(targets) == 0 {
			return record.VerificationPlan{}, nil, fmt.Errorf("verify: shared release has no buildable targets")
		}
	}
	plan := record.VerificationPlan{JobID: job.ID, RevisionID: revision.ID, Targets: make([]record.VerificationTarget, 0, len(targets))}
	builds := make([]record.BuildSpec, 0, len(targets))
	for i, value := range targets {
		target := value
		target.Variants = maps.Clone(target.Variants)
		targetID := record.TargetID("target_" + string(job.ID))
		if len(targets) > 1 {
			targetID = record.TargetID(fmt.Sprintf("target_%s_%d", job.ID, i+1))
		}
		plan.Targets = append(plan.Targets, record.VerificationTarget{ID: targetID, Port: target, Platform: config.Platform})
		branch := job.Spec.SourceBranch
		if job.Spec.Checkout != nil {
			branch = job.Spec.Checkout.Branch
		}
		if job.Prepared != nil {
			branch = job.Prepared.Branch
		}
		build := record.BuildSpec{Branch: branch, RevisionID: revision.ID, Source: source, Target: target, Config: config, Inputs: []record.Artifact{}}
		if correction := correctionOf(job); correction != nil {
			// A published contribution names the head its pull request points
			// at. An unpublished one names nothing, yet a previous forge
			// verification of this contribution pushed its own commit to that
			// branch, and a correction replaces exactly that. Authorizing the
			// commit being corrected is what makes it replaceable: the
			// provider still refuses unless the fork's head is that commit,
			// so a branch holding anything else is left alone.
			build.ReplaceRemoteHead = correction.RemoteHead
			if build.ReplaceRemoteHead == "" {
				build.ReplaceRemoteHead = correction.PreviousHead
			}
		}
		build.Config.ProviderConfig = slices.Clone(build.Config.ProviderConfig)
		if revision.Scope != nil {
			for _, member := range revision.Scope.Affected {
				if record.CompareTargets(member.Target, target) == 0 {
					build.Config.NeedsXcode = build.Config.NeedsXcode || member.NeedsXcode
				}
			}
			plan.Targets[i].Build = &build
			plan.Targets[i].Root = record.CompareTargets(target, job.Spec.Targets[0]) == 0
			plan.Targets[i].Reasons = []string{"shared-release sibling"}
		}
		builds = append(builds, build)
	}
	return plan, builds, nil
}

// PlanSingle retains the one-target planning contract used by evidence reuse.
func PlanSingle(job record.Job, revision record.Revision) (record.VerificationPlan, record.BuildSpec, error) {
	if err := requireSingle(job, revision); err != nil {
		return record.VerificationPlan{}, record.BuildSpec{}, err
	}
	if job.Spec.Build == nil {
		if job.Spec.Preparation != nil && job.Spec.Preparation.VerificationProblem != "" {
			return record.VerificationPlan{}, record.BuildSpec{}, fmt.Errorf("verify: %s; prepared branch is preserved", job.Spec.Preparation.VerificationProblem)
		}
		return record.VerificationPlan{}, record.BuildSpec{}, fmt.Errorf("verify: no build configuration was selected")
	}
	return planSingleWithConfig(job, revision, *job.Spec.Build)
}

// planSingleWithConfig creates a one-target plan from an exact configuration
// selected by accepted requirements and recorded evidence.
func planSingleWithConfig(job record.Job, revision record.Revision, config record.BuildConfig) (record.VerificationPlan, record.BuildSpec, error) {
	if err := requireSingle(job, revision); err != nil {
		return record.VerificationPlan{}, record.BuildSpec{}, err
	}
	plan, builds, err := PlanWithConfig(job, revision, config)
	if err != nil {
		return record.VerificationPlan{}, record.BuildSpec{}, err
	}
	return plan, builds[0], nil
}

// requireSingle is the one-target planning contract: one job target whose
// coverage intent the release scope resolves to exactly one member.
func requireSingle(job record.Job, revision record.Revision) error {
	targets, err := job.Spec.RequiredTargets(revision.Scope)
	if err != nil {
		return err
	}
	if len(job.Spec.Targets) != 1 || len(targets) != 1 {
		return fmt.Errorf("verify: this cycle requires one verification target and an explicit build configuration")
	}
	return nil
}

// correctionOf is the correction a job is making, or nil when it is not one.
func correctionOf(job record.Job) *record.CorrectionSpec {
	if job.Spec.Preparation == nil {
		return nil
	}
	return job.Spec.Preparation.Correction
}
