// Package treequery reads a ports tree's ports as MacPorts evaluates them:
// a directory's ports, the port index's dependents and dependency graph,
// and the archives a port's fetch plan names. It's the engine's default
// port reader, built outside the engine, which satisfies the engine's
// interfaces by its methods alone.
package treequery

import (
	"cmp"
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

// New is a reader of repo's sources, resolving ports with ports, in the
// workspaces given.
func New(repo *git.Repository, ports *selection.Reader, workspaces *workspace.Registry) *Reader {
	return &Reader{repo: repo, ports: ports, workspaces: workspaces}
}

// Reader evaluates Portfiles with MacPorts, in a projection of the
// source tree holding only what each evaluation needs: the default port
// reader the engine is given, built outside it (the architecture review
// of 5897a82, CX-5's package half).
type Reader struct {
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
func (p *Reader) nativePlatform(ctx context.Context) (model.Platform, error) {
	p.nativeOnce.Do(func() { p.native, p.nativeErr = p.ports.NativePlatform(ctx) })
	return p.native, p.nativeErr
}

func (p *Reader) Ports(ctx context.Context, source model.Source, directory string, environment model.Environment, variants map[string]bool) ([]macports.PortInfo, error) {
	snapshot, err := p.family(ctx, source, directory, environment, variants)
	if err != nil {
		return nil, err
	}
	main := snapshot.Target.Name
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
	slices.SortFunc(names, macports.ComparePortNames)
	for _, name := range names {
		ports = append(ports, snapshot.Ports[name])
	}
	return ports, nil
}

// Family is a directory's evaluation in this Mac's own context, with its
// defaults, every subport and the root it was evaluated in, which a
// change record compares.
func (p *Reader) Family(ctx context.Context, source model.Source, directory string) (macports.Snapshot, error) {
	return p.family(ctx, source, directory, model.Environment{}, nil)
}

// family evaluates a directory as Ports reads it, its main port the
// snapshot's target.
func (p *Reader) family(ctx context.Context, source model.Source, directory string, environment model.Environment, variants map[string]bool) (_ macports.Snapshot, err error) {
	files, done, err := p.workspaces.Acquire(ctx, p.repo, source)
	if err != nil {
		return macports.Snapshot{}, err
	}
	defer func() { err = errors.Join(err, done()) }()
	if err := files.EnsurePort(ctx, model.Target{Portfile: directory + "/Portfile"}); err != nil {
		return macports.Snapshot{}, err
	}
	// MacPorts runs as this Mac's release. Another release is modelled in
	// its session, as the oracle's contexts are, its developer tools from
	// the facts table (decision 7), and so is this Mac's own where the
	// environment states its tools, which needn't be this Mac's. The
	// directory's main port is found natively, and its subports come from
	// the modelled evaluation.
	native, err := p.nativePlatform(ctx)
	if err != nil {
		return macports.Snapshot{}, err
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
		return macports.Snapshot{}, err
	}
	targets, err := p.ports.Resolve(ctx, tree, macports.Selection{Selector: directory, Variants: variants})
	if err != nil {
		return macports.Snapshot{}, fmt.Errorf("evaluating %s: %w", directory, err)
	}
	if len(targets) == 0 {
		return macports.Snapshot{}, fmt.Errorf("evaluating %s: it defines no port", directory)
	}
	bound, err := files.Context(targets[0], session)
	if err != nil {
		return macports.Snapshot{}, err
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
		return macports.Snapshot{}, fmt.Errorf("evaluating %s: %w", directory, err)
	}
	return snapshot, nil
}

func (p *Reader) Directory(ctx context.Context, source model.Source, name string) (_ string, err error) {
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
	// A name the index doesn't hold is a port the tree hasn't, which a
	// new port's name check reads as free: create said "whether a subport
	// is named mods wasn't checked: no port named mods" (field testing).
	if errors.Is(err, selection.ErrUnknownPort) {
		return "", unknownPort{err}
	}
	if err != nil {
		return "", err
	}
	if len(targets) == 0 || !strings.Contains(targets[0].Portfile, "/") {
		return "", notInTree(name)
	}
	return path.Dir(targets[0].Portfile), nil
}

// NotInTree is a port a tree doesn't have, said as a person would, and
// ErrNoPort, as a reader standing in for this one says it.
func NotInTree(name string) error { return notInTree(name) }

// notInTree is a port a tree doesn't have, said as a person would, and
// ErrNoPort.
type notInTree string

func (n notInTree) Error() string      { return "no port " + string(n) + " in this tree" }
func (notInTree) Is(target error) bool { return target == ErrNoPort }

// unknownPort is a name the tree's port index doesn't hold, said as the
// index says it, and ErrNoPort.
type unknownPort struct{ error }

func (unknownPort) Is(target error) bool { return target == ErrNoPort }

// dependencyPhases words the index's reverse-dependency fields.
var dependencyPhases = map[string]string{portindex.DependsBuild: "build", portindex.DependsLib: "library", portindex.DependsRun: "runtime"}

// index reads the port index of a source, staged for it as the source's
// ports are resolved.
func (p *Reader) index(ctx context.Context, source model.Source, read func(*portindex.Index) error) (err error) {
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
func (p *Reader) Spellings(ctx context.Context, source model.Source, spelling string) (spellings []portindex.MaintainerSpelling, err error) {
	err = p.index(ctx, source, func(index *portindex.Index) error {
		spellings, err = index.Spellings(spelling)
		return err
	})
	return spellings, err
}

// Dependents reads the direct dependents of the directories' ports from
// the port index of the source.
func (p *Reader) Dependents(ctx context.Context, source model.Source, directories []string) (all []Dependent, err error) {
	err = p.index(ctx, source, func(index *portindex.Index) error {
		all, err = dependentsIn(index, directories)
		return err
	})
	return all, err
}

// PortsDefined names the ports each directory defines, as the source's
// index has them (portNamer).
func (p *Reader) PortsDefined(ctx context.Context, source model.Source, directories []string) (map[string][]string, error) {
	defined := map[string][]string{}
	err := p.index(ctx, source, func(index *portindex.Index) error {
		return index.Each(func(entry portindex.Entry) bool {
			if slices.Contains(directories, entry.Portdir) {
				defined[entry.Portdir] = append(defined[entry.Portdir], entry.Name)
			}
			return true
		})
	})
	return defined, err
}

// ArchiveVersions are every indexed port's version and revision, by
// lowercased name, as an archive's name writes them, 2.56.0_0, from the
// port index of the source: what a check's estimate looks for on
// packages.macports.org.
func (p *Reader) ArchiveVersions(ctx context.Context, source model.Source) (versions map[string]string, err error) {
	versions = map[string]string{}
	err = p.index(ctx, source, func(index *portindex.Index) error {
		return index.Each(func(entry portindex.Entry) bool {
			if version := entry.Fields["version"]; version != "" {
				versions[strings.ToLower(entry.Name)] = version + "_" + cmp.Or(entry.Fields["revision"], "0")
			}
			return true
		})
	})
	return versions, err
}

// DependencyGraph reads every port's direct dependencies from the port
// index of the source (portindex.Index.Dependencies).
func (p *Reader) DependencyGraph(ctx context.Context, source model.Source) (graph map[string][]string, err error) {
	err = p.index(ctx, source, func(index *portindex.Index) error {
		graph, err = index.Dependencies()
		return err
	})
	return graph, err
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
	slices.SortFunc(all, func(a, b Dependent) int { return macports.ComparePortNames(a.Name, b.Name) })
	return all, nil
}

// Dependent is a port that depends directly on a port a branch changes.
type Dependent struct {
	Name      string
	Directory string
	// On names the changed ports it depends on.
	On []string
	// Phases are how: "build", "library", or "runtime".
	Phases []string
	// Variants are those under which alone it depends on them, as
	// enchant2 links nuspell under +nuspell; none where its default
	// variants do.
	Variants []string
}

// Words say a dependent for a person: its name, how it depends, and the
// variants it depends under, where only they make it: "enchant2 (library,
// under +nuspell)".
func (d Dependent) Words() string {
	how := strings.Join(d.Phases, ", ")
	if len(d.Variants) > 0 {
		how += ", under +" + strings.Join(d.Variants, " or +")
	}
	return fmt.Sprintf("%s (%s)", d.Name, how)
}
