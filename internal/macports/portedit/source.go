package portedit

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/portfile"
	"github.com/herbygillot/dockhand/internal/record"
)

type sourceInput struct {
	files           workspace
	before          macports.Snapshot
	primary, target record.Target
	info            macports.PortInfo
	data            []byte
}

func (s *Service) load(ctx context.Context, request Request) (_ *sourceInput, err error) {
	if s == nil || s.Ports == nil || request.Root == "" {
		return nil, fmt.Errorf("portedit: a disposable source workspace and MacPorts reader are required")
	}
	files := workspace{Root: request.Root}

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
	data, err := os.ReadFile(filepath.Join(files.Root, selected.Portfile))
	if err != nil {
		return nil, err
	}
	return &sourceInput{files: files, before: before, primary: targets[0], target: selected, info: info, data: data}, nil
}

type workspace struct{ Root string }

func (s *Service) evaluateEdit(ctx context.Context, request Request, input *sourceInput, contents []byte) (_ portfile.Edit, _ macports.Snapshot, _ string, err error) {
	edit := portfile.Edit{Path: input.target.Portfile, After: contents}
	path := filepath.Join(input.files.Root, input.target.Portfile)
	original, err := os.ReadFile(path)
	if err != nil {
		return edit, macports.Snapshot{}, "", err
	}
	if err = os.WriteFile(path, contents, 0600); err != nil {
		return edit, macports.Snapshot{}, "", err
	}
	defer func() { err = errors.Join(err, os.WriteFile(path, original, 0600)) }()
	bound, err := macports.NewContext(request.Source, input.files.Root, input.primary, input.before.Platform)
	if err != nil {
		return edit, macports.Snapshot{}, "", err
	}
	after, err := s.Ports.Evaluate(ctx, bound)
	if err == nil {
		err = checkSnapshot(after, bound)
	}
	// Probe snapshots describe uncommitted contents, not the immutable base tree.
	after.Source = record.Source{}
	return edit, after, input.files.Root, err
}
