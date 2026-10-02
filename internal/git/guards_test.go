package git_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/testsupport"
)

// The guards git's primitives keep (the test plan's step 2, items 15 to
// 17).

// A ref transaction whose git is killed, or whose context ends, can't be
// told to have happened or not: the error says it's uncertain, for the
// caller to read the refs back, rather than a plain failure.
func TestARefUpdateThatDiesIsUncertain(t *testing.T) {
	t.Parallel()
	repo, head := portsCheckout(t)
	real, err := exec.LookPath("git")
	require.NoError(t, err)
	// git, but update-ref ends as if killed, after what it was given.
	fake := filepath.Join(t.TempDir(), "git")
	testsupport.WriteExecutable(t, fake, "#!/bin/sh\nfor a do case \"$a\" in update-ref) cat >/dev/null; kill -9 $$;; esac; done\nexec "+real+" \"$@\"\n")
	dying, err := git.Open(t.Context(), repo.Root, fake)
	require.NoError(t, err)
	change := []git.RefChange{{Name: "refs/heads/feature", Desired: git.RefValue{Exists: true, Object: head}}}
	err = dying.UpdateRefs(t.Context(), change)
	require.ErrorIs(t, err, git.ErrRefUpdateUncertain)

	ended, cancel := context.WithCancel(t.Context())
	cancel()
	err = repo.UpdateRefs(ended, change)
	require.ErrorIs(t, err, git.ErrRefUpdateUncertain)
	value, err := repo.ReadRef(t.Context(), "refs/heads/feature")
	require.NoError(t, err)
	require.False(t, value.Exists, "an update whose context ended before it ran made nothing")

	require.NoError(t, repo.UpdateRefs(t.Context(), change), "the same change, given a live git")
	err = repo.UpdateRefs(t.Context(), change)
	var conflict *git.RefConflict
	require.ErrorAs(t, err, &conflict, "a ref that isn't as expected is a conflict, not uncertain")
	require.NotErrorIs(t, err, git.ErrRefUpdateUncertain)
}

// A working file that appeared, or went, since its edit was prepared
// stops every edit, as one that changed does.
func TestWorkingFilesThatAppearedOrWentStopTheEdits(t *testing.T) {
	t.Parallel()
	repo, _ := portsCheckout(t)
	_, tree, err := repo.WorkingTree(t.Context())
	require.NoError(t, err)
	jq, _, err := repo.File(t.Context(), tree, "textproc/jq/Portfile")
	require.NoError(t, err)

	created := []git.FileEdit{{Path: "devel/newport/Portfile", Before: git.FileState{}, After: []byte("name newport\n"), Mode: 0o100644}}
	require.NoError(t, os.MkdirAll(filepath.Join(repo.Root, "devel/newport"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(repo.Root, "devel/newport/Portfile"), []byte("someone's\n"), 0o644))
	require.ErrorIs(t, repo.ApplyToWorkingFiles(t.Context(), created), git.ErrWorkingFile, "it appeared")
	data, err := os.ReadFile(filepath.Join(repo.Root, "devel/newport/Portfile"))
	require.NoError(t, err)
	require.Equal(t, "someone's\n", string(data))

	edited := []git.FileEdit{{Path: "textproc/jq/Portfile", Before: jq, After: []byte("name jq\nversion 2\n"), Mode: jq.Mode}}
	require.NoError(t, os.Remove(filepath.Join(repo.Root, "textproc/jq/Portfile")))
	require.ErrorIs(t, repo.ApplyToWorkingFiles(t.Context(), edited), git.ErrWorkingFile, "it went")
	require.NoFileExists(t, filepath.Join(repo.Root, "textproc/jq/Portfile"))
}

// Writing working files deletes one, creates one in a directory that
// isn't there, and sets and clears the executable bit, as the edits say.
func TestWorkingFilesAreWrittenAsTheEditsSay(t *testing.T) {
	t.Parallel()
	repo, _ := portsCheckout(t)
	_, tree, err := repo.WorkingTree(t.Context())
	require.NoError(t, err)
	jq, _, err := repo.File(t.Context(), tree, "textproc/jq/Portfile")
	require.NoError(t, err)
	harbor, _, err := repo.File(t.Context(), tree, "devel/libharbor/Portfile")
	require.NoError(t, err)
	edits := []git.FileEdit{
		{Path: "devel/libharbor/Portfile", Before: harbor, Delete: true},
		{Path: "net/brandnew/files/hook.sh", Before: git.FileState{}, After: []byte("#!/bin/sh\n"), Mode: 0o100755},
		{Path: "textproc/jq/Portfile", Before: jq, After: []byte("name jq\n"), Mode: 0o100755},
	}
	require.NoError(t, repo.ApplyToWorkingFiles(t.Context(), edits))
	require.NoFileExists(t, filepath.Join(repo.Root, "devel/libharbor/Portfile"))
	info, err := os.Stat(filepath.Join(repo.Root, "net/brandnew/files/hook.sh"))
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o755), info.Mode().Perm())
	info, err = os.Stat(filepath.Join(repo.Root, "textproc/jq/Portfile"))
	require.NoError(t, err)
	require.NotZero(t, info.Mode().Perm()&0o111, "set")

	_, tree, err = repo.WorkingTree(t.Context())
	require.NoError(t, err)
	jq, _, err = repo.File(t.Context(), tree, "textproc/jq/Portfile")
	require.NoError(t, err)
	require.NoError(t, repo.ApplyToWorkingFiles(t.Context(), []git.FileEdit{{Path: "textproc/jq/Portfile", Before: jq, After: []byte("name jq\n"), Mode: 0o100644}}))
	info, err = os.Stat(filepath.Join(repo.Root, "textproc/jq/Portfile"))
	require.NoError(t, err)
	require.Zero(t, info.Mode().Perm()&0o111, "cleared")
}

// CommitBefore walks first parents to the newest commit at or before an
// instant, start included, and finds none before the first; CommitTime is
// a commit's committer time. The port index mirror reads both (the test
// plan's step 3).
func TestCommitBeforeAndCommitTime(t *testing.T) {
	t.Parallel()
	repo, _ := portsCheckout(t)
	// Committed at a time given, which testsupport.Git's environment
	// leaves out.
	commitWhen := func(when, message string) string {
		t.Helper()
		cmd := exec.Command("git", "-c", "user.name=Test", "-c", "user.email=test@example.org", "-c", "commit.gpgSign=false", "commit", "-q", "--allow-empty", "-m", message)
		cmd.Dir = repo.Root
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_DATE="+when, "GIT_COMMITTER_DATE="+when)
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "%s", out)
		return testsupport.Git(t, repo.Root, "rev-parse", "HEAD")
	}
	at := func(s string) time.Time {
		parsed, err := time.Parse(time.RFC3339, s)
		require.NoError(t, err)
		return parsed
	}
	first := commitWhen("2026-01-01T00:00:00Z", "first")
	second := commitWhen("2026-02-01T00:00:00Z", "second")
	third := commitWhen("2026-03-01T00:00:00Z", "third")

	when, err := repo.CommitTime(t.Context(), second)
	require.NoError(t, err)
	require.Equal(t, at("2026-02-01T00:00:00Z"), when)

	for instant, want := range map[string]string{
		"2026-03-15T00:00:00Z": third,
		"2026-03-01T00:00:00Z": third, // at, not only before
		"2026-02-15T00:00:00Z": second,
		"2026-01-01T00:00:01Z": first,
		"2025-12-01T00:00:00Z": "", // the checkout's first commit is newer than all of these
	} {
		found, err := repo.CommitBefore(t.Context(), third, at(instant))
		require.NoError(t, err)
		require.Equal(t, want, found, instant)
	}
	_, err = repo.CommitBefore(t.Context(), "HEAD", at("2026-03-15T00:00:00Z"))
	require.ErrorContains(t, err, "literal commit objects are required")
	_, err = repo.CommitTime(t.Context(), "HEAD")
	require.ErrorContains(t, err, "literal commit objects are required")
}
