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
	"github.com/herbygillot/dockhand/v2/internal/model"
)

func normalizeSpec(spec model.JobSpec) (model.JobSpec, error) {
	if !utf8.ValidString(spec.Reason) || (spec.ChangeID != "" && !validToken(string(spec.ChangeID))) || (spec.InputRevision != "" && !validToken(string(spec.InputRevision))) {
		return model.JobSpec{}, fmt.Errorf("%w: invalid change ID, revision ID, or reason encoding", ErrInvalidRequest)
	}
	switch spec.Action {
	case model.Bump, model.BumpRevision, model.RefreshChecksums, model.Verify, model.Publish:
	case model.Rebase, model.Amend:
		return model.JobSpec{}, fmt.Errorf("%w: %s", ErrUnsupportedAction, spec.Action)
	default:
		return model.JobSpec{}, fmt.Errorf("%w: unknown action %q", ErrInvalidRequest, spec.Action)
	}
	if spec.Verification != model.VerificationRequired && spec.Verification != model.VerificationSkipped {
		return model.JobSpec{}, fmt.Errorf("%w: an explicit verification policy is required", ErrInvalidRequest)
	}
	switch spec.Destination {
	case model.BranchReady:
		if spec.Verification != model.VerificationSkipped {
			return model.JobSpec{}, fmt.Errorf("%w: branch-ready requires explicitly skipped verification", ErrInvalidRequest)
		}
	case model.VerificationComplete:
		if spec.Verification != model.VerificationRequired {
			return model.JobSpec{}, fmt.Errorf("%w: verification-complete requires verification", ErrInvalidRequest)
		}
	case model.Published:
	default:
		return model.JobSpec{}, fmt.Errorf("%w: a valid destination is required", ErrInvalidRequest)
	}
	if spec.Action == model.Verify && spec.Destination != model.VerificationComplete {
		return model.JobSpec{}, fmt.Errorf("%w: verify must request verification-complete", ErrInvalidRequest)
	}
	if spec.Action == model.Publish && (spec.Destination != model.Published || spec.InputRevision == "") {
		return model.JobSpec{}, fmt.Errorf("%w: publish requires an existing revision and the published destination", ErrInvalidRequest)
	}
	if spec.Version != "" && (spec.Action != model.Bump || !validToken(spec.Version)) {
		return model.JobSpec{}, fmt.Errorf("%w: only bump accepts a nonempty version without whitespace or control characters", ErrInvalidRequest)
	}
	if spec.InputRevision != "" {
		if spec.Source != (model.Source{}) {
			return model.JobSpec{}, fmt.Errorf("%w: omit source when selecting an existing revision", ErrInvalidRequest)
		}
	} else {
		if spec.ChangeID != "" {
			return model.JobSpec{}, fmt.Errorf("%w: an existing change requires an explicit input revision", ErrInvalidRequest)
		}
		if err := validateSource(spec.Source); err != nil {
			return model.JobSpec{}, err
		}
	}
	if len(spec.Targets) == 0 {
		return model.JobSpec{}, fmt.Errorf("%w: at least one resolved target is required", ErrInvalidRequest)
	}
	spec.Targets = slices.Clone(spec.Targets)
	for i, target := range spec.Targets {
		if !validToken(target.Name) || strings.ContainsAny(target.Name, "/\\") ||
			!fs.ValidPath(target.Portfile) || path.Base(target.Portfile) != "Portfile" ||
			strings.ContainsRune(target.Portfile, '\\') || strings.IndexFunc(target.Portfile, unicode.IsControl) >= 0 ||
			(target.Subport != "" && (!validToken(target.Subport) || strings.ContainsAny(target.Subport, "/\\"))) {
			return model.JobSpec{}, fmt.Errorf("%w: invalid target %q or Portfile path %q", ErrInvalidRequest, target.Name, target.Portfile)
		}
		for variant := range target.Variants {
			if !validToken(variant) || strings.ContainsAny(variant, "/\\") || strings.HasPrefix(variant, "+") || strings.HasPrefix(variant, "-") {
				return model.JobSpec{}, fmt.Errorf("%w: invalid variant name %q", ErrInvalidRequest, variant)
			}
		}
		if len(target.Variants) == 0 {
			target.Variants = nil
		} else {
			target.Variants = maps.Clone(target.Variants)
		}
		spec.Targets[i] = target
	}
	slices.SortFunc(spec.Targets, func(a, b model.Target) int { return strings.Compare(targetKey(a), targetKey(b)) })
	for i := 1; i < len(spec.Targets); i++ {
		if targetKey(spec.Targets[i-1]) == targetKey(spec.Targets[i]) {
			return model.JobSpec{}, fmt.Errorf("%w: duplicate target %q", ErrInvalidRequest, spec.Targets[i].Name)
		}
	}
	return spec, nil
}

func validateSource(source model.Source) error {
	if !git.ValidObjectID(string(source.Tree)) {
		return fmt.Errorf("%w: an immutable source tree ID is required", ErrInvalidRequest)
	}
	for _, id := range []model.ObjectID{source.Commit, source.Base} {
		if id != "" && (!git.ValidObjectID(string(id)) || len(id) != len(source.Tree)) {
			return fmt.Errorf("%w: invalid source commit or base ID", ErrInvalidRequest)
		}
	}
	return nil
}

func validToken(value string) bool {
	return value != "" && utf8.ValidString(value) && strings.IndexFunc(value, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) == -1
}

func targetKey(target model.Target) string {
	data, _ := json.Marshal(target)
	return string(data)
}
