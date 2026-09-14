package github_test

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/herbygillot/dockhand/internal/forge"
	"github.com/herbygillot/dockhand/internal/forge/github"
	"github.com/stretchr/testify/require"
)

type githubRepository interface {
	forge.Repository
	forge.ReleaseRepository
}

func testRepository(t *testing.T, client *github.Client) githubRepository {
	t.Helper()
	repository, err := client.Repository("https://github.com", "owner/project")
	require.NoError(t, err)
	result, ok := repository.(githubRepository)
	require.True(t, ok)
	return result
}

func TestRepositoryBindingValidatesNamesWithoutContactingGitHub(t *testing.T) {
	client := &github.Client{HTTP: &http.Client{Transport: rejectTransport{t}}}
	for _, name := range []string{"owner/project", "Owner-1/project.name", "owner/project_name"} {
		repository, err := client.Repository("https://github.com", name)
		require.NoError(t, err)
		require.Equal(t, name, repository.Name())
	}
	for _, name := range []string{"", "owner", "owner/", "/project", "owner/project/more", "../project", "owner/..", "owner/proj%2fect", "owner/project?x=y", "owner/project#ref", "owner/project name", "https://github.com/owner/project"} {
		repository, err := client.Repository("https://github.com", name)
		require.Error(t, err, name)
		require.Nil(t, repository)
	}
	repository, err := client.Repository("https://example.invalid", "owner/project")
	require.Error(t, err)
	require.Nil(t, repository)
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
		fmt.Fprintf(w, `{"ref":"refs/tags/v2","object":{"type":"commit","sha":%q}}`, strings.Repeat("a", 40))
	}))
	defer server.Close()
	client := &github.Client{Config: github.Config{BaseURL: server.URL}}
	one, err := client.Repository("https://github.com", "owner/one")
	require.NoError(t, err)
	two, err := client.Repository("https://github.com", "owner/two")
	require.NoError(t, err)
	for _, repository := range []forge.Repository{one, two, one} {
		tag, err := repository.Tag(t.Context(), "v2")
		require.NoError(t, err)
		require.Equal(t, "v2", tag.Name)
	}
	require.Equal(t, []string{"/repos/owner/one/git/ref/tags/v2", "/repos/owner/two/git/ref/tags/v2", "/repos/owner/one/git/ref/tags/v2"}, []string{<-paths, <-paths, <-paths})
}
