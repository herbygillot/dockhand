package treequery

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/depblock"
	"github.com/herbygillot/dockhand/internal/macports/distfetch"
	"github.com/herbygillot/dockhand/internal/macports/portfile"
	"github.com/herbygillot/dockhand/internal/macports/workspace"
	"github.com/herbygillot/dockhand/internal/model"
)

// FetchedArchive is one archive, as the Portfile declares it.
type FetchedArchive struct {
	Name string
	Path string
	// Mirror is true when upstream no longer served it as declared, and
	// MacPorts' distfiles mirror did.
	Mirror bool
	Sum    portfile.Checksum
}

// FetchArchives evaluates the port in the source and fetches each archive
// it declares from upstream, or, when upstream no longer serves it as the
// Portfile's checksums say, from MacPorts' distfiles mirror, where a
// stealth-updated archive's old contents survive.
func (p *Reader) FetchArchives(ctx context.Context, source model.Source, directory, into string) ([]FetchedArchive, error) {
	info, plan, err := p.ArchivePlan(ctx, source, directory, "")
	if err != nil {
		return nil, err
	}
	return FetchPlanned(ctx, distfetch.Client{Mirror: distfetch.MacPortsMirror}.Store(into), info, plan)
}

// ErrNoArchives is a port whose fetch plan names no archives, and ErrNoPort
// a directory that defines no port by the name asked for.
var (
	ErrNoArchives = errors.New("the port's fetch plan names no archives")
	ErrNoPort     = errors.New("no such port")
)

func (p *Reader) ArchivePlan(ctx context.Context, source model.Source, directory, port string) (_ macports.PortInfo, _ []macports.Distfile, err error) {
	files, done, err := p.workspaces.Acquire(ctx, p.repo, source)
	if err != nil {
		return macports.PortInfo{}, nil, err
	}
	defer func() { err = errors.Join(err, done()) }()
	if err := files.EnsurePort(ctx, model.Target{Portfile: directory + "/Portfile"}); err != nil {
		return macports.PortInfo{}, nil, err
	}
	tree, err := files.Tree(model.Platform{})
	if err != nil {
		return macports.PortInfo{}, nil, err
	}
	targets, err := p.ports.Resolve(ctx, tree, macports.Selection{Selector: directory})
	if err != nil {
		return macports.PortInfo{}, nil, err
	}
	i := slices.IndexFunc(targets, func(target model.Target) bool { return port == "" || target.Name == port })
	if i < 0 {
		// A directory resolves to its main port; a subport is asked for by
		// its name, as libuv's libuv-devel is (the batch 11 run on #34620).
		targets, err = p.ports.Resolve(ctx, tree, macports.Selection{Selector: directory, Subport: port})
		switch {
		case errors.Is(err, macports.ErrTarget):
			return macports.PortInfo{}, nil, fmt.Errorf("%w: %s defines no port %s", ErrNoPort, directory, port)
		case err != nil:
			return macports.PortInfo{}, nil, err
		}
		if i = slices.IndexFunc(targets, func(target model.Target) bool { return target.Name == port }); i < 0 {
			return macports.PortInfo{}, nil, fmt.Errorf("%w: %s defines no port %s", ErrNoPort, directory, port)
		}
	}
	bound, err := files.Context(targets[i], model.Platform{})
	if err != nil {
		return macports.PortInfo{}, nil, err
	}
	observation, err := p.ports.Observe(ctx, bound, macports.ObservationRequest{SelectedOnly: true})
	if err != nil {
		return macports.PortInfo{}, nil, err
	}
	name := targets[i].Name
	info := observation.Snapshot.Ports[name]
	own, err := p.withoutVendored(ctx, files, targets[i], info)
	if err != nil {
		return info, nil, err
	}
	if own != nil {
		observation = *own
	}
	plan, err := planOf(observation.Snapshot.Ports[name], observation.Ports[name], filepath.Join(tree.Root(), filepath.FromSlash(directory)))
	return info, plan, err
}

// withoutVendored is a port observed with the crates or Go modules its
// Portfile declares set aside (ownArchives), so that its fetch
// plan names its own archives, which a comparison reads the declarations
// in. Nil for a port that declares none.
func (p *Reader) withoutVendored(ctx context.Context, files *workspace.Workspace, target model.Target, info macports.PortInfo) (_ *macports.Observation, err error) {
	contents, err := os.ReadFile(filepath.Join(files.Root(), filepath.FromSlash(target.Portfile)))
	if err != nil {
		return nil, err
	}
	stripped, vendored, err := ownArchives(contents, info)
	if err != nil || !vendored {
		return nil, err
	}
	overlay, err := files.Overlay(ctx, []git.FileEdit{{Path: target.Portfile, After: stripped}})
	if err != nil {
		return nil, err
	}
	defer func() { err = errors.Join(err, overlay.Close()) }()
	bound, err := overlay.Context(target, model.Platform{})
	if err != nil {
		return nil, err
	}
	observation, err := p.ports.Observe(ctx, bound, macports.ObservationRequest{SelectedOnly: true})
	if err != nil {
		return nil, err
	}
	// The overlay is gone once this returns, and with it the files/ its
	// evaluation's filespath names, which the fetch policy reads the
	// port's patches in: they're the tree's, as the port as it is says.
	// rust's nine patches read "local patch directory is unavailable", and
	// its archives weren't compared (the rust and cargo run).
	if own, ok := observation.Snapshot.Ports[target.Name]; ok {
		own.Options = maps.Clone(own.Options)
		own.Options["filespath"] = info.Options["filespath"]
		observation.Snapshot.Ports = maps.Clone(observation.Snapshot.Ports)
		observation.Snapshot.Ports[target.Name] = own
	}
	return &observation, nil
}

// planOf is the archives a port's fetch plan names, which dockhand fetches
// as MacPorts would: ErrNoArchives where it names none, MacPorts' reason
// where it couldn't make one, and the refusal of a fetch the policy leaves
// to a dedicated preparer.
func planOf(info macports.PortInfo, observed macports.PortObservation, portdir string) ([]macports.Distfile, error) {
	plan, err := observed.FetchPlan()
	switch {
	case err != nil && len(observed.Problems) == 0:
		return nil, ErrNoArchives
	case err != nil:
		return nil, err
	}
	if err := distfetch.CheckPolicy(info, portdir); err != nil {
		return nil, err
	}
	return plan, nil
}

// FetchPlanned fetches each archive of a fetch plan as the port's
// checksums declare it, from upstream, where MacPorts' own fetch plan
// finds it, or else MacPorts' mirror (Store.Shipped).
func FetchPlanned(ctx context.Context, store *distfetch.Store, info macports.PortInfo, plan []macports.Distfile) ([]FetchedArchive, error) {
	shipped, err := store.Shipped(ctx, info, plan)
	if err != nil {
		return nil, err
	}
	var fetched []FetchedArchive
	for _, archive := range shipped {
		fetched = append(fetched, FetchedArchive{Name: archive.Name, Path: archive.Path, Mirror: archive.Mirror, Sum: archive.Checksum})
	}
	return fetched, nil
}

// ownArchives is a port's Portfile with the crates or Go modules it
// declares set aside, as a checksum refresh sets them aside: evaluated, its
// fetch plan names the port's own archives, which CheckPolicy may admit
// where it refuses the port as it is. The declarations are its lock
// file's, read in those archives, and MacPorts' fetch of them isn't one
// the direct downloader makes. False, with the Portfile as it is, for a
// port that declares none.
func ownArchives(contents []byte, info macports.PortInfo) ([]byte, bool, error) {
	declared, err := depblock.Declared(contents, info)
	switch {
	case err != nil:
		return nil, false, fmt.Errorf("%w: %s: %w", portfile.ErrUnsupported, info.Name, err)
	case declared == nil:
		return contents, false, nil
	}
	stripped, err := declared.Strip(contents)
	return stripped, err == nil, err
}
