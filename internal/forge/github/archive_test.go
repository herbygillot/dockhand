package github_test

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/forge"
	"github.com/herbygillot/dockhand/internal/forge/github"
	githubapi "github.com/herbygillot/dockhand/internal/github"
)

// A commit's archive is where GitHub's archive link sends it, fetched and
// bounded; a commit GitHub hasn't is not found, and one past the bound is
// refused.
func TestArchiveFetchesTheCommitsTarball(t *testing.T) {
	t.Parallel()
	commit := strings.Repeat("a", 40)
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/repos/owner/project/tarball/" + commit:
			http.Redirect(w, r, server.URL+"/codeload/owner/project/tar.gz/"+commit, http.StatusFound)
		case "/codeload/owner/project/tar.gz/" + commit:
			fmt.Fprint(w, "the tarball")
		default:
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprint(w, `{"message":"Not Found"}`)
		}
	}))
	defer server.Close()
	client := github.Client{Client: &githubapi.Client{Config: githubapi.Config{BaseURL: server.URL + "/api"}}}
	repository, err := client.Repository("https://github.com", "owner/project")
	require.NoError(t, err)
	archives, ok := repository.(forge.ArchiveRepository)
	require.True(t, ok)
	var into bytes.Buffer
	require.NoError(t, archives.Archive(t.Context(), commit, &into, 1<<20))
	require.Equal(t, "the tarball", into.String())
	require.ErrorIs(t, archives.Archive(t.Context(), strings.Repeat("b", 40), &bytes.Buffer{}, 1<<20), forge.ErrNotFound)
	require.Error(t, archives.Archive(t.Context(), commit, &bytes.Buffer{}, 4), "past the bound")
	require.Error(t, archives.Archive(t.Context(), "main", &bytes.Buffer{}, 1<<20), "a commit, not a ref")
}
