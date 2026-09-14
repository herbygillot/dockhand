package git

import (
	"bytes"
	"context"
	"crypto/sha1"
	"crypto/sha256"
	"errors"
	"fmt"
	"hash"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

var ErrCheckoutChanged = errors.New("git: checkout changed during capture; retry verification")

type Checkout struct {
	Branch        string
	Head          string
	Tree          string
	ModifiedFiles int
	ModifiedPaths []string
	Untracked     []string
}

// CaptureCheckout records raw working bytes for index-tracked paths. Only a
// private index is written. Two matching reads detect ordinary concurrent edits;
// this is not a filesystem-wide atomic snapshot.
func (r *Repository) CaptureCheckout(ctx context.Context) (Checkout, error) {
	branch, head, err := r.checkoutHead(ctx)
	if err != nil {
		return Checkout{}, err
	}
	original, err := r.CommitTrees(ctx, []string{head})
	if err != nil {
		return Checkout{}, err
	}
	index, err := r.checkoutIndex(ctx)
	if err != nil {
		return Checkout{}, err
	}
	entries, err := r.captureFiles(ctx, index, len(head), true)
	if err != nil {
		return Checkout{}, err
	}
	untracked, err := r.output(ctx, "ls-files", "--others", "--exclude-standard", "-z")
	if err != nil {
		return Checkout{}, err
	}
	again, err := r.captureFiles(ctx, index, len(head), false)
	if err != nil {
		return Checkout{}, err
	}
	nextIndex, err := r.checkoutIndex(ctx)
	if err != nil {
		return Checkout{}, err
	}
	nextBranch, nextHead, err := r.checkoutHead(ctx)
	if err != nil {
		return Checkout{}, err
	}
	if branch != nextBranch || head != nextHead || !bytes.Equal(index, nextIndex) || !bytes.Equal(entries, again) {
		return Checkout{}, ErrCheckoutChanged
	}
	directory, err := os.MkdirTemp("", "dockhand-index-")
	if err != nil {
		return Checkout{}, err
	}
	defer os.RemoveAll(directory)
	env := []string{"GIT_INDEX_FILE=" + filepath.Join(directory, "index")}
	if _, err := r.run(ctx, nil, env, "read-tree", "--empty"); err != nil {
		return Checkout{}, err
	}
	if _, err := r.run(ctx, entries, env, "update-index", "-z", "--index-info"); err != nil {
		return Checkout{}, err
	}
	tree, err := r.run(ctx, nil, env, "write-tree")
	if err != nil {
		return Checkout{}, err
	}
	result := Checkout{Branch: branch, Head: head, Tree: strings.TrimSpace(string(tree))}
	result.ModifiedPaths, err = r.ChangedPaths(ctx, original[head], result.Tree)
	if err != nil {
		return Checkout{}, err
	}
	result.ModifiedFiles = len(result.ModifiedPaths)
	if len(untracked) > 0 {
		result.Untracked = strings.Split(strings.TrimSuffix(string(untracked), "\x00"), "\x00")
	}
	return result, nil
}

func (r *Repository) checkoutHead(ctx context.Context) (string, string, error) {
	head, err := r.Resolve(ctx, "HEAD")
	if err != nil {
		return "", "", fmt.Errorf("git: working-tree verification requires an existing HEAD commit: %w", err)
	}
	out, err := r.output(ctx, "symbolic-ref", "--quiet", "HEAD")
	if err != nil {
		var failure *CommandError
		var exit interface{ ExitCode() int }
		if errors.As(err, &failure) && errors.As(failure.Cause, &exit) && exit.ExitCode() == 1 {
			return "", head, nil
		}
		return "", "", err
	}
	branch, ok := strings.CutPrefix(strings.TrimSpace(string(out)), "refs/heads/")
	if !ok || !ValidBranchName(branch) {
		return "", "", fmt.Errorf("git: invalid checkout branch")
	}
	return branch, head, nil
}

func (r *Repository) checkoutIndex(ctx context.Context) ([]byte, error) {
	flags, err := r.output(ctx, "ls-files", "-v", "-z")
	if err != nil {
		return nil, err
	}
	for _, entry := range bytes.Split(flags, []byte{0}) {
		if len(entry) > 0 && (entry[0] == 'S' || entry[0] == 's') {
			return nil, fmt.Errorf("git: sparse or skip-worktree entries are unsupported; use --branch for committed verification")
		}
	}
	return r.output(ctx, "ls-files", "--stage", "-z")
}

func (r *Repository) captureFiles(ctx context.Context, index []byte, objectLength int, write bool) ([]byte, error) {
	root, err := os.OpenRoot(r.Root)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	var out bytes.Buffer
	for _, entry := range bytes.Split(index, []byte{0}) {
		if len(entry) == 0 {
			continue
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		metadata, path, ok := bytes.Cut(entry, []byte{'\t'})
		fields := strings.Fields(string(metadata))
		name := string(path)
		if !ok || len(fields) != 3 || fields[2] != "0" || !snapshotPath(name) {
			return nil, fmt.Errorf("git: unresolved or invalid index entry; resolve conflicts before verification")
		}
		if fields[0] != "100644" && fields[0] != "100755" && fields[0] != "120000" {
			return nil, fmt.Errorf("git: unsupported index mode for %q", name)
		}
		info, err := root.Lstat(name)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("git: reading %q: %w", name, err)
		}
		var data []byte
		mode := uint32(0100644)
		switch {
		case info.Mode()&os.ModeSymlink != 0:
			target, e := root.Readlink(name)
			err = e
			data = []byte(target)
			mode = 0120000
		case info.Mode().IsRegular():
			if info.Size() > 128<<20 {
				return nil, fmt.Errorf("git: working file exceeds 128 MiB: %q", name)
			}
			file, e := root.Open(name)
			if e != nil {
				return nil, e
			}
			data, err = io.ReadAll(io.LimitReader(file, 128<<20+1))
			err = errors.Join(err, file.Close())
			if len(data) > 128<<20 {
				return nil, fmt.Errorf("git: working file exceeds 128 MiB: %q", name)
			}
			if info.Mode()&0111 != 0 {
				mode = 0100755
			}
		default:
			return nil, fmt.Errorf("git: unsupported working file type: %q", name)
		}
		if err != nil {
			return nil, err
		}
		var h hash.Hash
		switch objectLength {
		case 40:
			h = sha1.New()
		case 64:
			h = sha256.New()
		default:
			return nil, fmt.Errorf("git: unsupported object format")
		}
		fmt.Fprintf(h, "blob %d\x00", len(data))
		h.Write(data)
		object := fmt.Sprintf("%x", h.Sum(nil))
		if write && object != fields[1] {
			actual, err := r.WriteBlob(ctx, data)
			if err != nil {
				return nil, err
			}
			if actual != object {
				return nil, fmt.Errorf("git: working blob identity mismatch")
			}
		}
		out.WriteString(strconv.FormatUint(uint64(mode), 8) + " " + object + "\t" + name + "\x00")
	}
	return out.Bytes(), nil
}
