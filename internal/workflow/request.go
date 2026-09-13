package workflow

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"maps"
	"path"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/herbygillot/dockhand/v2/internal/git"
	"github.com/herbygillot/dockhand/v2/internal/record"
	"github.com/herbygillot/dockhand/v2/internal/verify"
)

// normalizeSpec validates caller-supplied intent without consulting the state store
// or external services. It copies mutable inputs and canonicalizes target order
// and empty variant maps so equivalent requests have the same representation.
func normalizeSpec(spec record.JobSpec) (record.JobSpec, error) {
	if !utf8.ValidString(spec.Reason) || (spec.ChangeID != "" && !validToken(string(spec.ChangeID))) || (spec.InputRevision != "" && !validToken(string(spec.InputRevision))) {
		return record.JobSpec{}, fmt.Errorf("%w: invalid change ID, revision ID, or reason encoding", ErrInvalidRequest)
	}
	switch spec.Action {
	case record.Bump, record.BumpRevision, record.RefreshChecksums, record.Verify, record.Publish:
	case record.Rebase, record.Amend:
		return record.JobSpec{}, fmt.Errorf("%w: %s", ErrUnsupportedAction, spec.Action)
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
	if spec.Action == record.Publish && (spec.Destination != record.Published || spec.InputRevision == "") {
		return record.JobSpec{}, fmt.Errorf("%w: publish requires an existing revision and the published destination", ErrInvalidRequest)
	}
	if spec.Build != nil {
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
	if spec.Version != "" && (spec.Action != record.Bump || !validToken(spec.Version)) {
		return record.JobSpec{}, fmt.Errorf("%w: only bump accepts a nonempty version without whitespace or control characters", ErrInvalidRequest)
	}
	if spec.Preparation != nil {
		choices := *spec.Preparation
		if !preparationAction(spec.Action) || (spec.Action == record.Bump && spec.Version == "") || spec.InputRevision != "" || spec.Source.Commit == "" || len(spec.Targets) != 1 || spec.Destination == record.Published {
			return record.JobSpec{}, fmt.Errorf("%w: preparation requires one committed source target and a branch-ready or verification destination", ErrInvalidRequest)
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
		spec.Preparation = &choices
	}
	if spec.InputRevision != "" {
		if spec.Source != (record.Source{}) {
			return record.JobSpec{}, fmt.Errorf("%w: omit source when selecting an existing revision", ErrInvalidRequest)
		}
	} else {
		if spec.ChangeID != "" {
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
	slices.SortFunc(spec.Targets, func(a, b record.Target) int { return strings.Compare(targetKey(a), targetKey(b)) })
	for i := 1; i < len(spec.Targets); i++ {
		if targetKey(spec.Targets[i-1]) == targetKey(spec.Targets[i]) {
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

// targetKey provides deterministic ordering and identity for the complete target,
// including explicit variant choices. JSON encoding sorts the variant map keys.
func targetKey(target record.Target) string {
	data, _ := json.Marshal(target)
	return string(data)
}
