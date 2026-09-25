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
	"github.com/herbygillot/dockhand/internal/macports/portindex"
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

// dependencyPhases words the index's reverse-dependency fields.
var dependencyPhases = map[string]string{portindex.DependsBuild: "build", portindex.DependsLib: "library", portindex.DependsRun: "runtime"}

// Dependents reads the direct dependents of the directories' ports from
// the port index of the source.
func (p *evaluatedPorts) Dependents(ctx context.Context, source model.Source, directories []string) (_ []Dependent, err error) {
	files, done, err := p.workspaces.Acquire(ctx, p.repo, source)
	if err != nil {
		return nil, err
	}
	defer func() { err = errors.Join(err, done()) }()
	tree, err := files.Tree(model.Platform{})
	if err != nil {
		return nil, err
	}
	index, err := p.ports.Index.Index(ctx, tree)
	if err != nil {
		return nil, fmt.Errorf("reading the port index: %w", err)
	}
	var changed []string
	if err := index.Each(func(entry portindex.Entry) bool {
		if slices.Contains(directories, entry.Portdir) {
			changed = append(changed, entry.Name)
		}
		return true
	}); err != nil {
		return nil, err
	}
	reverse, err := index.ReverseDependencies()
	if err != nil {
		return nil, err
	}
	byName := map[string]*Dependent{}
	var dependents []*Dependent
	for _, name := range changed {
		for _, edge := range reverse.ByPort[strings.ToLower(name)] {
			if slices.Contains(directories, edge.Portdir) {
				continue
			}
			dependent := byName[edge.Name]
			if dependent == nil {
				dependent = &Dependent{Name: edge.Name, Directory: edge.Portdir}
				byName[edge.Name] = dependent
				dependents = append(dependents, dependent)
			}
			if !slices.Contains(dependent.On, name) {
				dependent.On = append(dependent.On, name)
			}
			for _, field := range edge.Fields {
				if phase := dependencyPhases[field]; phase != "" && !slices.Contains(dependent.Phases, phase) {
					dependent.Phases = append(dependent.Phases, phase)
				}
			}
		}
	}
	all := make([]Dependent, len(dependents))
	for i, dependent := range dependents {
		all[i] = *dependent
	}
	slices.SortFunc(all, func(a, b Dependent) int { return strings.Compare(a.Name, b.Name) })
	return all, nil
}
