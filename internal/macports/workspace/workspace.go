package workspace

import (
	"context"
	"errors"
	"fmt"
	"github.com/herbygillot/dockhand/internal/scratch"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/record"
)

// The shared resources every evaluation reads: PortGroups, livecheck and
// fetch definitions, variant descriptions, and the Xcode table.
const resources = "_resources"

// Scope is what a workspace holds: port directories as category/port, and
// whether the whole tree is present. _resources is present after any Ensure.
type Scope struct {
	Ports []string
	All   bool
}

// Holds reports whether the scope covers a port directory.
func (s Scope) Holds(directory string) bool {
	return s.All || slices.Contains(s.Ports, directory)
}

// Workspace is one projection of a tree. A base owns its directory, its
// session, and its overlays; an overlay shares the base's tracked files and
// replaces the edited ones.
type Workspace struct {
	repo      *git.Repository
	source    record.Source
	directory string
	base      *Workspace
	edits     []git.FileEdit

	mu        sync.Mutex
	entries   map[string]git.TreeEntry
	ports     []string
	all       bool
	resources bool
	// sharedResources marks an overlay whose _resources is a symlink to
	// the base's directory rather than entries of its own.
	sharedResources bool
	batch           macports.Batch
	closed          bool
	// adopted marks a workspace over a directory someone else owns and
	// filled: the whole tree is present, its entries come from a walk, and
	// Close leaves the directory.
	adopted bool
}

var (
	openMu sync.Mutex
	open   = map[string]*Workspace{}
)

// Open claims a directory for the tree, creates it, and materializes
// nothing into it.
func Open(ctx context.Context, repo *git.Repository, source record.Source) (*Workspace, error) {
	if repo == nil || !git.ValidObjectID(string(source.Tree)) {
		return nil, fmt.Errorf("workspace: a repository and a source tree are required")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	directory, err := scratch.Dir("workspace-")
	if err != nil {
		return nil, err
	}
	directory, err = filepath.EvalSymlinks(directory)
	if err != nil {
		return nil, errors.Join(err, os.RemoveAll(directory))
	}
	w := &Workspace{repo: repo, source: source, directory: directory, entries: map[string]git.TreeEntry{}}
	register(w)
	return w, nil
}

// Adopt describes a directory that already holds the whole tree, a plain
// materialization or a test fixture, so overlays can be made over it. The
// directory stays the caller's: Close leaves it. Entries come from a walk
// of the directory, so an overlay's Commit has no blob to check against
// and is refused; Rescan picks up files added after adoption.
func Adopt(root string, source record.Source) (*Workspace, error) {
	root, err := filepath.EvalSymlinks(root)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(root)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("workspace: %s is not a directory", root)
	}
	w := &Workspace{source: source, directory: root, adopted: true, all: true, resources: true}
	if err := w.scan(); err != nil {
		return nil, err
	}
	register(w)
	return w, nil
}

// Rescan re-reads an adopted directory's entries after files were added
// beneath it. A materialized workspace knows its entries and needs none.
func (w *Workspace) Rescan() error {
	if !w.adopted {
		return nil
	}
	return w.scan()
}

// scan lists the regular files and symlinks under an adopted directory as
// tree entries without objects.
func (w *Workspace) scan() error {
	entries := map[string]git.TreeEntry{}
	err := filepath.WalkDir(w.directory, func(name string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if entry.Name() == ".git" && name != w.directory {
				return filepath.SkipDir
			}
			return nil
		}
		relative, err := filepath.Rel(w.directory, name)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		mode := uint32(0100644)
		switch info, err := entry.Info(); {
		case err != nil:
			return err
		case info.Mode()&os.ModeSymlink != 0:
			mode = 0120000
		case info.Mode()&0100 != 0:
			mode = 0100755
		case !info.Mode().IsRegular():
			return nil
		}
		entries[relative] = git.TreeEntry{Name: relative, Mode: mode}
		return nil
	})
	if err != nil {
		return err
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	w.entries = entries
	return nil
}

// EnsurePortAt materializes a port's directory into the open workspace
// whose root this is, for a consumer that resolved the port by name and is
// about to read it. A root that is no workspace's needs nothing.
func EnsurePortAt(ctx context.Context, root string, target record.Target) error {
	openMu.Lock()
	w := open[root]
	openMu.Unlock()
	if w == nil {
		return nil
	}
	return w.EnsurePort(ctx, target)
}

// WidenAt materializes the whole tree into the open workspace whose root
// this is, for a consumer that walks the root and was handed a sparse one.
// A root that is no workspace's needs nothing.
func WidenAt(ctx context.Context, root string) error {
	openMu.Lock()
	w := open[root]
	openMu.Unlock()
	if w == nil {
		return nil
	}
	return w.EnsureAll(ctx)
}

func register(w *Workspace) {
	openMu.Lock()
	defer openMu.Unlock()
	open[w.directory] = w
}

func unregister(w *Workspace) {
	openMu.Lock()
	defer openMu.Unlock()
	delete(open, w.directory)
}

// ScopeOf reports the scope of an open workspace by its root, so a consumer
// handed a root string can refuse work that needs the whole tree. A root
// that is no workspace's, a plain materialization for instance, reports
// false and is taken as whole.
func ScopeOf(root string) (Scope, bool) {
	openMu.Lock()
	w := open[root]
	openMu.Unlock()
	if w == nil {
		return Scope{}, false
	}
	return w.Scope(), true
}

func (w *Workspace) Source() record.Source { return w.source }
func (w *Workspace) Root() string          { return w.directory }

// Base is the workspace an overlay was made from; a base returns itself.
func (w *Workspace) Base() *Workspace {
	if w.base != nil {
		return w.base
	}
	return w
}

// Scope reports what is present; an overlay's scope is its base's.
func (w *Workspace) Scope() Scope {
	w.mu.Lock()
	defer w.mu.Unlock()
	return Scope{Ports: slices.Clone(w.ports), All: w.all}
}

// EnsurePort materializes _resources and the target's port directory,
// including its files/ tree. It is idempotent and cheap when present.
func (w *Workspace) EnsurePort(ctx context.Context, target record.Target) error {
	directory := path.Dir(target.Portfile)
	if strings.Count(directory, "/") != 1 || !fsValid(directory) {
		return fmt.Errorf("workspace: %q is not a category/port/Portfile target", target.Portfile)
	}
	return w.ensure(ctx, []string{directory})
}

// EnsureAll materializes the whole tree; later Ensure calls are no-ops.
func (w *Workspace) EnsureAll(ctx context.Context) error { return w.ensure(ctx, nil) }

// ensure materializes the named port directories, or everything with none,
// skipping what is present. An overlay ensures its base and links what the
// base gained.
func (w *Workspace) ensure(ctx context.Context, directories []string) error {
	if w.base != nil {
		if err := w.base.ensure(ctx, directories); err != nil {
			return err
		}
		w.mu.Lock()
		if directories == nil {
			w.all = true
		}
		for _, directory := range directories {
			if !slices.Contains(w.ports, directory) {
				w.ports = append(w.ports, directory)
			}
		}
		slices.Sort(w.ports)
		w.mu.Unlock()
		return w.linkNew()
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return fmt.Errorf("workspace: closed")
	}
	if w.all {
		return nil
	}
	var include []string
	if directories != nil {
		for _, directory := range directories {
			if !slices.Contains(w.ports, directory) {
				include = append(include, directory)
			}
		}
		if !w.resources {
			include = append(include, resources)
		}
		if len(include) == 0 {
			return nil
		}
	}
	present := make(map[string]bool, len(w.entries))
	for name := range w.entries {
		present[name] = true
	}
	entries, err := w.repo.MaterializeInto(ctx, string(w.source.Tree), w.directory, include, present)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		w.entries[entry.Name] = entry
	}
	if directories == nil {
		w.all, w.resources = true, true
		return nil
	}
	w.resources = true
	for _, directory := range directories {
		if !slices.Contains(w.ports, directory) {
			w.ports = append(w.ports, directory)
		}
	}
	slices.Sort(w.ports)
	return nil
}

// Tree is the evaluator's view of the root; an overlay's names its base.
func (w *Workspace) Tree(platform record.Platform) (macports.Tree, error) {
	return macports.NewTreeOver(w.source, w.directory, w.Base().directory, platform)
}

// Context selects a target in the tree.
func (w *Workspace) Context(target record.Target, platform record.Platform) (macports.Context, error) {
	tree, err := w.Tree(platform)
	if err != nil {
		return macports.Context{}, err
	}
	return tree.Select(target)
}

// Batch is the interpreter session bound to the base root, opened on first
// use and shared by the base and its overlays until Close.
func (w *Workspace) Batch(ctx context.Context, ports macports.BatchReader) (macports.Batch, error) {
	base := w.Base()
	base.mu.Lock()
	defer base.mu.Unlock()
	if base.closed {
		return nil, fmt.Errorf("workspace: closed")
	}
	if base.batch != nil {
		return base.batch, nil
	}
	tree, err := base.Tree(record.Platform{})
	if err != nil {
		return nil, err
	}
	batch, err := ports.OpenBatch(ctx, tree)
	if err != nil {
		return nil, err
	}
	base.batch = batch
	return batch, nil
}

// Overlay is a sibling projection with the edits applied. Its scope is the
// edits' port directories and _resources, what evaluating those Portfiles
// reads, however wide the base is: a candidate evaluated over a whole-tree
// base must not link the whole tree. Tracked files in scope that the edits
// do not touch are hardlinked from the base, symlinks are re-created, and
// edited files are written with their entries' modes. Ensure widens the
// overlay as it widens a base. Untracked files in the base directory are not
// part of it. An edit must name a tracked regular file; an overlay adds and
// deletes nothing.
func (w *Workspace) Overlay(ctx context.Context, edits []git.FileEdit) (*Workspace, error) {
	base := w.Base()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	base.mu.Lock()
	if base.closed {
		base.mu.Unlock()
		return nil, fmt.Errorf("workspace: closed")
	}
	entries := make(map[string]git.TreeEntry, len(base.entries))
	for name, entry := range base.entries {
		entries[name] = entry
	}
	base.mu.Unlock()
	edited := map[string]git.FileEdit{}
	for _, edit := range edits {
		entry, ok := entries[edit.Path]
		if !ok || entry.Mode == 0120000 || edit.Delete {
			return nil, fmt.Errorf("workspace: an overlay replaces a tracked regular file the base holds; %q is not one", edit.Path)
		}
		if edit.Mode == 0 {
			edit.Mode = entry.Mode
		}
		edit.Before = git.FileState{Exists: true, Blob: entry.Object, Mode: entry.Mode}
		edited[edit.Path] = edit
	}
	directory, err := os.MkdirTemp(filepath.Dir(base.directory), "overlay-")
	if err != nil {
		return nil, err
	}
	directory, err = filepath.EvalSymlinks(directory)
	if err != nil {
		return nil, errors.Join(err, os.RemoveAll(directory))
	}
	overlay := &Workspace{repo: base.repo, source: base.source, directory: directory, base: base, entries: map[string]git.TreeEntry{}, resources: true}
	editsResources := false
	for _, edit := range edits {
		overlay.edits = append(overlay.edits, edited[edit.Path])
		if port := portDirectory(edit.Path); port != "" && !slices.Contains(overlay.ports, port) {
			overlay.ports = append(overlay.ports, port)
		}
		editsResources = editsResources || strings.HasPrefix(edit.Path, resources+"/")
	}
	slices.Sort(overlay.ports)
	// _resources is read, never written, by an evaluation, and it is a
	// hundred and fifty files: an overlay that does not edit it points one
	// symlink at the base's directory rather than linking every file, which
	// is what made a candidate evaluation cost thirty milliseconds of
	// kernel time. An overlay that edits a shared file gets real entries.
	base.mu.Lock()
	baseResources := base.resources || base.all
	base.mu.Unlock()
	if !editsResources && baseResources {
		if err := os.Symlink(filepath.Join(base.directory, resources), filepath.Join(directory, resources)); err != nil {
			return nil, errors.Join(err, os.RemoveAll(directory))
		}
		overlay.sharedResources = true
	}
	for name, entry := range entries {
		if !overlay.holds(name) {
			continue
		}
		if err := overlay.place(name, entry, edited); err != nil {
			return nil, errors.Join(err, os.RemoveAll(directory))
		}
	}
	register(overlay)
	return overlay, nil
}

// place puts one tracked entry into the overlay: the edit's contents, a
// re-created symlink, or a hardlink to the base's file.
func (w *Workspace) place(name string, entry git.TreeEntry, edited map[string]git.FileEdit) error {
	source := filepath.Join(w.base.directory, filepath.FromSlash(name))
	destination := filepath.Join(w.directory, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(destination), 0700); err != nil {
		return err
	}
	if edit, ok := edited[name]; ok {
		mode := os.FileMode(0600)
		if edit.Mode == 0100755 {
			mode = 0700
		}
		if err := os.WriteFile(destination, edit.After, mode); err != nil {
			return err
		}
		w.entries[name] = entry
		return nil
	}
	if entry.Mode == 0120000 {
		target, err := os.Readlink(source)
		if err != nil {
			return err
		}
		if err := os.Symlink(target, destination); err != nil {
			return err
		}
		w.entries[name] = entry
		return nil
	}
	if err := os.Link(source, destination); err != nil {
		return err
	}
	w.entries[name] = entry
	return nil
}

// holds reports whether a tracked path is within this projection's scope:
// everything once widened, _resources when materialized, and the port
// directories ensured or edited. Callers hold w.mu.
func (w *Workspace) holds(name string) bool {
	if strings.HasPrefix(name, resources+"/") {
		// An overlay sharing the base's _resources through a symlink holds
		// none of its entries itself, however wide it becomes.
		return !w.sharedResources && (w.all || w.resources)
	}
	if w.all {
		return true
	}
	port := portDirectory(name)
	return port != "" && slices.Contains(w.ports, port)
}

// portDirectory is the category/port directory a tracked path lies in, or
// empty for a path outside one: _resources, a dotfile directory, a
// top-level file.
func portDirectory(name string) string {
	parts := strings.SplitN(name, "/", 3)
	if len(parts) < 3 || strings.HasPrefix(parts[0], "_") || strings.HasPrefix(parts[0], ".") {
		return ""
	}
	return parts[0] + "/" + parts[1]
}

// linkNew places the base's entries within the overlay's scope that the
// overlay does not hold yet, after the base or the overlay gained scope.
func (w *Workspace) linkNew() error {
	base := w.base
	base.mu.Lock()
	entries := make(map[string]git.TreeEntry, len(base.entries))
	for name, entry := range base.entries {
		entries[name] = entry
	}
	base.mu.Unlock()
	w.mu.Lock()
	defer w.mu.Unlock()
	edited := map[string]git.FileEdit{}
	for _, edit := range w.edits {
		edited[edit.Path] = edit
	}
	for name, entry := range entries {
		if _, ok := w.entries[name]; ok || !w.holds(name) {
			continue
		}
		if err := w.place(name, entry, edited); err != nil {
			return err
		}
	}
	return nil
}

// Edits are the overlay's edits with their before states filled from the
// base's entries; a base has none.
func (w *Workspace) Edits() []git.FileEdit { return slices.Clone(w.edits) }

// Commit writes the overlay's edits as git objects over the base tree and
// returns the source whose tree they make. The overlay's files are, by
// construction, that tree's projection over the overlay's scope.
func (w *Workspace) Commit(ctx context.Context) (record.Source, error) {
	if w.base == nil {
		return w.source, nil
	}
	if w.repo == nil {
		return record.Source{}, fmt.Errorf("workspace: an overlay of an adopted directory has no repository to commit to")
	}
	tree, err := w.repo.EditTree(ctx, string(w.source.Tree), w.edits)
	if err != nil {
		return record.Source{}, err
	}
	return record.Source{Tree: record.ObjectID(tree), Base: w.source.Base}, nil
}

// Close closes the session, then removes the directory. Overlays close
// before their base; closing a base with open overlays leaves their files
// intact, since hardlinks survive their source.
func (w *Workspace) Close() error {
	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		return nil
	}
	w.closed = true
	batch := w.batch
	w.batch = nil
	w.mu.Unlock()
	unregister(w)
	var err error
	if batch != nil {
		err = batch.Close()
	}
	if w.adopted {
		return err
	}
	return errors.Join(err, os.RemoveAll(w.directory))
}

func fsValid(name string) bool {
	return name != "" && !strings.HasPrefix(name, "/") && !strings.Contains(name, "..") && !strings.ContainsAny(name, "\\\x00")
}
