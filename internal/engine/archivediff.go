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
	"strings"

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

// mirrorURL is MacPorts' distfiles mirror, which keeps each archive under
// its port's dist_subdir; tests stand in for it.
var mirrorURL = "https://distfiles.macports.org/"

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
func (p *evaluatedPorts) FetchArchives(ctx context.Context, source model.Source, directory, into string) (_ []FetchedArchive, err error) {
	files, done, err := p.workspaces.Acquire(ctx, p.repo, source)
	if err != nil {
		return nil, err
	}
	defer func() { err = errors.Join(err, done()) }()
	if err := files.EnsurePort(ctx, model.Target{Portfile: directory + "/Portfile"}); err != nil {
		return nil, err
	}
	tree, err := files.Tree(model.Platform{})
	if err != nil {
		return nil, err
	}
	targets, err := p.ports.Resolve(ctx, tree, macports.Selection{Selector: directory})
	if err != nil {
		return nil, err
	}
	if len(targets) == 0 {
		return nil, fmt.Errorf("%s defines no port", directory)
	}
	bound, err := files.Context(targets[0], model.Platform{})
	if err != nil {
		return nil, err
	}
	snapshot, err := p.ports.Evaluate(ctx, bound)
	if err != nil {
		return nil, err
	}
	info := snapshot.Ports[targets[0].Name]
	return fetchDeclared(ctx, archives.Client{}.Store(into), info, filepath.Join(tree.Root(), filepath.FromSlash(directory)))
}

// fetchDeclared fetches each archive the port declares, checked against
// its checksums, from upstream or else MacPorts' mirror.
func fetchDeclared(ctx context.Context, store *archives.Store, info macports.PortInfo, portdir string) ([]FetchedArchive, error) {
	sources, err := archives.Sources(info, portdir)
	if err != nil {
		return nil, err
	}
	declared := declaredChecksums(info.Options["checksums"])
	subdir := info.Options["dist_subdir"]
	if subdir == "" {
		subdir = info.Name
	}
	var fetched []FetchedArchive
	for _, source := range sources {
		want, ok := declared[source.Name]
		if !ok && len(declared) == 1 && len(sources) == 1 {
			want, ok = declared[""]
		}
		if !ok {
			return nil, fmt.Errorf("%s has no checksums in the Portfile", source.Name)
		}
		download, err := store.Fetch(ctx, info, source)
		if err == nil && !changedContents(want, download.Checksum) {
			fetched = append(fetched, FetchedArchive{Name: source.Name, Path: download.Path, Sum: download.Checksum})
			continue
		}
		if download.Path != "" {
			_ = os.Remove(download.Path)
		}
		mirrored, mirrorErr := store.Fetch(ctx, info, archives.Source{Name: source.Name, URL: mirrorURL + strings.Trim(subdir, "/") + "/" + source.Name})
		if mirrorErr != nil || changedContents(want, mirrored.Checksum) {
			return nil, fmt.Errorf("neither upstream nor MacPorts' mirror has %s as the Portfile's checksums describe it", source.Name)
		}
		fetched = append(fetched, FetchedArchive{Name: source.Name, Path: mirrored.Path, Mirror: true, Sum: mirrored.Checksum})
	}
	return fetched, nil
}
