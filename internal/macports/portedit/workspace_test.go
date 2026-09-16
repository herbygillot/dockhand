package portedit

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/portfile"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/stretchr/testify/require"
)

func TestWorkspaceRestoresContentsAfterTheCallbackEvenOnFailure(t *testing.T) {
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
	input := &sourceInput{files: &workspace{root: "/snapshot"}, target: record.Target{Portfile: "devel/fixture/Portfile"}}
	require.Equal(t, filepath.Join("/snapshot", "devel", "fixture", "Portfile"), input.portfile())
	require.Equal(t, filepath.Join("/snapshot", "devel", "fixture"), input.portdir())
	other := *input
	other.target = record.Target{Portfile: "devel/sibling/Portfile"}
	require.Equal(t, filepath.Join("/snapshot", "devel", "sibling"), other.portdir(), "a copy with another target resolves its own paths")
}

func TestArchiveStoreKeepsBytesOnlyWithADirectory(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/missing" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = w.Write([]byte("archive bytes"))
	}))
	defer server.Close()
	service := &Service{HTTP: server.Client()}
	info := macports.PortInfo{Options: map[string]string{}}
	hashed, err := service.archives("").fetch(t.Context(), info, archiveSource{Name: "a.tar.gz", URL: server.URL + "/a.tar.gz"})
	require.NoError(t, err)
	require.Empty(t, hashed.path, "without a directory only the hashes are kept")
	require.Equal(t, int64(len("archive bytes")), hashed.Size)

	directory := t.TempDir()
	kept, err := service.archives(directory).fetch(t.Context(), info, archiveSource{Name: "a.tar.gz", URL: server.URL + "/a.tar.gz"})
	require.NoError(t, err)
	data, err := os.ReadFile(kept.path)
	require.NoError(t, err)
	require.Equal(t, "archive bytes", string(data))
	require.Equal(t, hashed.SHA256, kept.SHA256)

	_, err = service.archives(directory).fetch(t.Context(), info, archiveSource{Name: "b.tar.gz", URL: server.URL + "/missing"})
	require.Error(t, err)
	entries, err := os.ReadDir(directory)
	require.NoError(t, err)
	require.Len(t, entries, 1, "a failed download leaves no partial file")

	first, err := service.archives("").fetchFirst(t.Context(), info, "c.tar.gz", []string{server.URL + "/missing", server.URL + "/c.tar.gz"})
	require.NoError(t, err)
	require.Equal(t, server.URL+"/c.tar.gz", first.URL, "the first working location wins")
	_, err = service.archives("").fetchFirst(t.Context(), info, "d.tar.gz", []string{server.URL + "/missing"})
	require.ErrorContains(t, err, "404")
}

func TestCommitEditRecordsFilesAndFidelityBeforeJudging(t *testing.T) {
	input := &sourceInput{target: record.Target{Name: "fixture", Portfile: "devel/fixture/Portfile"}}
	request := Request{Reason: "because"}
	edit := portfile.Edit{Path: "devel/fixture/Portfile", After: []byte("new")}
	var result Result
	require.NoError(t, result.commitEdit(input, request, edit, Fidelity{ExpectedChanges: []string{"fixture.revision +1"}}, "revbump"))
	require.Equal(t, []portfile.Edit{edit}, result.Files)
	require.Len(t, result.Fidelity, 1)
	require.Equal(t, []CommitIntent{{Subject: "fixture: revbump", Body: "because", Paths: []string{"devel/fixture/Portfile"}}}, result.Commits)

	var failed Result
	err := failed.commitEdit(input, request, edit, Fidelity{UnexpectedChanges: []string{"sibling.version changed"}}, "revbump")
	require.ErrorIs(t, err, ErrFidelity)
	require.Equal(t, []portfile.Edit{edit}, failed.Files, "the attempted edit is reported")
	require.Len(t, failed.Fidelity, 1)
	require.Nil(t, failed.Commits, "no commit is intended")
}
