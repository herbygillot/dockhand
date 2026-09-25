package engine

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/model"
)

func TestCaptureNumbersSnapshotsAndReusesUnchangedOnes(t *testing.T) {
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

	run(t, dir, "add", "textproc/jq/Portfile")
	write(t, dir, map[string]string{"textproc/jq/Portfile": "name jq\nversion 1.9\n"})
	staged, err = e.Capture(t.Context(), CaptureRequest{Branch: branch, Mode: CaptureStaged})
	require.NoError(t, err)
	require.Equal(t, first.Revision.Source.Tree, staged.Revision.Source.Tree, "the index holds 1.8.1")
	require.Equal(t, first.Revision.ID, staged.Revision.ID)
}
