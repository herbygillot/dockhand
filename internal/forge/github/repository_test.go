package github_test

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/herbygillot/dockhand/v2/internal/forge"
	"github.com/herbygillot/dockhand/v2/internal/forge/github"
	"github.com/stretchr/testify/require"
)

func testRepository(t *testing.T, client *github.Client) forge.Repository {
	t.Helper()
	repository, err := client.Repository("owner/project")
	require.NoError(t, err)
	return repository
}

func TestRepositoryBindingValidatesNamesWithoutContactingGitHub(t *testing.T) {
	client := &github.Client{HTTP: &http.Client{Transport: rejectTransport{t}}}
	for _, name := range []string{"owner/project", "Owner-1/project.name", "owner/project_name"} {
		repository, err := client.Repository(name)
		require.NoError(t, err)
		require.Equal(t, name, repository.Name())
	}
	for _, name := range []string{"", "owner", "owner/", "/project", "owner/project/more", "../project", "owner/..", "owner/proj%2fect", "owner/project?x=y", "owner/project#ref", "owner/project name", "https://github.com/owner/project"} {
		repository, err := client.Repository(name)
		require.Error(t, err, name)
		require.Nil(t, repository)
	}
	repository := testRepository(t, client)
	require.Equal(t, "https://github.com/owner/project/tags", repository.TagsURL())
	require.Equal(t, "https://github.com/owner/project/archive/refs/tags/release/2.0.tar.gz", repository.TagArchiveURL("release/2.0"))
	archive, err := url.Parse(repository.TagArchiveURL("release/2#meta%"))
	require.NoError(t, err)
	require.Empty(t, archive.Fragment)
	require.Empty(t, archive.RawQuery)
	require.Equal(t, "/owner/project/archive/refs/tags/release/2#meta%.tar.gz", archive.Path)
}

type rejectTransport struct{ t *testing.T }

func (r rejectTransport) RoundTrip(*http.Request) (*http.Response, error) {
	r.t.Error("repository binding must not perform HTTP requests")
	return nil, errors.New("unexpected request")
}

func TestRepositoriesSharingAClientKeepTheirOwnRequestScope(t *testing.T) {
	paths := make(chan string, 3)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths <- r.URL.Path
		if r.Header.Get("Accept") != "application/vnd.github+json" || r.Header.Get("X-GitHub-Api-Version") != "2026-03-10" || r.Header.Get("User-Agent") != "dockhand/2" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		fmt.Fprintf(w, `{"ref":"refs/tags/v2","object":{"type":"commit","sha":%q}}`, strings.Repeat("a", 40))
	}))
	defer server.Close()
	client := &github.Client{Config: github.Config{BaseURL: server.URL}}
	one, err := client.Repository("owner/one")
	require.NoError(t, err)
	two, err := client.Repository("owner/two")
	require.NoError(t, err)
	for _, repository := range []forge.Repository{one, two, one} {
		tag, err := repository.Tag(t.Context(), "v2")
		require.NoError(t, err)
		require.Equal(t, "v2", tag.Name)
	}
	require.Equal(t, []string{"/repos/owner/one/git/ref/tags/v2", "/repos/owner/two/git/ref/tags/v2", "/repos/owner/one/git/ref/tags/v2"}, []string{<-paths, <-paths, <-paths})
}
