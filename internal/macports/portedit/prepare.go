package portedit

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/dependency"
	"github.com/herbygillot/dockhand/internal/macports/portfile"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/upstream"
)

var (
	ErrNotImplemented = errors.New("portedit: requested transformation is not implemented")
	ErrUnsupported    = errors.New("portedit: source cannot be edited with the supported transformations")
	ErrFidelity       = errors.New("portedit: evaluation does not match the intended change")
)

type CommitIntent struct {
	Subject string
	Body    string
	Paths   []string
}

type Request struct {
	Action record.Action
	Source record.Source
	// Root is an exclusively owned disposable source snapshot, never a user checkout.
	Root      string
	Selection macports.Selection
	Platform  record.Platform
	Version   string
	Reason    string
	Release   *record.Release
}

type Fidelity struct {
	Before            macports.Snapshot
	After             macports.Snapshot
	ExpectedChanges   []string
	UnexpectedChanges []string
}

type Result struct {
	Base      record.Source
	Target    record.Target
	Files     []portfile.Edit
	Commits   []CommitIntent
	Fidelity  []Fidelity
	Release   *record.Release
	Downloads []Download
}

type Service struct {
	DependencyTools  dependency.Tools
	archiveDirectory string
	Ports            macports.Reader
	Upstream         *upstream.Service
	HTTP             *http.Client
	MaxDownloadBytes int64
}

func (s *Service) Prepare(ctx context.Context, request Request) (_ Result, err error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	if err := request.Validate(); err != nil {
		return Result{}, err
	}
	input, err := s.load(ctx, request)
	if err != nil {
		return Result{}, err
	}
	if request.Action == record.Bump {
		return s.prepareVersion(ctx, request, input)
	}
	revised, err := bumpRevision(input.data, input.target.Subport, input.info.Revision)
	if err != nil {
		return Result{}, err
	}
	edit, after, root, err := s.evaluateEdit(ctx, request, input, revised)
	if err != nil {
		return Result{}, err
	}
	fidelity := revisionFidelity(input.before, after, input.target.Name, input.files.Root, root)
	result := Result{Base: request.Source, Target: input.target, Files: []portfile.Edit{edit}, Fidelity: []Fidelity{fidelity}}
	if len(fidelity.UnexpectedChanges) > 0 {
		return result, fmt.Errorf("%w: %v", ErrFidelity, fidelity.UnexpectedChanges)
	}
	result.Commits = []CommitIntent{{Subject: input.target.Name + ": revbump", Body: request.Reason, Paths: []string{input.target.Portfile}}}
	return result, nil
}

func (request Request) Validate() error {
	if request.Action != record.BumpRevision && request.Action != record.Bump {
		return fmt.Errorf("%w: %s", ErrNotImplemented, request.Action)
	}
	if request.Action == record.Bump && request.Release == nil {
		return fmt.Errorf("portedit: a resolved release is required")
	}
	if request.Action == record.BumpRevision && request.Version != "" {
		return fmt.Errorf("portedit: an explicit version applies only to bump")
	}
	return nil
}
