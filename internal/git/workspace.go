package git

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ErrBranchExists reports a branch name already in use.
var ErrBranchExists = errors.New("git: branch already exists")

// CreateBranch creates refs/heads/<name> at commit, refusing a branch that
// already exists rather than moving it.
func (r *Repository) CreateBranch(ctx context.Context, name, commit string) error {
	if !ValidBranchName(name) || !ValidObjectID(commit) {
		return fmt.Errorf("git: invalid branch %q or commit %q", name, commit)
	}
	ref := "refs/heads/" + name
	current, err := r.ReadRef(ctx, ref)
	if err != nil {
		return err
	}
	if current.Exists {
		return fmt.Errorf("%w: %s", ErrBranchExists, name)
	}
	return r.UpdateRefs(ctx, []RefChange{{Name: ref, Desired: RefValue{Exists: true, Object: commit}}})
}

// DeleteBranch removes refs/heads/<name> only while it still points at
// commit, so a branch someone moved is never deleted.
func (r *Repository) DeleteBranch(ctx context.Context, name, commit string) error {
	if !ValidBranchName(name) || !ValidObjectID(commit) {
		return fmt.Errorf("git: invalid branch %q or commit %q", name, commit)
	}
	return r.UpdateRefs(ctx, []RefChange{{Name: "refs/heads/" + name, Expected: RefValue{Exists: true, Object: commit}}})
}

// AddSparseWorktree checks branch out into a new linked worktree at
// directory, holding only the cone paths given (and the files at the top of
// the tree, which cone mode always keeps). The sparse patterns belong to the
// new worktree alone; Git turns on extensions.worktreeConfig to keep them
// there. A directory that exists is refused. A worktree that fails partway
// is removed again.
func (r *Repository) AddSparseWorktree(ctx context.Context, directory, branch string, cone []string) (err error) {
	if !filepath.IsAbs(directory) || !ValidBranchName(branch) {
		return fmt.Errorf("git: worktree needs an absolute directory and a valid branch")
	}
	if _, statErr := os.Lstat(directory); statErr == nil {
		return fmt.Errorf("git: %s already exists", directory)
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return statErr
	}
	if err := os.MkdirAll(filepath.Dir(directory), 0o755); err != nil {
		return err
	}
	if _, err := r.output(ctx, "worktree", "add", "--quiet", "--no-checkout", "--", directory, branch); err != nil {
		return err
	}
	defer func() {
		if err != nil {
			cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
			defer cancel()
			err = errors.Join(err, r.RemoveWorktree(cleanup, directory))
		}
	}()
	worktree := &Repository{Root: directory, CommonDir: r.CommonDir, Executable: r.Executable}
	if _, err := worktree.output(ctx, append([]string{"sparse-checkout", "set", "--cone", "--"}, cone...)...); err != nil {
		return err
	}
	// A --no-checkout worktree has an empty index; reading HEAD's tree
	// through the sparse patterns populates exactly the cone.
	_, err = worktree.output(ctx, "read-tree", "-mu", "HEAD")
	return err
}

// ExpandSparse adds directories to the worktree's sparse cone and checks
// them out.
func (r *Repository) ExpandSparse(ctx context.Context, directories ...string) error {
	if len(directories) == 0 {
		return nil
	}
	_, err := r.output(ctx, append([]string{"sparse-checkout", "add", "--"}, directories...)...)
	return err
}

// SparseCone lists the worktree's sparse directories; empty when the
// checkout is not sparse.
func (r *Repository) SparseCone(ctx context.Context) ([]string, error) {
	enabled, err := r.output(ctx, "config", "--type=bool", "--default=false", "core.sparseCheckout")
	if err != nil || strings.TrimSpace(string(enabled)) != "true" {
		return nil, err
	}
	out, err := r.output(ctx, "sparse-checkout", "list")
	if err != nil {
		return nil, err
	}
	return strings.Fields(string(out)), nil
}

// RemoveWorktree removes a linked worktree and its administrative files,
// discarding its working files.
func (r *Repository) RemoveWorktree(ctx context.Context, directory string) error {
	_, err := r.output(ctx, "worktree", "remove", "--force", "--", directory)
	return err
}

// TrackedChanges lists tracked paths whose index or working contents differ
// from HEAD: the work a branch switch could displace. Untracked files are
// not listed.
func (r *Repository) TrackedChanges(ctx context.Context) ([]string, error) {
	out, err := r.output(ctx, "status", "--porcelain=v1", "-z", "--untracked-files=no", "--ignore-submodules=none")
	if err != nil {
		return nil, err
	}
	var paths []string
	fields := strings.Split(string(out), "\x00")
	for i := 0; i < len(fields); i++ {
		entry := fields[i]
		if len(entry) < 4 {
			continue
		}
		paths = append(paths, entry[3:])
		// A rename or copy is followed by its source path.
		if entry[0] == 'R' || entry[0] == 'C' {
			i++
		}
	}
	return paths, nil
}

// Switch checks out an existing local branch in this checkout. Git refuses
// when the switch would overwrite local changes, and so does this.
func (r *Repository) Switch(ctx context.Context, branch string) error {
	if !ValidBranchName(branch) {
		return fmt.Errorf("git: invalid branch %q", branch)
	}
	_, err := r.output(ctx, "switch", "--no-guess", "--quiet", branch)
	return err
}
