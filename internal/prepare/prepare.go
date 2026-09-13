package prepare

import (
	"context"
	"errors"
	"fmt"

	"github.com/herbygillot/dockhand/v2/internal/git"
	"github.com/herbygillot/dockhand/v2/internal/macports"
	"github.com/herbygillot/dockhand/v2/internal/record"
	"github.com/herbygillot/dockhand/v2/internal/upstream"
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
}

type Service struct {
	Repo     *git.Repository
	Ports    macports.Reader
	Upstream *upstream.Service
}

func (s *Service) Prepare(ctx context.Context, request Request) (_ Result, err error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	if request.Action != record.BumpRevision {
		return Result{}, fmt.Errorf("%w: %s (version bumps need release resolution, downloads, and checksum refresh)", ErrNotImplemented, request.Action)
	}
	if s == nil || s.Repo == nil || s.Ports == nil {
		return Result{}, fmt.Errorf("prepare: Git and MacPorts are required")
	}
	if request.Version != "" {
		return Result{}, fmt.Errorf("prepare: an explicit version applies only to bump")
	}
	if request.Source.Commit != "" {
		trees, err := s.Repo.CommitTrees(ctx, []string{string(request.Source.Commit)})
		if err != nil {
			return Result{}, err
		}
		if trees[string(request.Source.Commit)] != string(request.Source.Tree) {
			return Result{}, fmt.Errorf("prepare: source commit and tree disagree")
		}
	}
	if request.Source.Base != "" {
		if _, err := s.Repo.CommitTrees(ctx, []string{string(request.Source.Base)}); err != nil {
			return Result{}, err
		}
	}
	files, err := s.Repo.Materialize(ctx, string(request.Source.Tree))
	if err != nil {
		return Result{}, err
	}
	defer func() { err = errors.Join(err, files.Close()) }()
	tree, err := macports.NewTree(request.Source, files.Root, request.Platform)
	if err != nil {
		return Result{}, err
	}
	selection := request.Selection
	selection.Subport = ""
	targets, err := s.Ports.Resolve(ctx, tree, selection)
	if err != nil {
		return Result{}, err
	}
	if len(targets) != 1 || targets[0].Subport != "" {
		return Result{}, fmt.Errorf("%w: select one Portfile", ErrUnsupported)
	}
	bound, err := tree.Select(targets[0])
	if err != nil {
		return Result{}, err
	}
	before, err := s.Ports.Evaluate(ctx, bound)
	if err != nil {
		return Result{}, err
	}
	if err := checkSnapshot(before, bound); err != nil {
		return Result{}, err
	}
	selected := targets[0]
	if request.Selection.Subport != "" {
		selected.Name, selected.Subport = request.Selection.Subport, request.Selection.Subport
	}
	info, ok := before.Ports[selected.Name]
	if !ok {
		return Result{}, fmt.Errorf("%w: subport %s was not evaluated", ErrUnsupported, selected.Name)
	}
	original, data, err := s.Repo.File(ctx, string(request.Source.Tree), selected.Portfile)
	if err != nil {
		return Result{}, err
	}
	if !original.Exists {
		return Result{}, fmt.Errorf("prepare: selected Portfile is missing")
	}
	revised, err := bumpRevision(data, selected.Subport, info.Revision)
	if err != nil {
		return Result{}, err
	}
	edit := git.FileEdit{Path: selected.Portfile, Before: original, After: revised, Mode: original.Mode}
	candidate, err := s.Repo.EditTree(ctx, string(request.Source.Tree), []git.FileEdit{edit})
	if err != nil {
		return Result{}, err
	}
	afterFiles, err := s.Repo.Materialize(ctx, candidate)
	if err != nil {
		return Result{}, err
	}
	defer func() { err = errors.Join(err, afterFiles.Close()) }()
	afterSource := record.Source{Tree: record.ObjectID(candidate), Base: request.Source.Base}
	afterContext, err := macports.NewContext(afterSource, afterFiles.Root, targets[0], before.Platform)
	if err != nil {
		return Result{}, err
	}
	after, err := s.Ports.Evaluate(ctx, afterContext)
	if err != nil {
		return Result{}, err
	}
	if err := checkSnapshot(after, afterContext); err != nil {
		return Result{}, err
	}
	fidelity := revisionFidelity(before, after, selected.Name, files.Root, afterFiles.Root)
	result := Result{Base: request.Source, Target: selected, Files: []git.FileEdit{edit}, Fidelity: []Fidelity{fidelity}}
	if len(fidelity.UnexpectedChanges) != 0 {
		return result, fmt.Errorf("%w: %v", ErrFidelity, fidelity.UnexpectedChanges)
	}
	result.PreparedTree = record.ObjectID(candidate)
	result.Commits = []CommitIntent{{Subject: selected.Name + ": revbump", Body: request.Reason, Paths: []string{selected.Portfile}}}
	return result, nil
}
