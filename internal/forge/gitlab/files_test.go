package gitlab_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/herbygillot/dockhand/internal/forge"
	forgegitlab "github.com/herbygillot/dockhand/internal/forge/gitlab"
	"github.com/stretchr/testify/require"
)

// A file is read at an exact commit through the raw file endpoint, with the
// project path encoded; an absent path is not found, and the bound holds.
func TestFileReadsOneRawFileAtACommit(t *testing.T) {
	t.Parallel()
	commit := strings.Repeat("b", 40)
	manifest := "module example.com/project\ngo 1.24\n"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, commit, r.URL.Query().Get("ref"))
		switch r.URL.Path {
		case "/api/v4/projects/group%2Fproject/repository/files/go.mod/raw", "/api/v4/projects/group/project/repository/files/go.mod/raw":
			fmt.Fprint(w, manifest)
		default:
			w.WriteHeader(404)
			fmt.Fprint(w, `{"message":"404 File Not Found"}`)
		}
	}))
	defer server.Close()
	repository, err := (&forgegitlab.Client{HTTP: server.Client()}).Repository(server.URL, "group/project")
	require.NoError(t, err)
	files, ok := repository.(forge.FileRepository)
	require.True(t, ok)
	data, err := files.File(t.Context(), commit, "go.mod", 1<<20)
	require.NoError(t, err)
	require.Equal(t, manifest, string(data))
	_, err = files.File(t.Context(), commit, "missing.mod", 1<<20)
	require.ErrorIs(t, err, forge.ErrNotFound)
	_, err = files.File(t.Context(), commit, "go.mod", 4)
	require.Error(t, err, "the bound holds")
	_, err = files.File(t.Context(), "main", "go.mod", 1<<20)
	require.Error(t, err, "a commit is required")
}
