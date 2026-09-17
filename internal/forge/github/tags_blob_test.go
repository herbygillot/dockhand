package github_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/herbygillot/dockhand/internal/forge/github"
	githubapi "github.com/herbygillot/dockhand/internal/github"
	"github.com/stretchr/testify/require"
)

func TestListTagsSkipsTagsThatNameNoCommit(t *testing.T) {
	sha := strings.Repeat("a", 40)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/repos/git/git/tags") {
			http.NotFound(w, r)
			return
		}
		fmt.Fprintf(w, `[{"name":"v2.56.0-rc1","commit":{"sha":%q}},{"name":"junio-gpg-pub","commit":{"sha":""}},{"name":"bad..name","commit":{"sha":%q}}]`, sha, sha)
	}))
	defer server.Close()
	client := &github.Client{Client: &githubapi.Client{Config: githubapi.Config{BaseURL: server.URL}}}
	repository, err := client.Repository("https://github.com", "git/git")
	require.NoError(t, err)
	tags, err := repository.ListTags(t.Context())
	require.NoError(t, err)
	require.Len(t, tags, 1, "a key blob and an unusable ref name are not releases")
	require.Equal(t, "v2.56.0-rc1", tags[0].Name)
}
