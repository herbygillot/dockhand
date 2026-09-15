package prepare

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/dependency"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/upstream"
)

var (
	ErrNotImplemented = errors.New("prepare: requested transformation is not implemented")
	ErrUnsupported    = errors.New("prepare: source cannot be edited with the supported transformations")
	ErrFidelity       = errors.New("prepare: evaluation does not match the intended change")
)

type CommitIntent struct {
	Subject string
	Body    string
	Paths   []string
}

type Request struct {
	Action    record.Action
	Source    record.Source
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
	Base         record.Source
	Target       record.Target
	PreparedTree record.ObjectID
	Files        []git.FileEdit
	Commits      []CommitIntent
	Fidelity     []Fidelity
	Release      *record.Release
	Downloads    []Download
}

type Service struct {
	DependencyTools  dependency.Tools
	archiveDirectory string
	Repo             *git.Repository
	Ports            macports.Reader
	Upstream         *upstream.Service
	HTTP             *http.Client
	MaxDownloadBytes int64
}

func (s *Service) Prepare(ctx context.Context, request Request) (_ Result, err error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	if request.Action != record.BumpRevision && request.Action != record.Bump {
		return Result{}, fmt.Errorf("%w: %s", ErrNotImplemented, request.Action)
	}
	if request.Action == record.Bump && request.Release == nil {
		return Result{}, fmt.Errorf("prepare: a resolved release is required")
	}
	if request.Action == record.BumpRevision && request.Version != "" {
		return Result{}, fmt.Errorf("prepare: an explicit version applies only to bump")
	}
	input, err := s.load(ctx, request)
	if err != nil {
		return Result{}, err
	}
	defer func() { err = errors.Join(err, input.files.Close()) }()
	if request.Action == record.Bump {
		return s.prepareVersion(ctx, request, input)
	}
	revised, err := bumpRevision(input.data, input.target.Subport, input.info.Revision)
	if err != nil {
		return Result{}, err
	}
	edit, tree, after, root, err := s.evaluateEdit(ctx, request, input, revised)
	if err != nil {
		return Result{}, err
	}
	fidelity := revisionFidelity(input.before, after, input.target.Name, input.files.Root, root)
	result := Result{Base: request.Source, Target: input.target, Files: []git.FileEdit{edit}, Fidelity: []Fidelity{fidelity}}
	if len(fidelity.UnexpectedChanges) > 0 {
		return result, fmt.Errorf("%w: %v", ErrFidelity, fidelity.UnexpectedChanges)
	}
	result.PreparedTree = record.ObjectID(tree)
	result.Commits = []CommitIntent{{Subject: input.target.Name + ": revbump", Body: request.Reason, Paths: []string{input.target.Portfile}}}
	return result, nil
}
