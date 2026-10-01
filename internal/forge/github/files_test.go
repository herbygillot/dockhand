package github_test

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
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
		require.Equal(t, "application/vnd.github.object+json", r.Header.Get("Accept"))
		switch r.URL.Path {
		case "/api/repos/owner/project/contents/go.mod":
			fmt.Fprintf(w, `{"type":"file","encoding":"base64","size":%d,"name":"go.mod","path":"go.mod","content":%q}`, len(manifest), base64.StdEncoding.EncodeToString([]byte(manifest)))
		case "/api/repos/owner/project/contents/cmd":
			fmt.Fprint(w, `{"type":"dir","name":"cmd","path":"cmd","entries":[{"type":"file","name":"main.go","path":"cmd/main.go"}]}`)
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

// A file past the megabyte the contents API gives inline, as a large
// package.json is, is read raw, as GitHub documents for files up to 100
// MB, up to the caller's limit, which is the one that applies, in its own
// words; it failed with "unsupported content encoding: none" before.
func TestAFileLargerThanTheAPIGivesInlineIsReadRaw(t *testing.T) {
	t.Parallel()
	commit := strings.Repeat("b", 40)
	large := `{"name": "project", "description": "` + strings.Repeat("x", 2<<20) + `"}`
	var raw atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/api/repos/owner/project/contents/package.json", r.URL.Path)
		require.Equal(t, commit, r.URL.Query().Get("ref"))
		switch r.Header.Get("Accept") {
		case "application/vnd.github.object+json":
			fmt.Fprintf(w, `{"type":"file","encoding":"none","size":%d,"name":"package.json","path":"package.json","content":""}`, len(large))
		case "application/vnd.github.raw+json":
			raw.Add(1)
			fmt.Fprint(w, large)
		default:
			t.Errorf("asked for %q", r.Header.Get("Accept"))
		}
	}))
	defer server.Close()
	client := github.Client{Client: &githubapi.Client{Config: githubapi.Config{BaseURL: server.URL + "/api"}}}
	repository, err := client.Repository("https://github.com", "owner/project")
	require.NoError(t, err)
	files := repository.(forge.FileRepository)
	data, err := files.File(t.Context(), commit, "package.json", 4<<20)
	require.NoError(t, err)
	require.Equal(t, large, string(data))
	require.EqualValues(t, 1, raw.Load())

	_, err = files.File(t.Context(), commit, "package.json", 1<<20)
	require.EqualError(t, err, "github: package.json is larger than the 1024 KiB dockhand reads of it")
	require.EqualValues(t, 1, raw.Load(), "a file the API says is too large isn't fetched")
}
