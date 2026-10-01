package engine

import (
	"context"
	"errors"
	"fmt"
	"path"
	"slices"
	"strings"
	"sync"

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
	return assemble(e, &e.PortReader, func() (PortReader, error) {
		ports, err := e.selectionReader()
		if err != nil {
			return nil, err
		}
		return &evaluatedPorts{repo: e.Repo, ports: ports, workspaces: &workspace.Registry{}}, nil
	})
}

// evaluatedPorts evaluates Portfiles with MacPorts, in a projection of the
// source tree holding only what each evaluation needs.
type evaluatedPorts struct {
	repo       *git.Repository
	ports      *selection.Reader
	workspaces *workspace.Registry
	// native is the release MacPorts runs as here, read once.
	native     model.Platform
	nativeOnce sync.Once
	nativeErr  error
}

// nativePlatform is the release MacPorts describes on this host: the Mac's
// own, or on another host the one its sessions model.
func (p *evaluatedPorts) nativePlatform(ctx context.Context) (model.Platform, error) {
	p.nativeOnce.Do(func() { p.native, p.nativeErr = p.ports.NativePlatform(ctx) })
	return p.native, p.nativeErr
}

func (p *evaluatedPorts) Ports(ctx context.Context, source model.Source, directory string, environment model.Environment, variants map[string]bool) (_ []macports.PortInfo, err error) {
	files, done, err := p.workspaces.Acquire(ctx, p.repo, source)
	if err != nil {
		return nil, err
	}
	defer func() { err = errors.Join(err, done()) }()
	if err := files.EnsurePort(ctx, model.Target{Portfile: directory + "/Portfile"}); err != nil {
		return nil, err
	}
	// MacPorts runs as this Mac's release. Another release is modelled in
	// its session, as the oracle's contexts are, its developer tools from
	// the facts table (decision 7), and so is this Mac's own where the
	// environment states its tools, which needn't be this Mac's. The
	// directory's main port is found natively, and its subports come from
	// the modelled evaluation.
	native, err := p.nativePlatform(ctx)
	if err != nil {
		return nil, err
	}
	platform := environment.Platform
	session := platform
	modelled := platform != (model.Platform{}) && platform != native || environment.DeveloperTools != ""
	if modelled {
		session = model.Platform{}
		if platform == (model.Platform{}) {
			platform = native
		}
	}
	tree, err := files.Tree(session)
	if err != nil {
		return nil, err
	}
	targets, err := p.ports.Resolve(ctx, tree, macports.Selection{Selector: directory, Variants: variants})
	if err != nil {
		return nil, fmt.Errorf("evaluating %s: %w", directory, err)
	}
	if len(targets) == 0 {
		return nil, fmt.Errorf("evaluating %s: it defines no port", directory)
	}
	bound, err := files.Context(targets[0], session)
	if err != nil {
		return nil, err
	}
	var snapshot macports.Snapshot
	if modelled {
		var observation macports.Observation
		observation, err = p.ports.Observe(ctx, bound, macports.ObservationRequest{Platform: platform, DeveloperTools: environment.DeveloperTools})
		snapshot = observation.Snapshot
	} else {
		snapshot, err = p.ports.Evaluate(ctx, bound)
	}
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
		return "", notInTree(name)
	}
	return path.Dir(targets[0].Portfile), nil
}

// notInTree is a port a tree doesn't have, said as a person would, and
// ErrNoPort.
type notInTree string

func (n notInTree) Error() string      { return "no port " + string(n) + " in this tree" }
func (notInTree) Is(target error) bool { return target == ErrNoPort }

// dependencyPhases words the index's reverse-dependency fields.
var dependencyPhases = map[string]string{portindex.DependsBuild: "build", portindex.DependsLib: "library", portindex.DependsRun: "runtime"}

// index reads the port index of a source, staged for it as the source's
// ports are resolved.
func (p *evaluatedPorts) index(ctx context.Context, source model.Source, read func(*portindex.Index) error) (err error) {
	files, done, err := p.workspaces.Acquire(ctx, p.repo, source)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, done()) }()
	tree, err := files.Tree(model.Platform{})
	if err != nil {
		return err
	}
	index, err := p.ports.Index.Index(ctx, tree)
	if err != nil {
		return fmt.Errorf("reading the port index: %w", err)
	}
	return read(index)
}

// Spellings reads the ways the port index of the source writes a
// maintainer (portindex.Index.Spellings).
func (p *evaluatedPorts) Spellings(ctx context.Context, source model.Source, spelling string) (spellings []portindex.MaintainerSpelling, err error) {
	err = p.index(ctx, source, func(index *portindex.Index) error {
		spellings, err = index.Spellings(spelling)
		return err
	})
	return spellings, err
}

// Dependents reads the direct dependents of the directories' ports from
// the port index of the source.
func (p *evaluatedPorts) Dependents(ctx context.Context, source model.Source, directories []string) (all []Dependent, err error) {
	err = p.index(ctx, source, func(index *portindex.Index) error {
		all, err = dependentsIn(index, directories)
		return err
	})
	return all, err
}

// dependentsIn are the direct dependents of the directories' ports in an
// index.
func dependentsIn(index *portindex.Index, directories []string) ([]Dependent, error) {
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
