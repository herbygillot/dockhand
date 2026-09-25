package git

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/herbygillot/dockhand/internal/atomicfile"
)

// ErrWorkingFile reports a working file that is not a regular file, or
// that changed since it was read.
var ErrWorkingFile = errors.New("git: working file")

// WorkingTree writes the checkout's tracked files, as they are on disk, into
// a tree: HEAD's tree with every tracked change applied, staged or not. A
// tracked file missing from disk is left out; untracked files are not read,
// and neither are files a sparse checkout leaves out. Nothing in the
// checkout, its index, or its refs changes; only objects are written. It
// returns HEAD's commit and the tree.
func (r *Repository) WorkingTree(ctx context.Context) (head, tree string, err error) {
	if head, err = r.Resolve(ctx, "HEAD^{commit}"); err != nil {
		return "", "", err
	}
	trees, err := r.CommitTrees(ctx, []string{head})
	if err != nil {
		return "", "", err
	}
	base := trees[head]
	changes, err := r.TrackedChanges(ctx)
	if err != nil {
		return "", "", err
	}
	var edits []FileEdit
	seen := map[string]bool{}
	for _, name := range changes {
		if seen[name] {
			continue
		}
		seen[name] = true
		before, _, err := r.File(ctx, base, name)
		if err != nil {
			return "", "", err
		}
		data, mode, present, err := r.readWorking(name)
		if err != nil {
			return "", "", err
		}
		switch {
		case !present && before.Exists:
			edits = append(edits, FileEdit{Path: name, Before: before, Delete: true})
		case present:
			edits = append(edits, FileEdit{Path: name, Before: before, After: data, Mode: mode})
		}
	}
	if tree, err = r.EditTree(ctx, base, edits); err != nil {
		return "", "", err
	}
	return head, tree, nil
}

// readWorking reads a tracked file from the working directory, refusing
// anything but a regular file.
func (r *Repository) readWorking(name string) (data []byte, mode uint32, present bool, err error) {
	path := filepath.Join(r.Root, filepath.FromSlash(name))
	info, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, 0, false, nil
	}
	if err != nil {
		return nil, 0, false, err
	}
	if !info.Mode().IsRegular() {
		return nil, 0, false, fmt.Errorf("%w: %s is not a regular file", ErrWorkingFile, name)
	}
	if data, err = os.ReadFile(path); err != nil {
		return nil, 0, false, err
	}
	mode = 0o100644
	if info.Mode().Perm()&0o111 != 0 {
		mode = 0o100755
	}
	return data, mode, true, nil
}

// BlobID is the object ID data would have as a blob, without writing it.
func (r *Repository) BlobID(ctx context.Context, data []byte) (string, error) {
	out, err := r.run(ctx, data, nil, "hash-object", "--stdin")
	return objectResult(out, err)
}

// ApplyToWorkingFiles writes edits into the checkout's working files, and
// nothing into its index or refs. Every file must still be as each edit's
// Before describes it: the same contents and executable bit, or absent.
// All are checked before any is written, so a file that changed since the
// edits were prepared leaves every file as it was.
func (r *Repository) ApplyToWorkingFiles(ctx context.Context, edits []FileEdit) error {
	for _, edit := range edits {
		if !snapshotPath(edit.Path) {
			return fmt.Errorf("git: invalid file path %q", edit.Path)
		}
		data, mode, present, err := r.readWorking(edit.Path)
		if err != nil {
			return err
		}
		if present != edit.Before.Exists {
			return fmt.Errorf("%w: %s changed while the edit was prepared", ErrWorkingFile, edit.Path)
		}
		if !present {
			continue
		}
		blob, err := r.BlobID(ctx, data)
		if err != nil {
			return err
		}
		if blob != edit.Before.Blob || mode != edit.Before.Mode {
			return fmt.Errorf("%w: %s changed while the edit was prepared", ErrWorkingFile, edit.Path)
		}
	}
	for _, edit := range edits {
		if err := ctx.Err(); err != nil {
			return err
		}
		path := filepath.Join(r.Root, filepath.FromSlash(edit.Path))
		if edit.Delete {
			if err := os.Remove(path); err != nil {
				return err
			}
			continue
		}
		perm := fs.FileMode(0o644)
		if info, err := os.Stat(path); err == nil {
			perm = info.Mode().Perm()
		} else if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		if edit.Mode == 0o100755 {
			perm |= 0o111
		} else {
			perm &^= 0o111
		}
		if err := atomicfile.Write(path, edit.After, perm); err != nil {
			return err
		}
	}
	return nil
}
