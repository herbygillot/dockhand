package git

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
)

var ErrFilePrecondition = errors.New("git: file precondition does not match")

type FileState struct {
	Exists bool
	Blob   string
	Mode   uint32
}

type FileEdit struct {
	Path   string
	Before FileState
	After  []byte
	Mode   uint32
	Delete bool
}

func (r *Repository) File(ctx context.Context, tree, name string) (FileState, []byte, error) {
	if !snapshotPath(name) {
		return FileState{}, nil, fmt.Errorf("git: invalid file path %q", name)
	}
	entry, err := r.fileEntry(ctx, tree, strings.Split(name, "/"))
	if err != nil {
		return FileState{}, nil, err
	}
	if entry == nil {
		return FileState{}, nil, nil
	}
	data, err := r.ReadBlob(ctx, entry.Object)
	if err != nil {
		return FileState{}, nil, err
	}
	return FileState{Exists: true, Blob: entry.Object, Mode: entry.Mode}, data, nil
}

func (r *Repository) fileEntry(ctx context.Context, tree string, parts []string) (*TreeEntry, error) {
	entries, err := r.ReadTree(ctx, tree)
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		if entry.Name != parts[0] {
			continue
		}
		if len(parts) == 1 {
			if entry.Type != "blob" || !regularMode(entry.Mode) {
				return nil, fmt.Errorf("git: %s is not a regular file", strings.Join(parts, "/"))
			}
			return &entry, nil
		}
		if entry.Type != "tree" {
			return nil, fmt.Errorf("git: %s is not a directory", entry.Name)
		}
		return r.fileEntry(ctx, entry.Object, parts[1:])
	}
	return nil, nil
}

func regularMode(mode uint32) bool { return mode == 0o100644 || mode == 0o100755 }

// EditTree writes new objects without moving refs or touching a worktree/index.
// All preconditions are checked against the same immutable input tree.
func (r *Repository) EditTree(ctx context.Context, tree string, edits []FileEdit) (string, error) {
	typ, err := r.ObjectType(ctx, tree)
	if err != nil {
		return "", err
	}
	if typ != "tree" {
		return "", fmt.Errorf("git: edit input must be a tree")
	}
	ordered := slices.Clone(edits)
	slices.SortFunc(ordered, func(a, b FileEdit) int { return strings.Compare(a.Path, b.Path) })
	paths := make(map[string]bool, len(ordered))
	for _, edit := range ordered {
		if !snapshotPath(edit.Path) || paths[edit.Path] {
			return "", fmt.Errorf("git: invalid or duplicate edit path %q", edit.Path)
		}
		paths[edit.Path] = true
		if edit.Before.Exists {
			if !ValidObjectID(edit.Before.Blob) || !regularMode(edit.Before.Mode) {
				return "", fmt.Errorf("git: invalid precondition for %s", edit.Path)
			}
		} else if edit.Before.Blob != "" || edit.Before.Mode != 0 {
			return "", fmt.Errorf("git: absent file has object/mode preconditions: %s", edit.Path)
		}
		if edit.Delete {
			if !edit.Before.Exists || edit.Mode != 0 || len(edit.After) != 0 {
				return "", fmt.Errorf("git: invalid deletion for %s", edit.Path)
			}
		} else if !regularMode(edit.Mode) {
			return "", fmt.Errorf("git: unsupported edit mode for %s", edit.Path)
		}
	}
	for _, edit := range ordered {
		parts := strings.Split(edit.Path, "/")
		for i := 1; i < len(parts); i++ {
			if paths[strings.Join(parts[:i], "/")] {
				return "", fmt.Errorf("git: overlapping file and directory edits: %s", edit.Path)
			}
		}
		entry, err := r.fileEntry(ctx, tree, parts)
		if err != nil {
			return "", err
		}
		actual := FileState{}
		if entry != nil {
			actual = FileState{Exists: true, Blob: entry.Object, Mode: entry.Mode}
		}
		if actual != edit.Before {
			return "", fmt.Errorf("%w: %s", ErrFilePrecondition, edit.Path)
		}
	}
	if len(ordered) == 0 {
		return tree, ctx.Err()
	}
	return r.editTree(ctx, tree, ordered)
}

func (r *Repository) editTree(ctx context.Context, tree string, edits []FileEdit) (string, error) {
	var entries []TreeEntry
	var err error
	if tree != "" {
		entries, err = r.ReadTree(ctx, tree)
		if err != nil {
			return "", err
		}
	}
	byName := make(map[string]TreeEntry, len(entries))
	for _, entry := range entries {
		byName[entry.Name] = entry
	}
	children := map[string][]FileEdit{}
	for _, edit := range edits {
		head, tail, nested := strings.Cut(edit.Path, "/")
		if nested {
			edit.Path = tail
			children[head] = append(children[head], edit)
			continue
		}
		if edit.Delete {
			delete(byName, head)
			continue
		}
		blob, err := r.WriteBlob(ctx, edit.After)
		if err != nil {
			return "", err
		}
		byName[head] = TreeEntry{Name: head, Mode: edit.Mode, Type: "blob", Object: blob}
	}
	childNames := make([]string, 0, len(children))
	for name := range children {
		childNames = append(childNames, name)
	}
	slices.Sort(childNames)
	for _, name := range childNames {
		next, err := r.editTree(ctx, byName[name].Object, children[name])
		if err != nil {
			return "", err
		}
		remaining, err := r.ReadTree(ctx, next)
		if err != nil {
			return "", err
		}
		if len(remaining) == 0 {
			delete(byName, name)
		} else {
			byName[name] = TreeEntry{Name: name, Mode: 0o40000, Type: "tree", Object: next}
		}
	}
	entries = entries[:0]
	for _, entry := range byName {
		entries = append(entries, entry)
	}
	slices.SortFunc(entries, func(a, b TreeEntry) int { return strings.Compare(a.Name, b.Name) })
	return r.WriteTree(ctx, entries)
}

func (r *Repository) DiffTrees(ctx context.Context, before, after string) ([]byte, error) {
	types, err := r.ObjectTypes(ctx, []string{before, after})
	if err != nil {
		return nil, err
	}
	if types[before] != "tree" || types[after] != "tree" {
		return nil, fmt.Errorf("git: diff inputs must be trees")
	}
	return r.output(ctx, "diff", "--no-ext-diff", "--no-textconv", "--no-renames", "--no-color", "--binary", "--src-prefix=a/", "--dst-prefix=b/", before, after, "--")
}
