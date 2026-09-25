package engine

import (
	"context"
	"errors"
	"fmt"
	"path"
	"slices"
	"strings"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/selection"
	"github.com/herbygillot/dockhand/internal/macports/workspace"
	"github.com/herbygillot/dockhand/internal/model"
)

// portReader is the engine's PortReader: the one it was given, or
// MacPorts' own evaluator.
func (e *Engine) portReader() (PortReader, error) {
	if e.PortReader != nil {
		return e.PortReader, nil
	}
	ports, err := e.selectionReader()
	if err != nil {
		return nil, err
	}
	e.PortReader = &evaluatedPorts{repo: e.Repo, ports: ports, workspaces: &workspace.Registry{}}
	return e.PortReader, nil
}

// evaluatedPorts evaluates Portfiles with MacPorts, in a projection of the
// source tree holding only what each evaluation needs.
type evaluatedPorts struct {
	repo       *git.Repository
	ports      *selection.Reader
	workspaces *workspace.Registry
}

func (p *evaluatedPorts) Ports(ctx context.Context, source model.Source, directory string, platform model.Platform) (_ []macports.PortInfo, err error) {
	files, done, err := p.workspaces.Acquire(ctx, p.repo, source)
	if err != nil {
		return nil, err
	}
	defer func() { err = errors.Join(err, done()) }()
	if err := files.EnsurePort(ctx, model.Target{Portfile: directory + "/Portfile"}); err != nil {
		return nil, err
	}
	tree, err := files.Tree(platform)
	if err != nil {
		return nil, err
	}
	targets, err := p.ports.Resolve(ctx, tree, macports.Selection{Selector: directory})
	if err != nil {
		return nil, fmt.Errorf("evaluating %s: %w", directory, err)
	}
	if len(targets) == 0 {
		return nil, fmt.Errorf("evaluating %s: it defines no port", directory)
	}
	bound, err := files.Context(targets[0], platform)
	if err != nil {
		return nil, err
	}
	snapshot, err := p.ports.Evaluate(ctx, bound)
	if err != nil {
		return nil, fmt.Errorf("evaluating %s: %w", directory, err)
	}
	main := targets[0].Name
	var ports []macports.PortInfo
	if info, ok := snapshot.Ports[main]; ok {
		ports = append(ports, info)
	}
	var names []string
	for name := range snapshot.Ports {
		if name != main {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	for _, name := range names {
		ports = append(ports, snapshot.Ports[name])
	}
	return ports, nil
}

func (p *evaluatedPorts) Directory(ctx context.Context, source model.Source, name string) (_ string, err error) {
	files, done, err := p.workspaces.Acquire(ctx, p.repo, source)
	if err != nil {
		return "", err
	}
	defer func() { err = errors.Join(err, done()) }()
	tree, err := files.Tree(model.Platform{})
	if err != nil {
		return "", err
	}
	targets, err := p.ports.Resolve(ctx, tree, macports.Selection{Selector: name})
	if err != nil {
		return "", err
	}
	if len(targets) == 0 || !strings.Contains(targets[0].Portfile, "/") {
		return "", fmt.Errorf("no port %s in this tree", name)
	}
	return path.Dir(targets[0].Portfile), nil
}
