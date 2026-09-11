package prepare

import (
	"context"
	"errors"

	"github.com/herbygillot/dockhand/v2/internal/macports"
	"github.com/herbygillot/dockhand/v2/internal/model"
	"github.com/herbygillot/dockhand/v2/internal/upstream"
)

var ErrNotImplemented = errors.New("prepare: source preparation is not implemented")

type FilePrecondition struct {
	Exists bool
	Blob   model.ObjectID
	Mode   uint32
}

// Path is relative to the ports tree, including for shared PortGroups.
type FileEdit struct {
	Path   string
	Before FilePrecondition
	After  []byte
	Mode   uint32
	Delete bool
}

type CommitIntent struct {
	Subject     string
	Body        string
	AuthorName  string
	AuthorEmail string
	Paths       []string
}

type Request struct {
	Action  model.Action
	Context macports.Context
	Version string
	Reason  string
	Edits   []FileEdit
}

type Fidelity struct {
	Before            macports.Snapshot
	After             macports.Snapshot
	ExpectedChanges   []string
	UnexpectedChanges []string
}

type Result struct {
	Base         model.Source
	PreparedTree model.ObjectID
	Files        []FileEdit
	Commits      []CommitIntent
	Fidelity     []Fidelity
}

type Service struct {
	Ports    macports.Reader
	Upstream *upstream.Service
}

func (s *Service) Prepare(ctx context.Context, request Request) (Result, error) {
	return Result{}, ErrNotImplemented
}
