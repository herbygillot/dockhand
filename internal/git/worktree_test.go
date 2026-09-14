package git_test

import (
	"bytes"
	"github.com/herbygillot/dockhand/internal/git"
	"github.com/stretchr/testify/require"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func workGit(t *testing.T, repo *git.Repository, args ...string) []byte {
	t.Helper()
	command := exec.CommandContext(t.Context(), "git", append([]string{"-c", "core.hooksPath=" + os.DevNull, "-c", "commit.gpgSign=false", "-c", "user.name=Fixture", "-c", "user.email=test@example.invalid"}, args...)...)
	command.Dir = repo.Root
	out, err := command.CombinedOutput()
	require.NoError(t, err, "%s", out)
	return out
}
func workFile(t *testing.T, repo *git.Repository, name, data string) {
	t.Helper()
	filename := filepath.Join(repo.Root, name)
	require.NoError(t, os.MkdirAll(filepath.Dir(filename), 0700))
	require.NoError(t, os.WriteFile(filename, []byte(data), 0600))
}
func TestCheckoutCapturesWorkingBytesAndPreservesIndexAndRefs(t *testing.T) {
	repo := snapshotRepo(t)
	for _, name := range []string{"port", "deleted", "staged-delete", "executable", "assumed"} {
		workFile(t, repo, name, "original\n")
	}
	workFile(t, repo, ".gitignore", "ignored\n")
	workFile(t, repo, ".gitattributes", "port filter=broken\n")
	workGit(t, repo, "add", ".")
	workGit(t, repo, "commit", "-qm", "fixture")
	workGit(t, repo, "config", "filter.broken.clean", "false")
	workGit(t, repo, "config", "filter.broken.required", "true")
	workFile(t, repo, "port", "staged\n")
	workGit(t, repo, "-c", "filter.broken.required=false", "add", "port")
	workFile(t, repo, "port", "working\r\n")
	workFile(t, repo, "new", "staged addition")
	workGit(t, repo, "add", "new")
	workFile(t, repo, "new", "edited addition")
	require.NoError(t, os.Remove(filepath.Join(repo.Root, "deleted")))
	workGit(t, repo, "rm", "staged-delete")
	require.NoError(t, os.Chmod(filepath.Join(repo.Root, "executable"), 0700))
	workGit(t, repo, "update-index", "--assume-unchanged", "assumed")
	workFile(t, repo, "assumed", "working despite flag")
	workFile(t, repo, "untracked", "excluded")
	workFile(t, repo, "ignored", "excluded")
	require.NoError(t, os.Symlink("port", filepath.Join(repo.Root, "link")))
	workGit(t, repo, "add", "link")
	index, err := os.ReadFile(filepath.Join(repo.CommonDir, "index"))
	require.NoError(t, err)
	head := workGit(t, repo, "rev-parse", "HEAD")
	captured, err := repo.CaptureCheckout(t.Context())
	require.NoError(t, err)
	require.Equal(t, 7, captured.ModifiedFiles)
	require.Equal(t, []string{"untracked"}, captured.Untracked)
	files, err := repo.Materialize(t.Context(), captured.Tree)
	require.NoError(t, err)
	defer files.Close()
	for name, want := range map[string]string{"port": "working\r\n", "new": "edited addition", "assumed": "working despite flag"} {
		data, err := os.ReadFile(filepath.Join(files.Root, name))
		require.NoError(t, err)
		require.Equal(t, want, string(data))
	}
	for _, name := range []string{"deleted", "staged-delete", "untracked", "ignored"} {
		require.NoFileExists(t, filepath.Join(files.Root, name))
	}
	link, err := os.Readlink(filepath.Join(files.Root, "link"))
	require.NoError(t, err)
	require.Equal(t, "port", link)
	info, err := os.Stat(filepath.Join(files.Root, "executable"))
	require.NoError(t, err)
	require.NotZero(t, info.Mode()&0111)
	after, err := os.ReadFile(filepath.Join(repo.CommonDir, "index"))
	require.NoError(t, err)
	require.Equal(t, index, after)
	require.Equal(t, head, workGit(t, repo, "rev-parse", "HEAD"))
	workFile(t, repo, "port", "later edit")
	data, err := os.ReadFile(filepath.Join(files.Root, "port"))
	require.NoError(t, err)
	require.Equal(t, "working\r\n", string(data))
}
func TestCheckoutHandlesDetachedAndLinkedWorktrees(t *testing.T) {
	repo := snapshotRepo(t)
	workFile(t, repo, "file", "initial")
	workGit(t, repo, "add", ".")
	workGit(t, repo, "commit", "-qm", "fixture")
	captured, err := repo.CaptureCheckout(t.Context())
	require.NoError(t, err)
	require.Zero(t, captured.ModifiedFiles)
	require.NotEmpty(t, captured.Branch)
	linked := filepath.Join(t.TempDir(), "linked")
	workGit(t, repo, "worktree", "add", "--detach", linked, "HEAD")
	other, err := git.Open(t.Context(), linked, "")
	require.NoError(t, err)
	workFile(t, other, "file", "linked edit")
	capture, err := other.CaptureCheckout(t.Context())
	require.NoError(t, err)
	require.Empty(t, capture.Branch)
	require.Equal(t, 1, capture.ModifiedFiles)
	original, err := repo.CaptureCheckout(t.Context())
	require.NoError(t, err)
	require.Equal(t, captured.Tree, original.Tree)
}
func TestCheckoutRejectsSparseAndConflictedIndexes(t *testing.T) {
	repo := snapshotRepo(t)
	workFile(t, repo, "file", "initial")
	workGit(t, repo, "add", ".")
	workGit(t, repo, "commit", "-qm", "fixture")
	workGit(t, repo, "update-index", "--skip-worktree", "file")
	_, err := repo.CaptureCheckout(t.Context())
	require.ErrorContains(t, err, "skip-worktree")
	workGit(t, repo, "update-index", "--no-skip-worktree", "file")
	workGit(t, repo, "checkout", "-qb", "other")
	workFile(t, repo, "file", "other")
	workGit(t, repo, "commit", "-qam", "other")
	workGit(t, repo, "checkout", "-qb", "diverged", "HEAD~1")
	workFile(t, repo, "file", "diverged")
	workGit(t, repo, "commit", "-qam", "diverged")
	command := exec.CommandContext(t.Context(), "git", "merge", "other")
	command.Dir = repo.Root
	require.Error(t, command.Run())
	_, err = repo.CaptureCheckout(t.Context())
	require.ErrorContains(t, err, "resolve conflicts")
}

func TestCheckoutRejectsEditsObservedDuringCapture(t *testing.T) {
	repo := snapshotRepo(t)
	workFile(t, repo, "a", "original")
	workFile(t, repo, "z", "original")
	workGit(t, repo, "add", ".")
	workGit(t, repo, "commit", "-qm", "fixture")
	workFile(t, repo, "z", "dirty")
	wrapper := filepath.Join(t.TempDir(), "git")
	require.NoError(t, os.WriteFile(wrapper, []byte("#!/bin/sh\nfor arg do\n if [ \"$arg\" = hash-object ]; then printf 'concurrent edit' > a; fi\ndone\nexec /usr/bin/git \"$@\"\n"), 0700))
	repo.Executable = wrapper
	_, err := repo.CaptureCheckout(t.Context())
	require.ErrorIs(t, err, git.ErrCheckoutChanged)
}

func TestCheckoutSupportsSHA256Objects(t *testing.T) {
	root := t.TempDir()
	command := exec.CommandContext(t.Context(), "git", "init", "--quiet", "--object-format=sha256", root)
	out, err := command.CombinedOutput()
	require.NoError(t, err, "%s", out)
	repo, err := git.Open(t.Context(), root, "")
	require.NoError(t, err)
	workFile(t, repo, "file", "original")
	workGit(t, repo, "add", ".")
	workGit(t, repo, "commit", "-qm", "fixture")
	workFile(t, repo, "file", "edited")
	capture, err := repo.CaptureCheckout(t.Context())
	require.NoError(t, err)
	require.Len(t, capture.Tree, 64)
	require.Equal(t, 1, capture.ModifiedFiles)
	files, err := repo.Materialize(t.Context(), capture.Tree)
	require.NoError(t, err)
	defer files.Close()
	data, err := os.ReadFile(filepath.Join(files.Root, "file"))
	require.NoError(t, err)
	require.Equal(t, "edited", string(data))
}

func TestCaptureRealPortsCheckout(t *testing.T) {
	directory := os.Getenv("DOCKHAND_TEST_PORTS_REPO")
	if directory == "" {
		t.Skip("set DOCKHAND_TEST_PORTS_REPO for a real checkout capture")
	}
	repo, err := git.Open(t.Context(), directory, "")
	require.NoError(t, err)
	indexPath := string(bytes.TrimSpace(workGit(t, repo, "rev-parse", "--git-path", "index")))
	if !filepath.IsAbs(indexPath) {
		indexPath = filepath.Join(repo.Root, indexPath)
	}
	before, err := os.ReadFile(indexPath)
	require.NoError(t, err)
	head := workGit(t, repo, "rev-parse", "HEAD")
	started := time.Now()
	capture, err := repo.CaptureCheckout(t.Context())
	require.NoError(t, err)
	t.Logf("capture duration=%s modified=%d head=%s tree=%s", time.Since(started), capture.ModifiedFiles, capture.Head, capture.Tree)
	after, err := os.ReadFile(indexPath)
	require.NoError(t, err)
	require.Equal(t, before, after)
	require.Equal(t, head, workGit(t, repo, "rev-parse", "HEAD"))
}
