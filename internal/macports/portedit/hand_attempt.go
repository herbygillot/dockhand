package portedit

import (
	"context"
	"errors"

	"github.com/herbygillot/dockhand/internal/macports/portfile"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/refusal"
)

// HandAttempt is dockhand's best attempt at a version update it refused,
// for a person to finish by hand (update --hand-edit, Then's item 3): the
// refused edit where the refusal came with one, as strict fidelity's
// does, else the version written as its one literal; its revision reset
// where that can be; and its checksums refreshed for the new version's
// archives where they can be. What couldn't be done is in Left, with why.
// It's no edit of dockhand's own, whose fidelity is proven: the person
// takes it as theirs.
func (s *Service) HandAttempt(ctx context.Context, request Request, refused Result) (_ Result, err error) {
	if request.Action != model.EditUpdate {
		return Result{}, ErrUnsupported.Because("a hand edit's attempt is a version update's")
	}
	input, err := s.load(ctx, &request)
	if err != nil {
		return Result{}, err
	}
	defer func() { err = errors.Join(err, input.Close()) }()
	result := Result{Base: request.Source, Target: input.target, Release: request.Release}
	var contents []byte
	for _, file := range refused.Files {
		if file.Path == input.target.Portfile && !file.Delete {
			contents = file.After
		}
	}
	if contents == nil && refused.Versioned != nil {
		contents = refused.Versioned
	}
	if contents == nil {
		if contents, err = portfile.SetVersionLiteral(input.data, input.info.Version, request.Version); err != nil {
			return Result{}, err
		}
		if reset, err := s.resetRevision(ctx, request, input, contents); err == nil {
			contents = reset
		} else {
			result.Left = append(result.Left, ChecksumLeft{Kind: LeftDeclaration, What: "the revision", Reason: refusal.WordsOf(err)})
		}
	}
	// The new version's archives, fetched and their checksums written, as
	// a refresh writes them: only the checksums change from contents.
	evaluated, err := s.evaluateEdit(ctx, input, contents)
	if err == nil {
		refreshed, refreshErr := s.refreshChecksums(ctx, request, input.derive(contents, evaluated.after))
		err = refreshErr
		if err == nil && len(refreshed.Files) > 0 {
			contents = refreshed.Files[0].After
		}
		result.Downloads, result.Left = refreshed.Downloads, append(result.Left, refreshed.Left...)
	}
	if err != nil {
		result.Left = append(result.Left, ChecksumLeft{Kind: LeftDeclaration, What: "the checksums", Reason: refusal.WordsOf(err)})
	}
	result.Files = []portfile.Edit{{Path: input.target.Portfile, After: contents}}
	return result, nil
}
