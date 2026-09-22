package git

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf8"
)

var ErrBranchMissing = errors.New("git: local branch no longer exists")

// Branch resolves a literal local branch, without revision-expression fallback.
func (r *Repository) Branch(ctx context.Context, name string) (commit, tree string, err error) {
	if !ValidBranchName(name) {
		return "", "", fmt.Errorf("git: invalid local branch %q", name)
	}
	ref, err := r.ReadRef(ctx, "refs/heads/"+name)
	if err != nil {
		return "", "", err
	}
	if !ref.Exists {
		return "", "", fmt.Errorf("%w: %s", ErrBranchMissing, name)
	}
	trees, err := r.CommitTrees(ctx, []string{ref.Object})
	if err != nil {
		return "", "", err
	}
	return ref.Object, trees[ref.Object], nil
}

func ValidBranchName(name string) bool {
	return name != "" && utf8.ValidString(name) && !strings.HasPrefix(name, "refs/") && !strings.HasPrefix(name, "-") && validRefName("refs/heads/"+name)
}

type Snapshot struct {
	Tree      string
	Root      string
	directory string
}

func (s *Snapshot) Close() error { return os.RemoveAll(s.directory) }

// Materialize reads raw blobs, avoiding checkout filters and archive attributes.
// The returned directory is owned by the caller until Close.
func (r *Repository) Materialize(ctx context.Context, tree string) (_ *Snapshot, err error) {
	directory, err := os.MkdirTemp("", "dockhand-source-")
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			err = errors.Join(err, os.RemoveAll(directory))
		}
	}()
	canonical, err := filepath.EvalSymlinks(directory)
	if err != nil {
		return nil, err
	}
	directory = canonical
	if _, err = r.MaterializeInto(ctx, tree, directory, nil, nil); err != nil {
		return nil, err
	}
	return &Snapshot{Tree: tree, Root: directory, directory: directory}, nil
}

// MaterializeInto writes the tree's blobs under an existing directory: the
// paths the include pathspecs select, or every path with none, less the
// paths present already, which are skipped rather than created again. It
// returns the entries it wrote.
func (r *Repository) MaterializeInto(ctx context.Context, tree, directory string, include []string, present map[string]bool) ([]TreeEntry, error) {
	typ, err := r.ObjectType(ctx, tree)
	if err != nil {
		return nil, err
	}
	if typ != "tree" {
		return nil, fmt.Errorf("git: %s is not a tree", tree)
	}
	args := []string{"ls-tree", "-rz", tree, "--"}
	for _, name := range include {
		if !snapshotPath(name) {
			return nil, fmt.Errorf("git: invalid pathspec %q", name)
		}
		args = append(args, name)
	}
	if len(include) == 0 {
		args = append(args, ".")
	}
	out, err := r.output(ctx, args...)
	if err != nil {
		return nil, err
	}
	var entries []TreeEntry
	var input strings.Builder
	for _, line := range bytes.Split(bytes.TrimSuffix(out, []byte{0}), []byte{0}) {
		if len(line) == 0 {
			continue
		}
		meta, name, ok := bytes.Cut(line, []byte{'\t'})
		fields := strings.Fields(string(meta))
		if !ok || len(fields) != 3 || fields[1] != "blob" || !ValidObjectID(fields[2]) || !snapshotPath(string(name)) {
			return nil, fmt.Errorf("git: unsupported snapshot entry %q", line)
		}
		mode, parseErr := strconv.ParseUint(fields[0], 8, 32)
		if parseErr != nil || (mode != 0100644 && mode != 0100755 && mode != 0120000) {
			return nil, fmt.Errorf("git: unsupported snapshot mode %q", fields[0])
		}
		if present[string(name)] {
			continue
		}
		entries = append(entries, TreeEntry{Name: string(name), Mode: uint32(mode), Object: fields[2]})
		input.WriteString(fields[2] + "\n")
	}
	if len(entries) == 0 {
		return nil, ctx.Err()
	}
	command := r.command(ctx, nil, "cat-file", "--batch")
	command.Stdin = strings.NewReader(input.String())
	var stderr bytes.Buffer
	command.Stderr = &stderr
	stdout, err := command.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err = command.Start(); err != nil {
		return nil, err
	}
	readErr := extractBlobs(ctx, bufio.NewReader(stdout), directory, entries)
	if readErr != nil {
		_ = command.Process.Kill()
	}
	waitErr := command.Wait()
	if err = errors.Join(ctx.Err(), readErr, waitErr); err != nil {
		return nil, fmt.Errorf("git: materializing %s: %w: %s", tree, err, strings.TrimSpace(stderr.String()))
	}
	return entries, nil
}

func snapshotPath(name string) bool {
	if !fs.ValidPath(name) || name == "." || strings.ContainsAny(name, "\\\x00") {
		return false
	}
	for _, part := range strings.Split(name, "/") {
		if strings.EqualFold(part, ".git") {
			return false
		}
	}
	return true
}

func extractBlobs(ctx context.Context, in *bufio.Reader, root string, entries []TreeEntry) error {
	type link struct{ name, target string }
	var links []link
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		header, err := in.ReadString('\n')
		if err != nil {
			return err
		}
		fields := strings.Fields(header)
		if len(fields) != 3 || fields[0] != entry.Object || fields[1] != "blob" {
			return fmt.Errorf("invalid blob header %q", header)
		}
		size, err := strconv.ParseInt(fields[2], 10, 64)
		if err != nil || size < 0 {
			return fmt.Errorf("invalid blob size %q", fields[2])
		}
		filename := filepath.Join(root, filepath.FromSlash(entry.Name))
		if err := os.MkdirAll(filepath.Dir(filename), 0700); err != nil {
			return err
		}
		if entry.Mode == 0120000 {
			if size > 4096 {
				return fmt.Errorf("symlink target too long: %s", entry.Name)
			}
			data := make([]byte, int(size))
			if _, err := io.ReadFull(in, data); err != nil {
				return err
			}
			target := string(data)
			resolved := path.Clean(path.Join(path.Dir(entry.Name), target))
			if target == "" || path.IsAbs(target) || strings.ContainsAny(target, "\\\x00") || resolved == ".." || strings.HasPrefix(resolved, "../") {
				return fmt.Errorf("symlink leaves snapshot: %s", entry.Name)
			}
			links = append(links, link{filename, target})
		} else {
			mode := fs.FileMode(0600)
			if entry.Mode == 0100755 {
				mode = 0700
			}
			file, err := os.OpenFile(filename, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
			if err != nil {
				return err
			}
			_, copyErr := io.CopyN(file, in, size)
			if err := errors.Join(copyErr, file.Close()); err != nil {
				return err
			}
		}
		b, err := in.ReadByte()
		if err != nil {
			return err
		}
		if b != '\n' {
			return fmt.Errorf("invalid blob terminator")
		}
	}
	for _, link := range links {
		if err := os.Symlink(link.target, link.name); err != nil {
			return err
		}
	}
	// Resolve chains as well as individual targets before exposing the tree.
	for _, link := range links {
		actual, err := filepath.EvalSymlinks(link.name)
		if err != nil {
			return fmt.Errorf("invalid snapshot symlink %s: %w", link.name, err)
		}
		rel, err := filepath.Rel(root, actual)
		if err != nil || rel == ".." || strings.HasPrefix(rel, "../") {
			return fmt.Errorf("symlink leaves snapshot: %s", link.name)
		}
	}
	return nil
}

func (r *Repository) CurrentBranch(ctx context.Context) (string, error) {
	out, err := r.output(ctx, "symbolic-ref", "--quiet", "HEAD")
	if err != nil {
		return "", fmt.Errorf("git: select --branch when HEAD is detached or has no branch: %w", err)
	}
	ref := strings.TrimSpace(string(out))
	branch, ok := strings.CutPrefix(ref, "refs/heads/")
	if !ok || !ValidBranchName(branch) {
		return "", fmt.Errorf("git: HEAD does not name a local branch")
	}
	return branch, nil
}
