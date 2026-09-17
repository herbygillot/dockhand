package preparation

import (
	"context"
	"errors"
	"fmt"
	"github.com/herbygillot/dockhand/internal/macports/fidelity"
	"net/http"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/dependency"
	"github.com/herbygillot/dockhand/internal/macports/portedit"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/upstream"
)

type Request = portedit.Request
type CommitIntent = portedit.CommitIntent

const GeneratedBy = portedit.GeneratedBy

var ErrUnsupported = portedit.ErrUnsupported
var ErrFidelity = portedit.ErrFidelity
var ErrNotImplemented = portedit.ErrNotImplemented

type Result struct {
	Scope        *record.ReleaseScope       `json:",omitempty"`
	Coverage     []portedit.ContextCoverage `json:",omitempty"`
	Base         record.Source
	Target       record.Target
	PreparedTree record.ObjectID
	Files        []git.FileEdit
	Commits      []CommitIntent
	Fidelity     []portedit.Fidelity
	Release      *record.Release
	Downloads    []portedit.Download
}
type Service struct {
	Repo             *git.Repository
	Ports            macports.Reader
	Upstream         *upstream.Service
	DependencyTools  dependency.Tools
	HTTP             *http.Client
	MaxDownloadBytes int64
}

func (s *Service) editor() *portedit.Service {
	return &portedit.Service{Ports: s.Ports, DependencyTools: s.DependencyTools, HTTP: s.HTTP, MaxDownloadBytes: s.MaxDownloadBytes}
}
func (s *Service) open(ctx context.Context, request Request) (*git.Snapshot, error) {
	if s == nil || s.Repo == nil {
		return nil, fmt.Errorf("preparation: Git repository is required")
	}
	if request.Source.Commit != "" {
		trees, err := s.Repo.CommitTrees(ctx, []string{string(request.Source.Commit)})
		if err != nil {
			return nil, err
		}
		if trees[string(request.Source.Commit)] != string(request.Source.Tree) {
			return nil, fmt.Errorf("preparation: source commit and tree disagree")
		}
	}
	if request.Source.Base != "" {
		if _, err := s.Repo.CommitTrees(ctx, []string{string(request.Source.Base)}); err != nil {
			return nil, err
		}
	}
	return s.Repo.Materialize(ctx, string(request.Source.Tree))
}
func (s *Service) ResolveRelease(ctx context.Context, request Request) (_ record.Release, err error) {
	files, err := s.open(ctx, request)
	if err != nil {
		return record.Release{}, err
	}
	defer func() { err = errors.Join(err, files.Close()) }()
	request.Root = files.Root
	if request.Action != record.Bump {
		return record.Release{}, fmt.Errorf("%w: release resolution requires a bump action", ErrNotImplemented)
	}
	probe, err := s.editor().Probe(ctx, probeSource(request))
	if err != nil {
		return record.Release{}, err
	}
	discovery, err := s.Upstream.Bind(probe)
	if err != nil {
		return record.Release{}, err
	}
	release, err := discovery.Resolve(ctx, request.Version)
	if err != nil {
		return record.Release{}, err
	}
	return release, probe.CheckRelease(ctx, release)
}
func (s *Service) Prepare(ctx context.Context, request Request) (_ Result, err error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	if err := request.Validate(); err != nil {
		return Result{}, err
	}
	files, err := s.open(ctx, request)
	if err != nil {
		return Result{}, err
	}
	defer func() { err = errors.Join(err, files.Close()) }()
	request.Root = files.Root
	var original macports.PortInfo
	if request.Action == record.Bump {
		if s.Upstream == nil {
			return Result{}, fmt.Errorf("preparation: upstream source checker required")
		}
		probe, err := s.editor().Probe(ctx, probeSource(request))
		if err != nil {
			return Result{}, err
		}
		original = probe.Port()
		if err := s.Upstream.Check(ctx, original, *request.Release); err != nil {
			return Result{}, err
		}
	}
	edited, err := s.editor().Prepare(ctx, request)
	result := Result{Scope: edited.Scope, Coverage: edited.Coverage, Base: edited.Base, Target: edited.Target, Fidelity: edited.Fidelity, Release: edited.Release, Downloads: edited.Downloads}
	if err != nil {
		return result, err
	}
	if request.Action == record.Bump {
		if err := s.Upstream.Check(ctx, original, *request.Release); err != nil {
			return result, err
		}
	}
	result.Commits = edited.Commits
	for _, edit := range edited.Files {
		before, _, err := s.Repo.File(ctx, string(request.Source.Tree), edit.Path)
		if err != nil {
			return result, err
		}
		result.Files = append(result.Files, git.FileEdit{Path: edit.Path, Before: before, After: edit.After, Mode: before.Mode})
	}
	if len(result.Files) == 0 {
		result.PreparedTree = request.Source.Tree
		return result, nil
	}
	tree, err := s.Repo.EditTree(ctx, string(request.Source.Tree), result.Files)
	if err != nil {
		return result, err
	}
	candidate, err := s.Repo.Materialize(ctx, tree)
	if err != nil {
		return result, err
	}
	defer func() { err = errors.Join(err, candidate.Close()) }()
	expected := result.Fidelity[len(result.Fidelity)-1].After
	source := record.Source{Tree: record.ObjectID(tree), Base: request.Source.Base}
	bound, err := macports.NewContext(source, candidate.Root, expected.Target, expected.Platform)
	if err != nil {
		return result, err
	}
	snapshot, err := s.Ports.Evaluate(ctx, bound)
	if err != nil {
		return result, err
	}
	if snapshot.Source != source {
		return result, fmt.Errorf("%w: stored candidate source identity differs", ErrFidelity)
	}
	if err = fidelity.Equivalent(expected, snapshot, files.Root, candidate.Root); err != nil {
		return result, err
	}
	result.Fidelity[len(result.Fidelity)-1].After = snapshot
	result.PreparedTree = record.ObjectID(tree)
	return result, nil
}

func probeSource(request Request) portedit.ProbeSource {
	return portedit.ProbeSource{SharedRelease: request.SharedRelease, Source: request.Source, Root: request.Root, Selection: request.Selection, Platform: request.Platform}
}
