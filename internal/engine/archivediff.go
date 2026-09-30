package engine

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"slices"

	"github.com/herbygillot/dockhand/internal/archive"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/portedit/archives"
	"github.com/herbygillot/dockhand/internal/macports/portfile"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/scratch"
)

// ArchiveFetcher fetches the source archives a port's Portfile declares in
// a source tree, into a directory, each checked against the Portfile's
// checksums.
type ArchiveFetcher interface {
	FetchArchives(ctx context.Context, source model.Source, directory, into string) ([]FetchedArchive, error)
}

// FetchedArchive is one archive, as the Portfile declares it.
type FetchedArchive struct {
	Name string
	Path string
	// Mirror is true when upstream no longer served it as declared, and
	// MacPorts' distfiles mirror did.
	Mirror bool
	Sum    portfile.Checksum
}

// ArchiveDiff is how one of a port's archives changed from the branch's
// base to its files now.
type ArchiveDiff struct {
	Directory string
	// Old and New name the archives; one is empty when only one side
	// declares it.
	Old, New string
	// OldFromMirror is true when the base's archive came from MacPorts'
	// mirror, upstream serving something else under its name.
	OldFromMirror bool
	// Same is true when the archives are identical.
	Same                    bool
	Changed, Added, Removed int
	Patch                   []byte
	// Problem is why the port's archives could not be compared.
	Problem string
}

// ArchiveDiff compares the source archives of the ports a branch changes,
// or of the ports named, as the base declares them and as the branch's
// files declare them now: what changed inside, file by file.
func (e *Engine) ArchiveDiff(ctx context.Context, branch model.Branch, ports []string) ([]ArchiveDiff, error) {
	changed, err := e.Diff(ctx, branch, nil)
	if err != nil {
		return nil, err
	}
	fetcher, err := e.archiveFetcher()
	if err != nil {
		return nil, err
	}
	var diffs []ArchiveDiff
	for _, port := range changed.Ports {
		if len(ports) > 0 && !slices.ContainsFunc(ports, func(name string) bool { return name == port.Directory || name == path.Base(port.Directory) }) {
			continue
		}
		switch {
		case port.Added:
			diffs = append(diffs, ArchiveDiff{Directory: port.Directory, Problem: "a new port, with nothing at the base to compare with"})
			continue
		case port.Deleted:
			continue
		}
		found, err := e.archiveDiff(ctx, fetcher, branch, changed, port.Directory)
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			found = []ArchiveDiff{{Directory: port.Directory, Problem: err.Error()}}
		}
		diffs = append(diffs, found...)
	}
	return diffs, nil
}

func (e *Engine) archiveDiff(ctx context.Context, fetcher ArchiveFetcher, branch model.Branch, changed BranchDiff, directory string) (_ []ArchiveDiff, err error) {
	root, err := scratch.Dir("archive-diff-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(root)
	for _, side := range []string{"old", "new"} {
		if err := os.Mkdir(filepath.Join(root, side), 0o755); err != nil {
			return nil, err
		}
	}
	before, err := fetcher.FetchArchives(ctx, model.Source{Commit: branch.Base, Tree: model.ObjectID(changed.BaseTree), Base: branch.Base}, directory, filepath.Join(root, "old"))
	if err != nil {
		return nil, fmt.Errorf("the base's archives: %w", err)
	}
	after, err := fetcher.FetchArchives(ctx, model.Source{Tree: model.ObjectID(changed.Status.Tree), Base: branch.Base}, directory, filepath.Join(root, "new"))
	if err != nil {
		return nil, fmt.Errorf("the branch's archives: %w", err)
	}
	var diffs []ArchiveDiff
	for i := 0; i < max(len(before), len(after)); i++ {
		diff := ArchiveDiff{Directory: directory}
		switch {
		case i >= len(before):
			diff.New = after[i].Name
		case i >= len(after):
			diff.Old, diff.OldFromMirror = before[i].Name, before[i].Mirror
		default:
			diff.Old, diff.OldFromMirror, diff.New = before[i].Name, before[i].Mirror, after[i].Name
			diff.Same = before[i].Sum.SHA256 != "" && before[i].Sum.SHA256 == after[i].Sum.SHA256
			if !diff.Same {
				if err := e.compareArchives(ctx, filepath.Join(root, fmt.Sprint(i)), before[i].Path, after[i].Path, &diff); err != nil {
					return nil, err
				}
			}
		}
		diffs = append(diffs, diff)
	}
	return diffs, nil
}

// compareArchives extracts both archives, less their versioned top
// directories, and diffs what they hold.
func (e *Engine) compareArchives(ctx context.Context, root, older, newer string, diff *ArchiveDiff) error {
	for side, file := range map[string]string{"a": older, "b": newer} {
		if err := os.MkdirAll(filepath.Join(root, side), 0o755); err != nil {
			return err
		}
		if _, err := archive.Extract(ctx, file, filepath.Join(root, side)); err != nil {
			return fmt.Errorf("extracting %s: %w", path.Base(file), err)
		}
	}
	patch, err := e.Repo.DiffDirectories(ctx, root)
	if err != nil {
		return err
	}
	diff.Patch = patch
	for _, header := range bytes.Split(patch, []byte("\ndiff --git ")) {
		switch {
		case len(bytes.TrimSpace(header)) == 0:
		case bytes.Contains(header, []byte("\nnew file mode")):
			diff.Added++
		case bytes.Contains(header, []byte("\ndeleted file mode")):
			diff.Removed++
		default:
			diff.Changed++
		}
	}
	return nil
}

func (e *Engine) archiveFetcher() (ArchiveFetcher, error) {
	if e.ArchiveFetcher != nil {
		return e.ArchiveFetcher, nil
	}
	reader, err := e.portReader()
	if err != nil {
		return nil, err
	}
	fetcher, ok := reader.(ArchiveFetcher)
	if !ok {
		return nil, errors.New("fetching archives needs MacPorts' evaluator")
	}
	return fetcher, nil
}

// FetchArchives evaluates the port in the source and fetches each archive
// it declares from upstream, or, when upstream no longer serves it as the
// Portfile's checksums say, from MacPorts' distfiles mirror, where a
// stealth-updated archive's old contents survive.
func (p *evaluatedPorts) FetchArchives(ctx context.Context, source model.Source, directory, into string) ([]FetchedArchive, error) {
	info, plan, err := p.ArchivePlan(ctx, source, directory, "")
	if err != nil {
		return nil, err
	}
	return fetchPlanned(ctx, archives.Client{Mirror: archives.MacPortsMirror}.Store(into), info, plan)
}

// ArchivePlanner says what a port fetches in a source, as MacPorts
// evaluates it on this Mac.
type ArchivePlanner interface {
	// ArchivePlan is a port of a directory, or its first where port is
	// empty, as evaluated, and the archives its fetch plan names, which
	// fetchPlanned fetches; ErrNoArchives where it names none, as for a
	// port fetched with Git, or a metaport.
	ArchivePlan(ctx context.Context, source model.Source, directory, port string) (macports.PortInfo, []macports.Distfile, error)
}

// ErrNoArchives is a port whose fetch plan names no archives, and ErrNoPort
// a directory that defines no port by the name asked for.
var (
	ErrNoArchives = errors.New("the port's fetch plan names no archives")
	ErrNoPort     = errors.New("no such port")
)

func (p *evaluatedPorts) ArchivePlan(ctx context.Context, source model.Source, directory, port string) (_ macports.PortInfo, _ []macports.Distfile, err error) {
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
		return macports.PortInfo{}, nil, fmt.Errorf("%w: %s defines no port %s", ErrNoPort, directory, port)
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
	plan, err := planOf(info, observation.Ports[name], filepath.Join(tree.Root(), filepath.FromSlash(directory)))
	return info, plan, err
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
	if err := archives.CheckPolicy(info, portdir); err != nil {
		return nil, err
	}
	return plan, nil
}

// fetchPlanned fetches each archive of a fetch plan as the port's
// checksums declare it, from upstream, where MacPorts' own fetch plan
// finds it, or else MacPorts' mirror (Store.Shipped).
func fetchPlanned(ctx context.Context, store *archives.Store, info macports.PortInfo, plan []macports.Distfile) ([]FetchedArchive, error) {
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
