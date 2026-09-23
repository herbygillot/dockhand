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

// actionRule says what one action accepts: which destinations it may
// request, and which optional parts of a spec belong to it. Whether the
// action prepares a candidate tree is the record's own rule,
// Action.Prepares. Every rule that used to read "if the action is X" is a
// column here, so adding an action is a row, not a search.
type actionRule struct {
	destinations []record.Destination
	// correction requires a correction spec with a captured candidate:
	// amend and rebase adopt a branch. A preparing action may carry a
	// correction without a candidate, an update onto the contribution's
	// revision that the job prepares itself.
	correction bool
	// version accepts an explicit version; only a bump selects one.
	version bool
	// shared accepts shared-release authorization; only a bump moves siblings.
	shared bool
	// fresh accepts a fresh verification of existing evidence.
	fresh bool
	// checkout accepts working-tree provenance in place of a committed source.
	checkout bool
	// publication carries publication intent and the published destination.
	publication bool
	// dependents may verify direct dependents beside the roots.
	dependents bool
	// keepFailed may retain a failed local verification environment.
	keepFailed bool
}

var preparedDestinations = []record.Destination{record.BranchReady, record.VerificationComplete, record.Published}

var actionRules = map[record.Action]actionRule{
	record.Bump:             {destinations: preparedDestinations, version: true, shared: true, dependents: true, keepFailed: true},
	record.BumpRevision:     {destinations: preparedDestinations, dependents: true, keepFailed: true},
	record.RefreshChecksums: {destinations: preparedDestinations, dependents: true, keepFailed: true},
	record.Amend:            {destinations: preparedDestinations, correction: true, dependents: true, keepFailed: true},
	record.Rebase:           {destinations: preparedDestinations, correction: true, dependents: true, keepFailed: true},
	record.Verify:           {destinations: []record.Destination{record.VerificationComplete}, fresh: true, checkout: true, dependents: true, keepFailed: true},
	record.Publish:          {destinations: []record.Destination{record.Published}, publication: true},
}

// specStep validates or canonicalizes one part of a spec under its action's rule.
type specStep func(spec *record.JobSpec, rule actionRule) error

// normalizeSpec validates caller-supplied intent without consulting the state store
// or external services. It copies mutable inputs and canonicalizes target order
// and empty variant maps so equivalent requests have the same representation.
// Each step owns one part of the spec; the action's rule says which parts
// belong to it.
func normalizeSpec(spec record.JobSpec) (record.JobSpec, error) {
	rule, ok := actionRules[spec.Action]
	if !ok {
		return record.JobSpec{}, fmt.Errorf("%w: unknown action %q", ErrInvalidRequest, spec.Action)
	}
	spec.EvaluatedVersions = maps.Clone(spec.EvaluatedVersions)
	for _, step := range []specStep{
		validateEncodings, validateOptions, normalizeTargetBuilds, validateDestination,
		normalizePublication, normalizeBuild, normalizeRequirements, validateVersion,
		normalizePreparation, normalizeCheckout, validateSourceSelection, normalizeTargets,
	} {
		if err := step(&spec, rule); err != nil {
			return record.JobSpec{}, err
		}
	}
	return spec, nil
}

func validateEncodings(spec *record.JobSpec, _ actionRule) error {
	if spec.SourceBranch != "" && !git.ValidBranchName(spec.SourceBranch) {
		return fmt.Errorf("%w: invalid source branch", ErrInvalidRequest)
	}
	if !utf8.ValidString(spec.Subject) || strings.ContainsAny(spec.Subject, "\r\n\x00") || (spec.ChangeID != "" && !validToken(string(spec.ChangeID))) || (spec.InputRevision != "" && !validToken(string(spec.InputRevision))) {
		return fmt.Errorf("%w: invalid change ID, revision ID, or subject encoding", ErrInvalidRequest)
	}
	for _, reference := range spec.References {
		if !reference.Valid() {
			return fmt.Errorf("%w: invalid reference %q", ErrInvalidRequest, reference.Trailer())
		}
	}
	return nil
}

// validateOptions checks the flags that only some actions accept.
func validateOptions(spec *record.JobSpec, rule actionRule) error {
	local := spec.Build != nil && spec.Build.Provider != "github"
	if spec.KeepFailed && (!rule.keepFailed || spec.Verification != record.VerificationRequired || spec.Build != nil && !local) {
		return fmt.Errorf("%w: keeping failed environments requires local verification", ErrInvalidRequest)
	}
	if spec.FreshVerification && (!rule.fresh || spec.Verification != record.VerificationRequired) {
		return fmt.Errorf("%w: fresh verification requires verify", ErrInvalidRequest)
	}
	if spec.Preparation != nil && spec.Preparation.SharedRelease && !rule.shared {
		return fmt.Errorf("%w: shared release applies only to bump", ErrInvalidRequest)
	}
	if spec.IncludeDependents && (!rule.dependents || spec.Verification != record.VerificationRequired || !local) {
		return fmt.Errorf("%w: dependent verification requires a local build configuration and verification", ErrInvalidRequest)
	}
	if rule.correction && (spec.Preparation == nil || spec.Preparation.Correction == nil) {
		return ErrInvalidRequest
	}
	return nil
}

func normalizeTargetBuilds(spec *record.JobSpec, _ actionRule) error {
	if len(spec.TargetBuilds) == 0 {
		return nil
	}
	if !spec.IncludeDependents || spec.Build == nil || spec.Build.Provider == "github" {
		return fmt.Errorf("%w: target builds require a local dependent plan", ErrInvalidRequest)
	}
	spec.TargetBuilds = maps.Clone(spec.TargetBuilds)
	for name, build := range spec.TargetBuilds {
		if !validToken(name) || strings.ContainsAny(name, "/\\") || build.Provider != spec.Build.Provider || build.Platform != spec.Build.Platform || build.Tests != spec.Build.Tests || build.FromSource != spec.Build.FromSource {
			return fmt.Errorf("%w: incompatible target build for %s", ErrInvalidRequest, name)
		}
		for _, root := range spec.Targets {
			if root.Name == name {
				return fmt.Errorf("%w: target image overrides a root", ErrInvalidRequest)
			}
		}
		if err := verify.ValidateConfig(build); err != nil {
			return fmt.Errorf("%w: %v", ErrInvalidRequest, err)
		}
		build.ProviderConfig = slices.Clone(build.ProviderConfig)
		spec.TargetBuilds[name] = build
	}
	return nil
}

// validateDestination checks the verification policy against the destination
// and the destination against the action.
func validateDestination(spec *record.JobSpec, rule actionRule) error {
	if spec.Verification != record.VerificationRequired && spec.Verification != record.VerificationSkipped {
		return fmt.Errorf("%w: an explicit verification policy is required", ErrInvalidRequest)
	}
	switch spec.Destination {
	case record.BranchReady:
		if spec.Verification != record.VerificationSkipped {
			return fmt.Errorf("%w: branch-ready requires explicitly skipped verification", ErrInvalidRequest)
		}
	case record.VerificationComplete:
		if spec.Verification != record.VerificationRequired {
			return fmt.Errorf("%w: verification-complete requires verification", ErrInvalidRequest)
		}
	case record.Published:
	default:
		return fmt.Errorf("%w: a valid destination is required", ErrInvalidRequest)
	}
	if rule.publication {
		if spec.Destination != record.Published || spec.Publication == nil || !publicationVerification(spec, spec.Publication.Unverified, true) {
			return fmt.Errorf("%w: publish requires publication intent, the published destination, and either passing verification configuration or an explicitly unverified publication", ErrInvalidRequest)
		}
	} else if !slices.Contains(rule.destinations, spec.Destination) {
		return fmt.Errorf("%w: %s must request %s", ErrInvalidRequest, spec.Action, rule.destinations[0])
	}
	if spec.PublishTo != nil {
		destination := *spec.PublishTo
		if !spec.Action.Prepares() || spec.Preparation == nil || spec.Destination != record.Published || spec.Publication != nil || !publicationVerification(spec, spec.Verification == record.VerificationSkipped, false) {
			return fmt.Errorf("%w: combined publication requires a preparation job that verifies or explicitly skips verification", ErrInvalidRequest)
		}
		if err := publish.ValidateDestination(destination); err != nil {
			return fmt.Errorf("%w: %v", ErrInvalidRequest, err)
		}
		spec.PublishTo = &destination
	}
	if spec.Action.Prepares() && spec.Destination == record.Published && spec.PublishTo == nil {
		return fmt.Errorf("%w: publication destination required", ErrInvalidRequest)
	}
	return nil
}

// publicationVerification reports whether a publishing job's verification
// intent is coherent: required, with a build when the action carries its own
// evidence, or explicitly skipped with nothing to build or reuse.
func publicationVerification(spec *record.JobSpec, unverified, ownEvidence bool) bool {
	if unverified {
		return spec.Verification == record.VerificationSkipped && spec.Build == nil && spec.BuildRequirements == nil
	}
	return spec.Verification == record.VerificationRequired && (!ownEvidence || spec.Build != nil)
}

func normalizePublication(spec *record.JobSpec, rule actionRule) error {
	if spec.Publication == nil {
		return nil
	}
	v := *spec.Publication
	if v.LocalBranch != "" && !git.ValidBranchName(v.LocalBranch) {
		return ErrInvalidRequest
	}
	if !rule.publication || v.Forge == "" || v.Repository == "" || v.HeadRepository == "" || !git.ValidBranchName(v.HeadBranch) || !git.ValidBranchName(v.BaseBranch) || v.PushURL == "" || v.BaseURL == "" || !filepath.IsAbs(v.LockDirectory) || !git.ValidObjectID(string(v.Desired.Head)) || v.Desired.Title == "" || (v.EvidenceAttempt == "") != v.Unverified || !v.Unverified && !validToken(string(v.EvidenceAttempt)) || v.ExpectedRemoteHead.Exists != (v.ExpectedRemoteHead.Commit != "") || v.ExpectedRemoteHead.Exists && !git.ValidObjectID(string(v.ExpectedRemoteHead.Commit)) {
		return ErrInvalidRequest
	}
	if v.ExpectedPR != nil {
		pr := *v.ExpectedPR
		v.ExpectedPR = &pr
	}
	spec.Publication = &v
	return nil
}

func normalizeBuild(spec *record.JobSpec, _ actionRule) error {
	if spec.Build == nil {
		return nil
	}
	if spec.BuildRequirements != nil {
		return fmt.Errorf("%w: select a build configuration or recorded-evidence requirements, not both", ErrInvalidRequest)
	}
	if spec.Verification != record.VerificationRequired {
		return fmt.Errorf("%w: build configuration requires verification", ErrInvalidRequest)
	}
	if err := verify.ValidateConfig(*spec.Build); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidRequest, err)
	}
	build := *spec.Build
	build.ProviderConfig = slices.Clone(build.ProviderConfig)
	spec.Build = &build
	return nil
}

func normalizeRequirements(spec *record.JobSpec, rule actionRule) error {
	if spec.BuildRequirements == nil {
		return nil
	}
	if !spec.Action.Prepares() || spec.Verification != record.VerificationRequired || spec.Destination == record.BranchReady {
		return fmt.Errorf("%w: recorded-evidence requirements require a verified preparation job", ErrInvalidRequest)
	}
	if err := verify.ValidateRequirements(*spec.BuildRequirements); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidRequest, err)
	}
	requirements := *spec.BuildRequirements
	spec.BuildRequirements = &requirements
	return nil
}

func validateVersion(spec *record.JobSpec, rule actionRule) error {
	if spec.Version != "" && (!rule.version || !validToken(spec.Version)) {
		return fmt.Errorf("%w: only bump accepts a nonempty version without whitespace or control characters", ErrInvalidRequest)
	}
	return nil
}

func normalizePreparation(spec *record.JobSpec, rule actionRule) error {
	if spec.Preparation == nil {
		return nil
	}
	choices := *spec.Preparation
	if correction := choices.Correction; correction != nil {
		copy := *correction
		choices.Correction = &copy
		captured := copy.Candidate != (record.Source{})
		if rule.correction != captured || !spec.Action.Prepares() || !validToken(string(copy.ChangeID)) || !validToken(string(copy.RevisionID)) || !git.ValidBranchName(copy.Branch) || !git.ValidObjectID(string(copy.PreviousHead)) || copy.RemoteHead != "" && !git.ValidObjectID(string(copy.RemoteHead)) || captured && (validateSource(copy.Candidate) != nil || copy.Candidate.Commit == "" || copy.Candidate.Base == "") {
			return ErrInvalidRequest
		}
	}
	if !spec.Action.Prepares() || spec.InputRevision != "" || spec.Source.Commit == "" || len(spec.Targets) != 1 {
		return fmt.Errorf("%w: preparation requires one committed source target and a branch-ready, verification, or publication destination", ErrInvalidRequest)
	}
	if !git.ValidBranchName(choices.SourceBranch) || choices.Author.Name == "" || choices.Author.Email == "" || strings.ContainsAny(choices.Author.Name+choices.Author.Email, "\x00\r\n<>") || !utf8.ValidString(choices.Author.Name+choices.Author.Email) {
		return fmt.Errorf("%w: preparation requires a source branch and valid author identity", ErrInvalidRequest)
	}
	for _, value := range []string{choices.Platform.OS, choices.Platform.Version, choices.Platform.Architecture} {
		if !validToken(value) {
			return fmt.Errorf("%w: preparation requires a complete platform", ErrInvalidRequest)
		}
	}
	if spec.Build != nil && spec.Build.Platform != choices.Platform {
		return fmt.Errorf("%w: preparation and build platforms disagree", ErrInvalidRequest)
	}
	if spec.BuildRequirements != nil && spec.BuildRequirements.Platform != choices.Platform {
		return fmt.Errorf("%w: preparation and recorded-evidence platforms disagree", ErrInvalidRequest)
	}
	spec.Preparation = &choices
	return nil
}

func normalizeCheckout(spec *record.JobSpec, rule actionRule) error {
	if spec.Checkout == nil {
		return nil
	}
	c := *spec.Checkout
	if !rule.checkout || spec.InputRevision != "" || !git.ValidObjectID(string(c.Head)) || c.ModifiedFiles < 0 || (c.Branch != "" && !git.ValidBranchName(c.Branch)) || (c.ModifiedFiles == 0 && spec.Source.Commit != c.Head) || (c.ModifiedFiles > 0 && spec.Source.Commit != "") {
		return fmt.Errorf("%w: invalid checkout provenance", ErrInvalidRequest)
	}
	spec.Checkout = &c
	return nil
}

// validateSourceSelection checks that a job names either an existing
// revision or a fresh source, never both.
func validateSourceSelection(spec *record.JobSpec, _ actionRule) error {
	if spec.InputRevision != "" {
		if spec.Source != (record.Source{}) {
			return fmt.Errorf("%w: omit source when selecting an existing revision", ErrInvalidRequest)
		}
		return nil
	}
	if spec.ChangeID != "" && spec.Preparation == nil {
		return fmt.Errorf("%w: an existing change requires an explicit input revision", ErrInvalidRequest)
	}
	return validateSource(spec.Source)
}

// normalizeTargets validates each target and canonicalizes their order and
// empty variant maps.
func normalizeTargets(spec *record.JobSpec, _ actionRule) error {
	if len(spec.Targets) == 0 {
		return fmt.Errorf("%w: at least one resolved target is required", ErrInvalidRequest)
	}
	spec.Targets = slices.Clone(spec.Targets)
	for i, target := range spec.Targets {
		if !validToken(target.Name) || strings.ContainsAny(target.Name, "/\\") ||
			!fs.ValidPath(target.Portfile) || path.Base(target.Portfile) != "Portfile" ||
			strings.ContainsRune(target.Portfile, '\\') || strings.IndexFunc(target.Portfile, unicode.IsControl) >= 0 ||
			(target.Subport != "" && (!validToken(target.Subport) || strings.ContainsAny(target.Subport, "/\\"))) {
			return fmt.Errorf("%w: invalid target %q or Portfile path %q", ErrInvalidRequest, target.Name, target.Portfile)
		}
		for variant := range target.Variants {
			if !validToken(variant) || strings.ContainsAny(variant, "/\\") || strings.HasPrefix(variant, "+") || strings.HasPrefix(variant, "-") {
				return fmt.Errorf("%w: invalid variant name %q", ErrInvalidRequest, variant)
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
			return fmt.Errorf("%w: duplicate target %q", ErrInvalidRequest, spec.Targets[i].Name)
		}
	}
	return nil
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
