package workflow

import (
	"fmt"
	"io/fs"
	"maps"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/publish"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/verify"
)

// normalizeSpec validates caller-supplied intent without consulting the state store
// or external services. It copies mutable inputs and canonicalizes target order
// and empty variant maps so equivalent requests have the same representation.
func normalizeSpec(spec record.JobSpec) (record.JobSpec, error) {
	spec.EvaluatedVersions = maps.Clone(spec.EvaluatedVersions)
	if spec.SourceBranch != "" && !git.ValidBranchName(spec.SourceBranch) {
		return record.JobSpec{}, fmt.Errorf("%w: invalid source branch", ErrInvalidRequest)
	}
	if !utf8.ValidString(spec.Reason) || (spec.ChangeID != "" && !validToken(string(spec.ChangeID))) || (spec.InputRevision != "" && !validToken(string(spec.InputRevision))) {
		return record.JobSpec{}, fmt.Errorf("%w: invalid change ID, revision ID, or reason encoding", ErrInvalidRequest)
	}
	if spec.KeepFailed && (spec.Verification != record.VerificationRequired || spec.Action == record.Publish || spec.Build != nil && spec.Build.Provider == "github") {
		return record.JobSpec{}, fmt.Errorf("%w: keeping failed environments requires local verification", ErrInvalidRequest)
	}
	if spec.FreshVerification && (spec.Action != record.Verify || spec.Verification != record.VerificationRequired) {
		return record.JobSpec{}, fmt.Errorf("%w: fresh verification requires verify", ErrInvalidRequest)
	}
	if spec.IncludeDependents && (spec.Verification != record.VerificationRequired || spec.Build == nil || spec.Build.Provider == "github" || spec.Action != record.Verify && !preparationAction(spec.Action)) {
		return record.JobSpec{}, fmt.Errorf("%w: dependent verification requires a local build configuration and verification", ErrInvalidRequest)
	}
	if len(spec.TargetBuilds) > 0 {
		if !spec.IncludeDependents || spec.Build == nil || spec.Build.Provider == "github" {
			return record.JobSpec{}, fmt.Errorf("%w: target builds require a local dependent plan", ErrInvalidRequest)
		}
		spec.TargetBuilds = maps.Clone(spec.TargetBuilds)
		for name, build := range spec.TargetBuilds {
			if !validToken(name) || strings.ContainsAny(name, "/\\") || build.Provider != spec.Build.Provider || build.Platform != spec.Build.Platform || build.Tests != spec.Build.Tests || build.FromSource != spec.Build.FromSource {
				return record.JobSpec{}, fmt.Errorf("%w: incompatible target build for %s", ErrInvalidRequest, name)
			}
			for _, root := range spec.Targets {
				if root.Name == name {
					return record.JobSpec{}, fmt.Errorf("%w: target image overrides a root", ErrInvalidRequest)
				}
			}
			if err := verify.ValidateConfig(build); err != nil {
				return record.JobSpec{}, fmt.Errorf("%w: %v", ErrInvalidRequest, err)
			}
			build.ProviderConfig = slices.Clone(build.ProviderConfig)
			spec.TargetBuilds[name] = build
		}
	}
	switch spec.Action {
	case record.Bump, record.BumpRevision, record.RefreshChecksums, record.Verify, record.Publish:
	case record.Rebase, record.Amend:
		if spec.Preparation == nil || spec.Preparation.Correction == nil {
			return record.JobSpec{}, ErrInvalidRequest
		}
	default:
		return record.JobSpec{}, fmt.Errorf("%w: unknown action %q", ErrInvalidRequest, spec.Action)
	}
	if spec.Verification != record.VerificationRequired && spec.Verification != record.VerificationSkipped {
		return record.JobSpec{}, fmt.Errorf("%w: an explicit verification policy is required", ErrInvalidRequest)
	}
	switch spec.Destination {
	case record.BranchReady:
		if spec.Verification != record.VerificationSkipped {
			return record.JobSpec{}, fmt.Errorf("%w: branch-ready requires explicitly skipped verification", ErrInvalidRequest)
		}
	case record.VerificationComplete:
		if spec.Verification != record.VerificationRequired {
			return record.JobSpec{}, fmt.Errorf("%w: verification-complete requires verification", ErrInvalidRequest)
		}
	case record.Published:
	default:
		return record.JobSpec{}, fmt.Errorf("%w: a valid destination is required", ErrInvalidRequest)
	}
	if spec.Action == record.Verify && spec.Destination != record.VerificationComplete {
		return record.JobSpec{}, fmt.Errorf("%w: verify must request verification-complete", ErrInvalidRequest)
	}
	if spec.Action == record.Publish && (spec.Destination != record.Published || spec.Publication == nil || spec.Build == nil || spec.Verification != record.VerificationRequired) {
		return record.JobSpec{}, fmt.Errorf("%w: publish requires publication intent, passing verification configuration, and the published destination", ErrInvalidRequest)
	}
	if spec.PublishTo != nil {
		destination := *spec.PublishTo
		if !preparationAction(spec.Action) || spec.Preparation == nil || spec.Destination != record.Published || spec.Verification != record.VerificationRequired || spec.Publication != nil {
			return record.JobSpec{}, fmt.Errorf("%w: combined publication requires a verified preparation job", ErrInvalidRequest)
		}
		if err := publish.ValidateDestination(destination); err != nil {
			return record.JobSpec{}, fmt.Errorf("%w: %v", ErrInvalidRequest, err)
		}
		spec.PublishTo = &destination
	}
	if preparationAction(spec.Action) && spec.Destination == record.Published && spec.PublishTo == nil {
		return record.JobSpec{}, fmt.Errorf("%w: publication destination required", ErrInvalidRequest)
	}
	if spec.Publication != nil {
		v := *spec.Publication
		if v.LocalBranch != "" && !git.ValidBranchName(v.LocalBranch) {
			return record.JobSpec{}, ErrInvalidRequest
		}
		if spec.Action != record.Publish || v.Forge == "" || v.Repository == "" || v.HeadRepository == "" || !git.ValidBranchName(v.HeadBranch) || !git.ValidBranchName(v.BaseBranch) || v.PushURL == "" || v.BaseURL == "" || !filepath.IsAbs(v.LockDirectory) || !git.ValidObjectID(string(v.Desired.Head)) || v.Desired.Title == "" || !validToken(string(v.EvidenceAttempt)) || v.ExpectedRemoteHead.Exists != (v.ExpectedRemoteHead.Commit != "") || v.ExpectedRemoteHead.Exists && !git.ValidObjectID(string(v.ExpectedRemoteHead.Commit)) {
			return record.JobSpec{}, ErrInvalidRequest
		}
		if v.ExpectedPR != nil {
			pr := *v.ExpectedPR
			v.ExpectedPR = &pr
		}
		spec.Publication = &v
	}
	if spec.Build != nil {
		if spec.BuildRequirements != nil {
			return record.JobSpec{}, fmt.Errorf("%w: select a build configuration or recorded-evidence requirements, not both", ErrInvalidRequest)
		}
		if spec.Verification != record.VerificationRequired {
			return record.JobSpec{}, fmt.Errorf("%w: build configuration requires verification", ErrInvalidRequest)
		}
		if err := verify.ValidateConfig(*spec.Build); err != nil {
			return record.JobSpec{}, fmt.Errorf("%w: %v", ErrInvalidRequest, err)
		}
		build := *spec.Build
		build.ProviderConfig = slices.Clone(build.ProviderConfig)
		spec.Build = &build
	}
	if spec.BuildRequirements != nil {
		if !preparationAction(spec.Action) || spec.Verification != record.VerificationRequired || spec.Destination == record.BranchReady {
			return record.JobSpec{}, fmt.Errorf("%w: recorded-evidence requirements require a verified preparation job", ErrInvalidRequest)
		}
		if err := verify.ValidateRequirements(*spec.BuildRequirements); err != nil {
			return record.JobSpec{}, fmt.Errorf("%w: %v", ErrInvalidRequest, err)
		}
		requirements := *spec.BuildRequirements
		spec.BuildRequirements = &requirements
	}
	if spec.Version != "" && (spec.Action != record.Bump || !validToken(spec.Version)) {
		return record.JobSpec{}, fmt.Errorf("%w: only bump accepts a nonempty version without whitespace or control characters", ErrInvalidRequest)
	}
	if spec.Preparation != nil {
		choices := *spec.Preparation
		if correction := choices.Correction; correction != nil {
			copy := *correction
			choices.Correction = &copy
			if spec.Action != record.Amend && spec.Action != record.Rebase || !validToken(string(copy.ChangeID)) || !validToken(string(copy.RevisionID)) || !git.ValidBranchName(copy.Branch) || !git.ValidObjectID(string(copy.PreviousHead)) || copy.RemoteHead != "" && !git.ValidObjectID(string(copy.RemoteHead)) || validateSource(copy.Candidate) != nil || copy.Candidate.Commit == "" || copy.Candidate.Base == "" {
				return record.JobSpec{}, ErrInvalidRequest
			}
		}
		if !preparationAction(spec.Action) || spec.InputRevision != "" || spec.Source.Commit == "" || len(spec.Targets) != 1 {
			return record.JobSpec{}, fmt.Errorf("%w: preparation requires one committed source target and a branch-ready, verification, or publication destination", ErrInvalidRequest)
		}
		if !git.ValidBranchName(choices.SourceBranch) || choices.Author.Name == "" || choices.Author.Email == "" || strings.ContainsAny(choices.Author.Name+choices.Author.Email, "\x00\r\n<>") || !utf8.ValidString(choices.Author.Name+choices.Author.Email) {
			return record.JobSpec{}, fmt.Errorf("%w: preparation requires a source branch and valid author identity", ErrInvalidRequest)
		}
		for _, value := range []string{choices.Platform.OS, choices.Platform.Version, choices.Platform.Architecture} {
			if !validToken(value) {
				return record.JobSpec{}, fmt.Errorf("%w: preparation requires a complete platform", ErrInvalidRequest)
			}
		}
		if spec.Build != nil && spec.Build.Platform != choices.Platform {
			return record.JobSpec{}, fmt.Errorf("%w: preparation and build platforms disagree", ErrInvalidRequest)
		}
		if spec.BuildRequirements != nil && spec.BuildRequirements.Platform != choices.Platform {
			return record.JobSpec{}, fmt.Errorf("%w: preparation and recorded-evidence platforms disagree", ErrInvalidRequest)
		}
		spec.Preparation = &choices
	}
	if spec.Checkout != nil {
		c := *spec.Checkout
		if spec.Action != record.Verify || spec.InputRevision != "" || !git.ValidObjectID(string(c.Head)) || c.ModifiedFiles < 0 || (c.Branch != "" && !git.ValidBranchName(c.Branch)) || (c.ModifiedFiles == 0 && spec.Source.Commit != c.Head) || (c.ModifiedFiles > 0 && spec.Source.Commit != "") {
			return record.JobSpec{}, fmt.Errorf("%w: invalid checkout provenance", ErrInvalidRequest)
		}
		spec.Checkout = &c
	}
	if spec.InputRevision != "" {
		if spec.Source != (record.Source{}) {
			return record.JobSpec{}, fmt.Errorf("%w: omit source when selecting an existing revision", ErrInvalidRequest)
		}
	} else {
		if spec.ChangeID != "" && spec.Preparation == nil {
			return record.JobSpec{}, fmt.Errorf("%w: an existing change requires an explicit input revision", ErrInvalidRequest)
		}
		if err := validateSource(spec.Source); err != nil {
			return record.JobSpec{}, err
		}
	}
	if len(spec.Targets) == 0 {
		return record.JobSpec{}, fmt.Errorf("%w: at least one resolved target is required", ErrInvalidRequest)
	}
	spec.Targets = slices.Clone(spec.Targets)
	for i, target := range spec.Targets {
		if !validToken(target.Name) || strings.ContainsAny(target.Name, "/\\") ||
			!fs.ValidPath(target.Portfile) || path.Base(target.Portfile) != "Portfile" ||
			strings.ContainsRune(target.Portfile, '\\') || strings.IndexFunc(target.Portfile, unicode.IsControl) >= 0 ||
			(target.Subport != "" && (!validToken(target.Subport) || strings.ContainsAny(target.Subport, "/\\"))) {
			return record.JobSpec{}, fmt.Errorf("%w: invalid target %q or Portfile path %q", ErrInvalidRequest, target.Name, target.Portfile)
		}
		for variant := range target.Variants {
			if !validToken(variant) || strings.ContainsAny(variant, "/\\") || strings.HasPrefix(variant, "+") || strings.HasPrefix(variant, "-") {
				return record.JobSpec{}, fmt.Errorf("%w: invalid variant name %q", ErrInvalidRequest, variant)
			}
		}
		if len(target.Variants) == 0 {
			target.Variants = nil
		} else {
			target.Variants = maps.Clone(target.Variants)
		}
		spec.Targets[i] = target
	}
	slices.SortFunc(spec.Targets, record.CompareTargets)
	for i := 1; i < len(spec.Targets); i++ {
		if record.CompareTargets(spec.Targets[i-1], spec.Targets[i]) == 0 {
			return record.JobSpec{}, fmt.Errorf("%w: duplicate target %q", ErrInvalidRequest, spec.Targets[i].Name)
		}
	}
	return spec, nil
}

// validateSource checks syntax. Executors check source availability when they
// consume it; persistence does not inspect or pin Git objects.
func validateSource(source record.Source) error {
	if !git.ValidObjectID(string(source.Tree)) {
		return fmt.Errorf("%w: an immutable source tree ID is required", ErrInvalidRequest)
	}
	for _, id := range []record.ObjectID{source.Commit, source.Base} {
		if id != "" && (!git.ValidObjectID(string(id)) || len(id) != len(source.Tree)) {
			return fmt.Errorf("%w: invalid source commit or base ID", ErrInvalidRequest)
		}
	}
	return nil
}

// validToken accepts a nonempty UTF-8 identifier without whitespace or control characters.
func validToken(value string) bool {
	return value != "" && utf8.ValidString(value) && strings.IndexFunc(value, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) == -1
}
