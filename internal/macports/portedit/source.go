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
	scope                *record.ReleaseScope
	versionInput         record.ReleaseInput
	files                workspace
	before               macports.Snapshot
	primary, target      record.Target
	info                 macports.PortInfo
	data                 []byte
	platformOperands     []string
	baselineObservations map[observationKey]macports.Observation
}

func (s *Service) load(ctx context.Context, request Request) (_ *sourceInput, err error) {
	if s == nil || s.Ports == nil || request.Root == "" {
		return nil, fmt.Errorf("portedit: a disposable source workspace and MacPorts reader are required")
	}
	if request.SharedRelease {
		if _, ok := s.Ports.(macports.Observer); !ok {
			return nil, fmt.Errorf("%w: shared releases require native declaration observation", ErrUnsupported)
		}
	}
	files := workspace{Root: request.Root}

	tree, err := macports.NewTree(request.Source, files.Root, request.Platform)
	if err != nil {
		return nil, err
	}
	targets, err := s.Ports.Resolve(ctx, tree, request.Selection)
	if err != nil {
		return nil, err
	}
	if len(targets) != 1 {
		return nil, fmt.Errorf("%w: select one port", ErrUnsupported)
	}
	selected := targets[0]
	if selected.Subport != "" {
		targets, err = s.Ports.Resolve(ctx, tree, macports.Selection{Selector: selected.Portfile, Variants: selected.Variants})
		if err != nil {
			return nil, err
		}
		if len(targets) != 1 || targets[0].Subport != "" {
			return nil, fmt.Errorf("%w: select one owning Portfile", ErrUnsupported)
		}
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

func (s *Service) evaluateEdit(ctx context.Context, request Request, input *sourceInput, contents []byte) (portfile.Edit, macports.Snapshot, string, error) {
	return s.evaluateContents(ctx, request, input, contents, false)
}

func (s *Service) evaluateContents(ctx context.Context, request Request, input *sourceInput, contents []byte, selectedOnly bool) (_ portfile.Edit, _ macports.Snapshot, _ string, err error) {
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
	target := input.primary
	if selectedOnly {
		target = input.target
	}
	bound, err := macports.NewContext(request.Source, input.files.Root, target, input.before.Platform)
	if err != nil {
		return edit, macports.Snapshot{}, "", err
	}
	var after macports.Snapshot
	if reader, ok := s.Ports.(macports.SelectedReader); ok && selectedOnly {
		after, err = reader.EvaluateSelected(ctx, bound)
	} else {
		after, err = s.Ports.Evaluate(ctx, bound)
	}
	if err == nil {
		err = checkSnapshot(after, bound)
	}
	// Probe snapshots describe uncommitted contents, not the immutable base tree.
	after.Source = record.Source{}
	return edit, after, input.files.Root, err
}
