package engine

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"path"
	"regexp"
	"slices"
	"strings"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/portfile"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/store"
)

// PortRenameRequest is a port renamed, or moved to another category, on a
// branch, as MacPorts renames one: its Portfile and files/ at the new
// name's directory, and for a rename the old Portfile an obsolete stub
// replaced_by the new name (the person's "I agree we add a `rename`
// action to Dockhand.", 2026-10-29, from goplus, whose project gop became
// xgo).
type PortRenameRequest struct {
	Branch model.Branch
	Port   string
	// Name is the port's new name; empty moves it, keeping its name.
	Name string
	// Category is the new directory's: the port's own where empty, for a
	// rename. A move names one.
	Category string
	Plan     bool
}

// RenamedPort is a port's rename or move, as written or, for a plan, as it
// would be.
type RenamedPort struct {
	Port, Name string
	// From and To are the port's directories before and after.
	From, To string
	// Stub is the obsolete stub a rename leaves at From; nil for a move.
	Stub *portfile.Stub
	// Category says the categories line names the new category first now;
	// false where the directory's category didn't change, or no
	// categories line named the old one first.
	Category bool
	// Left are the new Portfile's lines still naming the old name, which
	// what the new name implies is the person's to change: a Go module's
	// path, a binary's name, notes.
	Left []string
	// Dependents are the ports that depend on the old name, by the port
	// index's graph, whose lines are the person's; DependentsProblem why
	// they couldn't be read.
	Dependents        []string
	DependentsProblem string
	Applied           bool
}

// RenamePort renames or moves a port on a branch: one edit, recorded, so
// tidy takes it as dockhand's. It does the mechanical part alone, saying
// what's left; a new release is update of the new name after. The port's
// directory is moved as committed, so one with changes not committed is
// refused.
func (e *Engine) RenamePort(ctx context.Context, request PortRenameRequest) (RenamedPort, error) {
	moving := request.Name == ""
	name := cmp.Or(request.Name, request.Port)
	if !macports.ValidName(name) {
		return RenamedPort{}, fmt.Errorf("%q is not a port name", name)
	}
	worktree, err := e.worktree(ctx, request.Branch)
	if err != nil {
		return RenamedPort{}, err
	}
	head, err := worktree.Resolve(ctx, "HEAD^{commit}")
	if err != nil {
		return RenamedPort{}, err
	}
	trees, err := worktree.CommitTrees(ctx, []string{head})
	if err != nil {
		return RenamedPort{}, err
	}
	tree := trees[head]
	from, err := e.findDirectory(ctx, tree, request.Port)
	if err != nil {
		return RenamedPort{}, err
	}
	if path.Base(from) != request.Port {
		return RenamedPort{}, fmt.Errorf("%s is a subport of %s's Portfile; rename the Portfile's port, %s", request.Port, from, path.Base(from))
	}
	category := cmp.Or(request.Category, path.Dir(from))
	if !macports.ValidCategory(category) {
		return RenamedPort{}, fmt.Errorf("%q is not a category", category)
	}
	to := category + "/" + name
	switch {
	case to == from && moving:
		return RenamedPort{}, fmt.Errorf("%s is in %s already", request.Port, category)
	case to == from:
		return RenamedPort{}, fmt.Errorf("%s is %s's name already", name, request.Port)
	}
	changed, err := worktree.TrackedChanges(ctx)
	if err != nil {
		return RenamedPort{}, err
	}
	if slices.ContainsFunc(changed, func(file string) bool { return strings.HasPrefix(file, from+"/") }) {
		return RenamedPort{}, fmt.Errorf("%s has changes not committed; it's moved as committed, so tidy or commit them first", from)
	}
	if existing, err := worktree.ExistingPaths(ctx, tree, []string{to}); err != nil || len(existing) > 0 {
		if err != nil {
			return RenamedPort{}, err
		}
		return RenamedPort{}, fmt.Errorf("there is already a %s", to)
	}
	if !moving {
		if err := e.refuseExisting(ctx, worktree, name); err != nil {
			return RenamedPort{}, err
		}
	}
	entries, err := worktree.FilesBelow(ctx, tree, from)
	if err != nil {
		return RenamedPort{}, err
	}
	var paths []string
	for _, entry := range entries {
		paths = append(paths, entry.Name)
	}
	contents, err := worktree.ReadFiles(ctx, tree, paths)
	if err != nil {
		return RenamedPort{}, err
	}
	renamed := RenamedPort{Port: request.Port, Name: name, From: from, To: to}
	text := contents[from+"/Portfile"]
	if !moving {
		if text, err = portfile.Rename(text, request.Port, name); err != nil {
			return RenamedPort{}, fmt.Errorf("%s: %w", from, err)
		}
		if renamed.Stub, err = e.stubFor(ctx, tree, from, request.Port, name); err != nil {
			return RenamedPort{}, err
		}
	}
	if text, renamed.Category, err = portfile.SetPrimaryCategory(text, path.Dir(from), category); err != nil {
		return RenamedPort{}, fmt.Errorf("%s: %w", from, err)
	}
	if !moving {
		renamed.Left = linesNaming(text, request.Port)
	}
	var edits []git.FileEdit
	for _, entry := range entries {
		before := git.FileState{Exists: true, Blob: entry.Object, Mode: entry.Mode}
		moved := to + strings.TrimPrefix(entry.Name, from)
		data := contents[entry.Name]
		if entry.Name == from+"/Portfile" {
			data = text
		}
		edits = append(edits, git.FileEdit{Path: moved, After: data, Mode: entry.Mode})
		if entry.Name == from+"/Portfile" && renamed.Stub != nil {
			edits = append(edits, git.FileEdit{Path: entry.Name, Before: before, After: renamed.Stub.Text(), Mode: entry.Mode})
			continue
		}
		edits = append(edits, git.FileEdit{Path: entry.Name, Before: before, Delete: true})
	}
	if !moving {
		renamed.Dependents, renamed.DependentsProblem = e.dependentNames(ctx, model.Source{Commit: model.ObjectID(head), Tree: model.ObjectID(tree)}, from)
	}
	if request.Plan {
		return renamed, nil
	}
	if err := expandFor(ctx, worktree, []string{from + "/Portfile", to + "/Portfile"}); err != nil {
		return renamed, err
	}
	if err := worktree.ApplyToWorkingFiles(ctx, edits); err != nil {
		if errors.Is(err, git.ErrWorkingFile) {
			return renamed, fmt.Errorf("%w; nothing was written, so run it again", err)
		}
		return renamed, err
	}
	// The new directory is staged, so a check of the working files takes
	// it, as create's new port is.
	if err := worktree.AddAll(ctx, from, to); err != nil {
		return renamed, err
	}
	renamed.Applied = true
	edit := model.Edit{ID: model.EditID(store.NewID("ed")), Branch: request.Branch.ID, Kind: model.EditRename, Port: name, Directory: to,
		Subject: name + ": rename from " + request.Port, At: e.now()}
	if moving {
		edit.Kind, edit.Subject = model.EditMove, name+": move to "+category
	}
	for _, file := range edits {
		recorded := model.EditedFile{Path: file.Path, Before: model.ObjectID(file.Before.Blob)}
		if !file.Delete {
			blob, err := worktree.BlobID(ctx, file.After)
			if err != nil {
				return renamed, err
			}
			recorded.After = model.ObjectID(blob)
		}
		edit.Files = append(edit.Files, recorded)
	}
	return renamed, store.Recorded(ctx, e.Store, e.Repository, func(tx store.Tx) error {
		if err := tx.AddEdit(edit); err != nil {
			return err
		}
		_, err := tx.AppendEvent(model.Event{At: edit.At, Branch: request.Branch.ID, Kind: model.EventBranchEdit, Level: model.LevelInfo,
			Message: fmt.Sprintf("%s: %s, from %s to %s", name, edit.Kind, from, to)})
		return err
	}, editRecorded(request.Branch.ID, edit.ID))
}

// stubFor is the obsolete stub a port renamed to name leaves at its
// directory: its version, epoch, and categories as the tree evaluates
// them, and its revision raised.
func (e *Engine) stubFor(ctx context.Context, tree, directory, port, name string) (*portfile.Stub, error) {
	reader, err := e.portReader()
	if err != nil {
		return nil, err
	}
	infos, err := reader.Ports(ctx, model.Source{Tree: model.ObjectID(tree)}, directory, model.Environment{}, nil)
	if err != nil {
		return nil, fmt.Errorf("%s couldn't be read for its stub: %w", directory, err)
	}
	i := slices.IndexFunc(infos, func(info macports.PortInfo) bool { return info.Name == port })
	if i < 0 {
		return nil, fmt.Errorf("%s has no port %s to rename", directory, port)
	}
	info := infos[i]
	return &portfile.Stub{Name: port, ReplacedBy: name, Version: info.Version, Revision: info.Revision + 1, Epoch: info.Epoch, Categories: info.Categories()}, nil
}

// dependentNames are the ports depending on a directory's ports, by name,
// or why they couldn't be read.
func (e *Engine) dependentNames(ctx context.Context, source model.Source, directory string) ([]string, string) {
	found, err := e.dependents(ctx, source, []string{directory})
	if err != nil {
		return nil, err.Error()
	}
	var names []string
	for _, dependent := range found {
		if !slices.Contains(names, dependent.Name) {
			names = append(names, dependent.Name)
		}
	}
	slices.SortFunc(names, macports.ComparePortNames)
	return names, ""
}

// linesNaming are a Portfile's lines that name a word, as "line 12:
// go.package github.com/goplus/gop".
func linesNaming(text []byte, word string) []string {
	named := regexp.MustCompile(`(^|[^A-Za-z0-9_.-])` + regexp.QuoteMeta(word) + `($|[^A-Za-z0-9_-])`)
	var lines []string
	for n, line := range strings.Split(string(text), "\n") {
		if named.MatchString(line) {
			lines = append(lines, fmt.Sprintf("line %d: %s", n+1, strings.TrimSpace(line)))
		}
	}
	return lines
}
