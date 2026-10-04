package git

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

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

// workingBlobID is the object ID a working file would have as a blob, as
// Git cleans it for its path: under core.autocrlf, a checked-out CRLF file
// is the LF blob it came from, not another (the M1's rerun, F4).
func (r *Repository) workingBlobID(ctx context.Context, data []byte, path string) (string, error) {
	out, err := r.run(ctx, data, nil, "hash-object", "--stdin", "--path="+path)
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
		blob, err := r.workingBlobID(ctx, data, edit.Path)
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

// IndexTree writes the index as it stands into a tree, as a commit would
// record it. Nothing but objects is written.
func (r *Repository) IndexTree(ctx context.Context) (string, error) {
	out, err := r.output(ctx, "write-tree")
	return objectResult(out, err)
}

// Untracked lists files in the checkout that Git neither tracks nor
// ignores.
func (r *Repository) Untracked(ctx context.Context) ([]string, error) {
	out, err := r.output(ctx, "ls-files", "-z", "--others", "--exclude-standard")
	if err != nil {
		return nil, err
	}
	var paths []string
	for path := range strings.SplitSeq(string(out), "\x00") {
		if path != "" {
			paths = append(paths, path)
		}
	}
	return paths, nil
}

// Ignored are the paths among those given that Git ignores, by a
// .gitignore in the worktree at any depth, .git/info/exclude, or
// core.excludesFile, as git check-ignore reads them: whatever Git itself
// would leave out of git add, dockhand leaves out too.
func (r *Repository) Ignored(ctx context.Context, paths ...string) ([]string, error) {
	for _, name := range paths {
		if !snapshotPath(name) {
			return nil, fmt.Errorf("git: invalid path %q", name)
		}
	}
	if len(paths) == 0 {
		return nil, nil
	}
	out, err := r.run(ctx, []byte(strings.Join(paths, "\x00")+"\x00"), nil, "check-ignore", "-z", "--stdin")
	// check-ignore exits 1 when none of them is ignored, which isn't an
	// error here.
	if exit := new(exec.ExitError); errors.As(err, &exit) && exit.ExitCode() == 1 {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var ignored []string
	for path := range strings.SplitSeq(string(out), "\x00") {
		if path != "" {
			ignored = append(ignored, path)
		}
	}
	return ignored, nil
}

// Conflicts lists paths with unresolved merge conflicts.
func (r *Repository) Conflicts(ctx context.Context) ([]string, error) {
	out, err := r.output(ctx, "diff", "--name-only", "-z", "--diff-filter=U")
	if err != nil {
		return nil, err
	}
	var paths []string
	for path := range strings.SplitSeq(string(out), "\x00") {
		if path != "" {
			paths = append(paths, path)
		}
	}
	return paths, nil
}

// WithFiles is tree with the named working files added as they are on
// disk, for untracked files a capture includes. Each must be a regular
// file.
func (r *Repository) WithFiles(ctx context.Context, tree string, paths []string) (string, error) {
	var edits []FileEdit
	for _, name := range paths {
		before, _, err := r.File(ctx, tree, name)
		if err != nil {
			return "", err
		}
		data, mode, present, err := r.readWorking(name)
		if err != nil {
			return "", err
		}
		if !present {
			return "", fmt.Errorf("%w: %s does not exist", ErrWorkingFile, name)
		}
		edits = append(edits, FileEdit{Path: name, Before: before, After: data, Mode: mode})
	}
	return r.EditTree(ctx, tree, edits)
}
