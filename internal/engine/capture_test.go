package engine

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/testsupport"
)

func TestCaptureNumbersSnapshotsAndReusesUnchangedOnes(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e := f.open(t)
	branch, err := e.Start(t.Context(), StartRequest{Name: "jq-update", Here: true})
	require.NoError(t, err)
	dir := branch.Worktree

	clean, err := e.Capture(t.Context(), CaptureRequest{Branch: branch})
	require.NoError(t, err)
	require.Equal(t, model.RevisionCommit, clean.Revision.Kind, "files exactly at the commit are the commit")
	require.Equal(t, "commit "+short(branch.Base), Describe(clean.Revision))

	write(t, dir, map[string]string{"textproc/jq/Portfile": "name jq\nversion 1.8.1\n", "notes.txt": "mine\n"})
	first, err := e.Capture(t.Context(), CaptureRequest{Branch: branch})
	require.NoError(t, err)
	require.Equal(t, "snapshot 1", Describe(first.Revision))
	require.Equal(t, []string{"notes.txt"}, first.Untracked, "untracked files are listed and left out")
	require.Empty(t, first.NewPorts)
	again, err := e.Capture(t.Context(), CaptureRequest{Branch: branch})
	require.NoError(t, err)
	require.True(t, again.Reused)
	require.Equal(t, first.Revision.ID, again.Revision.ID)

	head, err := e.Capture(t.Context(), CaptureRequest{Branch: branch, Mode: CaptureHead})
	require.NoError(t, err)
	require.Equal(t, clean.Revision.ID, head.Revision.ID)
	staged, err := e.Capture(t.Context(), CaptureRequest{Branch: branch, Mode: CaptureStaged})
	require.NoError(t, err)
	require.Equal(t, clean.Revision.ID, staged.Revision.ID, "nothing is staged")

	included, err := e.Capture(t.Context(), CaptureRequest{Branch: branch, Include: []string{"notes.txt"}})
	require.NoError(t, err)
	require.Equal(t, "snapshot 2", Describe(included.Revision))
	blobs, err := e.Repo.FileBlobs(t.Context(), string(included.Revision.Source.Tree), []string{"notes.txt"})
	require.NoError(t, err)
	require.Contains(t, blobs, "notes.txt")
	_, err = e.Capture(t.Context(), CaptureRequest{Branch: branch, Include: []string{"missing.txt"}})
	require.ErrorContains(t, err, "not an untracked file here")

	testsupport.Git(t, dir, "add", "textproc/jq/Portfile")
	write(t, dir, map[string]string{"textproc/jq/Portfile": "name jq\nversion 1.9\n"})
	staged, err = e.Capture(t.Context(), CaptureRequest{Branch: branch, Mode: CaptureStaged})
	require.NoError(t, err)
	require.Equal(t, first.Revision.Source.Tree, staged.Revision.Source.Tree, "the index holds 1.8.1")
	require.Equal(t, first.Revision.ID, staged.Revision.ID)
}

// A capture stands only if the files didn't move while it read them,
// the files --include adds among them.
func TestACaptureOfFilesThatMovedIsRefused(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e := f.open(t)
	branch, err := e.Start(t.Context(), StartRequest{Name: "jq-update", Here: true})
	require.NoError(t, err)
	dir := branch.Worktree
	write(t, dir, map[string]string{"textproc/jq/Portfile": "name jq\nversion 1.8.1\n", "notes.txt": "mine\n"})

	for _, test := range []struct {
		name    string
		include []string
		moved   map[string]string
	}{
		{"a tracked file", nil, map[string]string{"textproc/jq/Portfile": "name jq\nversion 1.8.2\n"}},
		{"an included file", []string{"notes.txt"}, map[string]string{"notes.txt": "edited\n"}},
		{"a tracked file, beside an included one", []string{"notes.txt"}, map[string]string{"textproc/jq/Portfile": "name jq\nversion 1.8.3\n"}},
	} {
		e.betweenReads = func() { write(t, dir, test.moved) }
		_, err := e.Capture(t.Context(), CaptureRequest{Branch: branch, Include: test.include})
		require.EqualError(t, err, "the files changed while they were read; check again once they settle", test.name)
	}
	e.betweenReads = nil
	_, err = e.Capture(t.Context(), CaptureRequest{Branch: branch, Include: []string{"notes.txt"}})
	require.NoError(t, err, "files at rest are captured")
}

// A Portfile written by hand in a new port's directory is a new port
// left out, which check names with the commands that take it (the Vx
// port's field testing, 2026-10-03).
func TestAHandWrittenPortIsANewPortLeftOut(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e := f.open(t)
	branch, err := e.Start(t.Context(), StartRequest{Name: "py-pyaml"})
	require.NoError(t, err)
	write(t, branch.Worktree, map[string]string{"python/py-pyaml/Portfile": "name py-pyaml\nversion 1\n", "python/py-pyaml/notes.txt": "mine\n",
		"python/py-pyaml/.Portfile.swp": "swap\n", "python/py-pyaml/Portfile~": "backup\n"})
	capture, err := e.Capture(t.Context(), CaptureRequest{Branch: branch})
	require.NoError(t, err)
	require.Equal(t, []string{"python/py-pyaml"}, capture.NewPorts)
	require.Equal(t, []string{"python/py-pyaml/Portfile", "python/py-pyaml/notes.txt"}, capture.Untracked, "an editor's swap and backup files aren't listed as left out")
}

// What gathers untracked files abides by what Git ignores, as git add
// does: a .gitignore at the tree's root and one in a port's directory,
// and .git/info/exclude. A capture leaves the ignored files out, and
// doesn't list them as left out; --include refuses one, saying why; and
// a branch's worktree with only ignored files isn't dirty for clean (the
// person, 2026-10-04).
func TestWhatGitIgnoresIsLeftOutOfWhatACheckGathers(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e := f.open(t)
	branch, err := e.Start(t.Context(), StartRequest{Name: "jq"})
	require.NoError(t, err)
	write(t, branch.Worktree, map[string]string{".gitignore": "*.swp\n", "textproc/jq/.gitignore": "scratch/\n"})
	testsupport.Git(t, branch.Worktree, "add", "--sparse", ".gitignore", "textproc/jq/.gitignore")
	testsupport.Git(t, branch.Worktree, "commit", "-q", "-m", "jq: ignore an editor's files")
	common := testsupport.Git(t, branch.Worktree, "rev-parse", "--path-format=absolute", "--git-common-dir")
	require.NoError(t, os.MkdirAll(filepath.Join(common, "info"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(common, "info", "exclude"), []byte("local.txt\n"), 0o644))
	write(t, branch.Worktree, map[string]string{"textproc/jq/.Portfile.swp": "swap\n", "textproc/jq/scratch/out.txt": "out\n", "textproc/jq/local.txt": "mine\n"})

	capture, err := e.Capture(t.Context(), CaptureRequest{Branch: branch})
	require.NoError(t, err)
	require.Empty(t, capture.Untracked, "every one is ignored")
	reason, err := e.dirty(t.Context(), branch.Worktree)
	require.NoError(t, err)
	require.Empty(t, reason, "ignored files make no worktree dirty")
	for _, path := range []string{"textproc/jq/.Portfile.swp", "textproc/jq/scratch/out.txt", "textproc/jq/local.txt"} {
		_, err = e.Capture(t.Context(), CaptureRequest{Branch: branch, Include: []string{path}})
		require.ErrorContains(t, err, "--include "+path+": Git ignores it, so a check leaves it out as git add would; git check-ignore -v "+path+" names the rule")
	}

	write(t, branch.Worktree, map[string]string{"textproc/jq/files/fix.patch": "fix\n"})
	capture, err = e.Capture(t.Context(), CaptureRequest{Branch: branch})
	require.NoError(t, err)
	require.Equal(t, []string{"textproc/jq/files/fix.patch"}, capture.Untracked, "what isn't ignored is still left out, and said")
	capture, err = e.Capture(t.Context(), CaptureRequest{Branch: branch, Include: []string{"textproc/jq/files/fix.patch"}})
	require.NoError(t, err)
	file, _, err := e.Repo.File(t.Context(), string(capture.Revision.Source.Tree), "textproc/jq/files/fix.patch")
	require.NoError(t, err)
	require.True(t, file.Exists, "--include takes it")
	for _, path := range []string{"textproc/jq/.Portfile.swp", "textproc/jq/scratch/out.txt", "textproc/jq/local.txt"} {
		file, _, err := e.Repo.File(t.Context(), string(capture.Revision.Source.Tree), path)
		require.NoError(t, err)
		require.False(t, file.Exists, path)
	}
}
