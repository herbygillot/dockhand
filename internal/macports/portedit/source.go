package portedit

import (
	"context"
	"errors"
	"fmt"
	"github.com/herbygillot/dockhand/internal/macports/fidelity"
	"os"
	"path/filepath"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/portfile"
	"github.com/herbygillot/dockhand/internal/record"
)

// sourceInput is one editing session: the workspace, the evaluated baseline,
// the selected and owning targets, and the original Portfile contents. Fields
// after data are filled in as preparation learns more about the source.
type sourceInput struct {
	scope                *record.ReleaseScope
	versionInput         record.ReleaseInput
	files                *workspace
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
	files := &workspace{root: request.Root, source: request.Source}

	tree, err := macports.NewTree(request.Source, files.root, request.Platform)
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
	if err := fidelity.CheckSnapshot(before, bound); err != nil {
		return nil, err
	}
	info, ok := before.Ports[selected.Name]
	if !ok {
		return nil, fmt.Errorf("%w: subport %s was not evaluated", ErrUnsupported, selected.Name)
	}
	data, err := os.ReadFile(files.path(selected.Portfile))
	if err != nil {
		return nil, err
	}
	return &sourceInput{files: files, before: before, primary: targets[0], target: selected, info: info, data: data}, nil
}

// workspace is the exclusively owned, disposable source snapshot an editing
// session probes: never a user checkout. It owns the paths under its root and
// the cycle that writes candidate contents over a file, evaluates, and restores
// the original, so callers never touch the snapshot directly.
type workspace struct {
	root   string
	source record.Source
}

func (w *workspace) path(relative string) string {
	return filepath.Join(w.root, filepath.FromSlash(relative))
}

// withContents runs fn with contents written over the named file, then
// restores the original even when fn fails.
func (w *workspace) withContents(relative string, contents []byte, fn func() error) (err error) {
	path := w.path(relative)
	original, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err = os.WriteFile(path, contents, 0600); err != nil {
		return err
	}
	defer func() { err = errors.Join(err, os.WriteFile(path, original, 0600)) }()
	return fn()
}

// portfile is the selected target's Portfile inside the workspace.
func (i *sourceInput) portfile() string { return i.files.path(i.target.Portfile) }

// portdir is the selected target's port directory inside the workspace.
func (i *sourceInput) portdir() string { return filepath.Dir(i.portfile()) }

// context binds the workspace to the owning Portfile, or to the selected
// subport alone for counterfactual probes.
func (i *sourceInput) context(platform record.Platform, selectedOnly bool) (macports.Context, error) {
	target := i.primary
	if selectedOnly {
		target = i.target
	}
	return macports.NewContext(i.files.source, i.files.root, target, platform)
}

// evaluation is one candidate's edit and its evaluated snapshot.
type evaluation struct {
	edit  portfile.Edit
	after macports.Snapshot
}

func (s *Service) evaluateEdit(ctx context.Context, input *sourceInput, contents []byte) (evaluation, error) {
	return s.evaluateContents(ctx, s.Ports, input, contents, false)
}

// evaluateCandidate omits sibling metadata for probes; full edit validation
// uses evaluateEdit so unrelated subport changes remain visible.
func (s *Service) evaluateCandidate(ctx context.Context, input *sourceInput, contents []byte) (evaluation, error) {
	return s.evaluateContents(ctx, s.Ports, input, contents, true)
}

// snapshotEvaluator is the reader used for one evaluation: the service's
// reader, or a batch bound to the workspace tree.
type snapshotEvaluator interface {
	Evaluate(context.Context, macports.Context) (macports.Snapshot, error)
}

func (s *Service) evaluateContents(ctx context.Context, reader snapshotEvaluator, input *sourceInput, contents []byte, selectedOnly bool) (evaluation, error) {
	result := evaluation{edit: portfile.Edit{Path: input.target.Portfile, After: contents}}
	err := input.files.withContents(input.target.Portfile, contents, func() error {
		bound, err := input.context(input.before.Platform, selectedOnly)
		if err != nil {
			return err
		}
		var after macports.Snapshot
		if selected, ok := reader.(macports.SelectedReader); ok && selectedOnly {
			after, err = selected.EvaluateSelected(ctx, bound)
		} else {
			after, err = reader.Evaluate(ctx, bound)
		}
		if err == nil {
			err = fidelity.CheckSnapshot(after, bound)
		}
		// Probe snapshots describe uncommitted contents, not the immutable base tree.
		after.Source = record.Source{}
		result.after = after
		return err
	})
	return result, err
}
