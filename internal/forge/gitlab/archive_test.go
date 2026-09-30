package gitlab_test

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/forge"
	forgegitlab "github.com/herbygillot/dockhand/internal/forge/gitlab"
)

// A commit's archive is GitLab's tar.gz of it, streamed and bounded; a
// commit GitLab hasn't is not found, and one past the bound is refused.
func TestArchiveStreamsTheCommitsTarball(t *testing.T) {
	t.Parallel()
	commit := strings.Repeat("b", 40)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/repository/archive.tar.gz") && r.URL.Query().Get("sha") == commit:
			fmt.Fprint(w, "the tarball")
		default:
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprint(w, `{"message":"404 Not Found"}`)
		}
	}))
	defer server.Close()
	repository, err := (&forgegitlab.Client{HTTP: server.Client()}).Repository(server.URL, "group/project")
	require.NoError(t, err)
	archives, ok := repository.(forge.ArchiveRepository)
	require.True(t, ok)
	var into bytes.Buffer
	require.NoError(t, archives.Archive(t.Context(), commit, &into, 1<<20))
	require.Equal(t, "the tarball", into.String())
	require.ErrorIs(t, archives.Archive(t.Context(), strings.Repeat("c", 40), &bytes.Buffer{}, 1<<20), forge.ErrNotFound)
	require.ErrorContains(t, archives.Archive(t.Context(), commit, &bytes.Buffer{}, 4), "exceeds 4 bytes")
	require.Error(t, archives.Archive(t.Context(), "main", &bytes.Buffer{}, 1<<20), "a commit, not a ref")
}
