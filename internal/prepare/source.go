package prepare

import (
	"context"
	"errors"
	"fmt"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/record"
)

type sourceInput struct {
	files           *git.Snapshot
	before          macports.Snapshot
	primary, target record.Target
	info            macports.PortInfo
	original        git.FileState
	data            []byte
}

func (s *Service) load(ctx context.Context, request Request) (_ *sourceInput, err error) {
	if s == nil || s.Repo == nil || s.Ports == nil {
		return nil, fmt.Errorf("prepare: Git and MacPorts are required")
	}
	if request.Source.Commit != "" {
		trees, err := s.Repo.CommitTrees(ctx, []string{string(request.Source.Commit)})
		if err != nil {
			return nil, err
		}
		if trees[string(request.Source.Commit)] != string(request.Source.Tree) {
			return nil, fmt.Errorf("prepare: source commit and tree disagree")
		}
	}
	if request.Source.Base != "" {
		if _, err := s.Repo.CommitTrees(ctx, []string{string(request.Source.Base)}); err != nil {
			return nil, err
		}
	}
	files, err := s.Repo.Materialize(ctx, string(request.Source.Tree))
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			err = errors.Join(err, files.Close())
		}
	}()
	tree, err := macports.NewTree(request.Source, files.Root, request.Platform)
	if err != nil {
		return nil, err
	}
	selection := request.Selection
	selection.Subport = ""
	targets, err := s.Ports.Resolve(ctx, tree, selection)
	if err != nil {
		return nil, err
	}
	if len(targets) != 1 || targets[0].Subport != "" {
		return nil, fmt.Errorf("%w: select one Portfile", ErrUnsupported)
	}
	bound, err := tree.Select(targets[0])
	if err != nil {
		return nil, err
	}
	before, err := s.Ports.Evaluate(ctx, bound)
	if err != nil {
		return nil, err
	}
	if err := checkSnapshot(before, bound); err != nil {
		return nil, err
	}
	selected := targets[0]
	if request.Selection.Subport != "" {
		selected.Name, selected.Subport = request.Selection.Subport, request.Selection.Subport
	}
	info, ok := before.Ports[selected.Name]
	if !ok {
		return nil, fmt.Errorf("%w: subport %s was not evaluated", ErrUnsupported, selected.Name)
	}
	original, data, err := s.Repo.File(ctx, string(request.Source.Tree), selected.Portfile)
	if err != nil {
		return nil, err
	}
	if !original.Exists {
		return nil, fmt.Errorf("prepare: selected Portfile is missing")
	}
	return &sourceInput{files: files, before: before, primary: targets[0], target: selected, info: info, original: original, data: data}, nil
}

func (s *Service) evaluateEdit(ctx context.Context, request Request, input *sourceInput, contents []byte) (_ git.FileEdit, _ string, _ macports.Snapshot, _ string, err error) {
	edit := git.FileEdit{Path: input.target.Portfile, Before: input.original, After: contents, Mode: input.original.Mode}
	candidate, err := s.Repo.EditTree(ctx, string(request.Source.Tree), []git.FileEdit{edit})
	if err != nil {
		return edit, "", macports.Snapshot{}, "", err
	}
	files, err := s.Repo.Materialize(ctx, candidate)
	if err != nil {
		return edit, "", macports.Snapshot{}, "", err
	}
	defer func() { err = errors.Join(err, files.Close()) }()
	source := record.Source{Tree: record.ObjectID(candidate), Base: request.Source.Base}
	bound, err := macports.NewContext(source, files.Root, input.primary, input.before.Platform)
	if err != nil {
		return edit, "", macports.Snapshot{}, "", err
	}
	after, err := s.Ports.Evaluate(ctx, bound)
	if err == nil {
		err = checkSnapshot(after, bound)
	}
	return edit, candidate, after, files.Root, err
}

func (s *Service) ResolveRelease(ctx context.Context, request Request) (_ record.Release, err error) {
	if request.Action != record.Bump {
		return record.Release{}, fmt.Errorf("%w: release resolution requires a bump action", ErrNotImplemented)
	}
	input, err := s.load(ctx, request)
	if err != nil {
		return record.Release{}, err
	}
	defer func() { err = errors.Join(err, input.files.Close()) }()
	if input.target.Subport != "" {
		return record.Release{}, fmt.Errorf("%w: version bumps currently select the primary port", ErrUnsupported)
	}
	return s.Upstream.Resolve(ctx, input.info, request.Version)
}
