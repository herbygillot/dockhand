package preparation

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/dependency"
	"github.com/herbygillot/dockhand/internal/macports/fidelity"
	"github.com/herbygillot/dockhand/internal/macports/patchcheck"
	"github.com/herbygillot/dockhand/internal/macports/portedit"
	"github.com/herbygillot/dockhand/internal/macports/portedit/archives"
	"github.com/herbygillot/dockhand/internal/macports/workspace"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/upstream"
)

type Request = portedit.Request
type CommitIntent = portedit.CommitIntent
type GoToolchain = portedit.GoToolchain

var ErrUnsupported = portedit.ErrUnsupported
var ErrFidelity = portedit.ErrFidelity
var ErrNotImplemented = portedit.ErrNotImplemented

// Result is the editor's result, carried whole, with its files as Git
// edits of the source tree and the tree they make. Prepared is bound to
// the committed tree's source identity once the candidate is written, and
// Commits wait for the second upstream check.
type Result struct {
	portedit.Result
	PreparedTree model.ObjectID
	Files        []git.FileEdit
}

// UncheckedPatches names the declared patches no check reached before the
// build, which applies them: a git-fetched port's, whose source isn't
// extracted here, or ones outside the source directory.
func (r Result) UncheckedPatches() []string {
	var names []string
	for _, patch := range r.Patches {
		if !patch.Checked {
			names = append(names, patch.Name+": "+patch.Detail)
		}
	}
	return names
}

// PatchProblems names the declared patches that no longer apply to the candidate source.
func (r Result) PatchProblems() []string {
	var problems []string
	for _, patch := range patchcheck.Rejected(r.Patches) {
		problems = append(problems, patch.Name+": "+patch.Detail)
	}
	return problems
}

type Service struct {
	Repo             *git.Repository
	Ports            macports.Evaluator
	Upstream         *upstream.Service
	DependencyTools  dependency.Tools
	HTTP             *http.Client
	MaxDownloadBytes int64
	// Workspaces hands out one projection per source; nil opens one per
	// preparation.
	Workspaces *workspace.Registry
}

func (s *Service) editor() *portedit.Service {
	editor := &portedit.Service{Ports: s.Ports, DependencyTools: s.DependencyTools, Archives: archives.Client{HTTP: s.HTTP, MaxBytes: s.MaxDownloadBytes}}
	if s.Upstream != nil {
		editor.Manifests = s.Upstream
	}
	return editor
}

// open projects the request's source: a workspace that materializes the
// selected port's directory and _resources when the editor resolves the
// target, and the whole tree only if a consumer asks for it.
func (s *Service) open(ctx context.Context, request Request) (*workspace.Workspace, func() error, error) {
	if s == nil || s.Repo == nil {
		return nil, nil, fmt.Errorf("preparation: Git repository is required")
	}
	if request.Source.Commit != "" {
		trees, err := s.Repo.CommitTrees(ctx, []string{string(request.Source.Commit)})
		if err != nil {
			return nil, nil, err
		}
		if trees[string(request.Source.Commit)] != string(request.Source.Tree) {
			return nil, nil, fmt.Errorf("preparation: source commit and tree disagree")
		}
	}
	if request.Source.Base != "" {
		if _, err := s.Repo.CommitTrees(ctx, []string{string(request.Source.Base)}); err != nil {
			return nil, nil, err
		}
	}
	return s.Workspaces.Acquire(ctx, s.Repo, request.Source)
}
func (s *Service) ResolveRelease(ctx context.Context, request Request) (_ model.Release, err error) {
	files, done, err := s.open(ctx, request)
	if err != nil {
		return model.Release{}, err
	}
	defer func() { err = errors.Join(err, done()) }()
	request.Workspace = files
	if request.Action != model.EditUpdate {
		return model.Release{}, fmt.Errorf("%w: release resolution requires a bump action", ErrNotImplemented)
	}
	probe, err := s.editor().Probe(ctx, probeSource(request))
	if err != nil {
		return model.Release{}, err
	}
	defer func() { err = errors.Join(err, probe.Close()) }()
	discovery, err := s.Upstream.Bind(probe)
	if err != nil {
		return model.Release{}, err
	}
	release, err := discovery.Resolve(ctx, request.Version)
	if err != nil {
		return model.Release{}, err
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
	files, done, err := s.open(ctx, request)
	if err != nil {
		return Result{}, err
	}
	defer func() { err = errors.Join(err, done()) }()
	request.Workspace = files
	var original macports.PortInfo
	if request.Action == model.EditUpdate {
		if s.Upstream == nil {
			return Result{}, fmt.Errorf("preparation: upstream source checker required")
		}
		probe, err := s.editor().Probe(ctx, probeSource(request))
		if err != nil {
			return Result{}, err
		}
		original = probe.Port()
		if err := probe.Close(); err != nil {
			return Result{}, err
		}
		if err := s.Upstream.Check(ctx, original, *request.Release); err != nil {
			return Result{}, err
		}
	}
	edited, err := s.editor().Prepare(ctx, request)
	result := Result{Result: edited}
	result.Result.Files, result.Commits = nil, nil
	if err != nil {
		return result, err
	}
	if request.Action == model.EditUpdate {
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
	// The committed candidate is evaluated once more from git, in a
	// projection of its own tree holding the target's directory, to prove
	// the stored tree evaluates as the prepared overlay did.
	expected := result.Prepared
	source := model.Source{Tree: model.ObjectID(tree), Base: request.Source.Base}
	candidate, err := workspace.Open(ctx, s.Repo, source)
	if err != nil {
		return result, err
	}
	defer func() { err = errors.Join(err, candidate.Close()) }()
	if err = candidate.EnsurePort(ctx, expected.Target); err != nil {
		return result, err
	}
	bound, err := candidate.Context(expected.Target, expected.Platform)
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
	if err = fidelity.Equivalent(expected, snapshot); err != nil {
		return result, err
	}
	// The committed candidate is the prepared result, and the last report
	// records it as evidence.
	result.Prepared = snapshot
	result.Fidelity[len(result.Fidelity)-1].After = snapshot
	result.PreparedTree = model.ObjectID(tree)
	return result, nil
}

func probeSource(request Request) portedit.ProbeSource {
	return portedit.ProbeSource{EditIntent: request.EditIntent, Source: request.Source, Workspace: request.Workspace, Selection: request.Selection, Platform: request.Platform}
}
