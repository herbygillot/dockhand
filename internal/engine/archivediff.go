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
	"github.com/herbygillot/dockhand/internal/macports/treequery"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/scratch"
)

// ArchiveFetcher fetches the source archives a port's Portfile declares in
// a source tree, into a directory, each checked against the Portfile's
// checksums.
type ArchiveFetcher interface {
	FetchArchives(ctx context.Context, source model.Source, directory, into string) ([]treequery.FetchedArchive, error)
}

// ArchiveDiff is how one of a port's archives changed from the branch's
// base to its files now.
type ArchiveDiff struct {
	Directory string
	// Old and New name the archives; one is empty when only one side
	// declares it. Match is how they correspond, by the port's source set
	// (macports.MatchSources): an uncertain one has a side alone, and
	// isn't compared.
	Old, New string
	Match    macports.SourceMatch
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

// Uncertain reports an archive that several of the other side's could
// correspond to, which wasn't compared.
func (d ArchiveDiff) Uncertain() bool { return d.Match.Status == macports.SourceUncertain }

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
	// Each archive is diffed with the one it corresponds to, as an
	// assessment pairs them, never merely the one in its place.
	named := func(fetched []treequery.FetchedArchive) ([]string, map[string]treequery.FetchedArchive) {
		var names []string
		byName := map[string]treequery.FetchedArchive{}
		for _, archive := range fetched {
			names, byName[archive.Name] = append(names, archive.Name), archive
		}
		return names, byName
	}
	beforeNames, old := named(before)
	afterNames, now := named(after)
	var diffs []ArchiveDiff
	for i, match := range macports.MatchSources(beforeNames, afterNames, nil) {
		diff := ArchiveDiff{Directory: directory, Old: match.Before, New: match.After, Match: match}
		previous, next := old[match.Before], now[match.After]
		diff.OldFromMirror = previous.Mirror
		if match.Status == macports.SourceMatched {
			diff.Same = previous.Sum.SHA256 != "" && previous.Sum.SHA256 == next.Sum.SHA256
			if !diff.Same {
				if err := e.compareArchives(ctx, filepath.Join(root, fmt.Sprint(i)), previous.Path, next.Path, &diff); err != nil {
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
	ports, err := e.capabilities()
	if err != nil {
		return nil, err
	}
	if ports.Fetcher == nil {
		return nil, errors.New("fetching archives needs MacPorts' evaluator")
	}
	return ports.Fetcher, nil
}

// ArchivePlanner says what a port fetches in a source, as MacPorts
// evaluates it on this Mac.
type ArchivePlanner interface {
	// ArchivePlan is a port of a directory, or its first where port is
	// empty, as evaluated, and the archives its fetch plan names, which
	// fetchPlanned fetches: its own, where it declares crates or Go
	// modules too; ErrNoArchives where it names none, as for a port
	// fetched with Git, or a metaport.
	ArchivePlan(ctx context.Context, source model.Source, directory, port string) (macports.PortInfo, []macports.Distfile, error)
}
