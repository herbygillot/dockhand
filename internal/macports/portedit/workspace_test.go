package portedit

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/herbygillot/dockhand/internal/macports/portfile"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/stretchr/testify/require"
)

func TestWorkspaceRestoresContentsAfterTheCallbackEvenOnFailure(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	relative := "devel/fixture/Portfile"
	require.NoError(t, os.MkdirAll(filepath.Dir(filepath.Join(root, relative)), 0700))
	require.NoError(t, os.WriteFile(filepath.Join(root, relative), []byte("original"), 0600))
	files := &workspace{root: root}
	require.Equal(t, filepath.Join(root, "devel", "fixture", "Portfile"), files.path(relative))
	boom := errors.New("boom")
	err := files.withContents(relative, []byte("candidate"), func() error {
		data, err := os.ReadFile(files.path(relative))
		require.NoError(t, err)
		require.Equal(t, "candidate", string(data), "the callback sees the candidate")
		return boom
	})
	require.ErrorIs(t, err, boom)
	data, err := os.ReadFile(files.path(relative))
	require.NoError(t, err)
	require.Equal(t, "original", string(data), "the original is restored after a failing callback")
	require.Error(t, files.withContents("devel/missing/Portfile", nil, func() error { t.Fatal("must not run"); return nil }))
}

func TestSourceInputPathsFollowTheSelectedTarget(t *testing.T) {
	t.Parallel()
	input := &sourceInput{files: &workspace{root: "/snapshot"}, target: record.Target{Portfile: "devel/fixture/Portfile"}}
	require.Equal(t, filepath.Join("/snapshot", "devel", "fixture", "Portfile"), input.portfile())
	require.Equal(t, filepath.Join("/snapshot", "devel", "fixture"), input.portdir())
	other := *input
	other.target = record.Target{Portfile: "devel/sibling/Portfile"}
	require.Equal(t, filepath.Join("/snapshot", "devel", "sibling"), other.portdir(), "a copy with another target resolves its own paths")
}

func TestCommitEditRecordsFilesAndFidelityBeforeJudging(t *testing.T) {
	t.Parallel()
	input := &sourceInput{target: record.Target{Name: "fixture", Portfile: "devel/fixture/Portfile"}}
	request := Request{Subject: "because"}
	edit := portfile.Edit{Path: "devel/fixture/Portfile", After: []byte("new")}
	var result Result
	require.NoError(t, result.commitEdit(input, request, edit, Fidelity{ExpectedChanges: []string{"fixture.revision +1"}}, "revbump"))
	require.Equal(t, []portfile.Edit{edit}, result.Files)
	require.Len(t, result.Fidelity, 1)
	require.Equal(t, []CommitIntent{{Subject: "fixture: because", Paths: []string{"devel/fixture/Portfile"}}}, result.Commits)

	var failed Result
	err := failed.commitEdit(input, request, edit, Fidelity{UnexpectedChanges: []string{"sibling.version changed"}}, "revbump")
	require.ErrorIs(t, err, ErrFidelity)
	require.Equal(t, []portfile.Edit{edit}, failed.Files, "the attempted edit is reported")
	require.Len(t, failed.Fidelity, 1)
	require.Nil(t, failed.Commits, "no commit is intended")
}
