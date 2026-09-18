package github_test

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/herbygillot/dockhand/internal/forge"
	"github.com/herbygillot/dockhand/internal/forge/github"
	githubapi "github.com/herbygillot/dockhand/internal/github"
	"github.com/stretchr/testify/require"
)

// A file is read at an exact commit through the contents API; an absent
// path is not found, a directory is not a file, and the bound holds.
func TestFileReadsOneFileAtACommit(t *testing.T) {
	t.Parallel()
	commit := strings.Repeat("a", 40)
	manifest := "module example.com/project\ngo 1.25\n"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, commit, r.URL.Query().Get("ref"))
		switch r.URL.Path {
		case "/api/repos/owner/project/contents/go.mod":
			fmt.Fprintf(w, `{"type":"file","encoding":"base64","size":%d,"name":"go.mod","path":"go.mod","content":%q}`, len(manifest), base64.StdEncoding.EncodeToString([]byte(manifest)))
		case "/api/repos/owner/project/contents/cmd":
			fmt.Fprint(w, `[{"type":"file","name":"main.go","path":"cmd/main.go"}]`)
		default:
			w.WriteHeader(404)
			fmt.Fprint(w, `{"message":"Not Found"}`)
		}
	}))
	defer server.Close()
	client := github.Client{Client: &githubapi.Client{Config: githubapi.Config{BaseURL: server.URL + "/api"}}}
	repository, err := client.Repository("https://github.com", "owner/project")
	require.NoError(t, err)
	files, ok := repository.(forge.FileRepository)
	require.True(t, ok)
	data, err := files.File(t.Context(), commit, "go.mod", 1<<20)
	require.NoError(t, err)
	require.Equal(t, manifest, string(data))
	_, err = files.File(t.Context(), commit, "missing.mod", 1<<20)
	require.ErrorIs(t, err, forge.ErrNotFound)
	_, err = files.File(t.Context(), commit, "cmd", 1<<20)
	require.ErrorIs(t, err, forge.ErrNotFound)
	_, err = files.File(t.Context(), commit, "go.mod", 4)
	require.Error(t, err, "the bound holds")
	for _, bad := range []struct{ commit, path string }{{"v1.2.3", "go.mod"}, {commit, "../go.mod"}, {commit, "/go.mod"}} {
		_, err = files.File(t.Context(), bad.commit, bad.path, 1<<20)
		require.Error(t, err, "%+v", bad)
	}
}
