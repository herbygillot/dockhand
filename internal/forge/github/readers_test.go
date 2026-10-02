package github_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/forge"
	"github.com/herbygillot/dockhand/internal/forge/github"
	githubapi "github.com/herbygillot/dockhand/internal/github"
)

// A commit's time is its committer's, as GitHub's Git API gives it; a
// commit GitHub hasn't is not found, and an answer for another commit, or
// with no date, is refused. A repository's description is read as GitHub
// gives it, a renamed one by its new name (the test plan's step 3).
func TestGitHubsCommitTimeAndDescription(t *testing.T) {
	t.Parallel()
	commit, other := strings.Repeat("a", 40), strings.Repeat("b", 40)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/repos/owner/project/git/commits/" + commit:
			fmt.Fprintf(w, `{"sha":%q,"committer":{"date":"2026-09-30T12:34:56Z"}}`, commit)
		case "/api/repos/owner/project/git/commits/" + strings.Repeat("c", 40):
			fmt.Fprintf(w, `{"sha":%q,"committer":{}}`, strings.Repeat("c", 40))
		case "/api/repos/owner/project":
			fmt.Fprint(w, `{"full_name":"owner/project-renamed","description":"a project","homepage":"https://project.example","license":{"spdx_id":"MIT"}}`)
		default:
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprint(w, `{"message":"Not Found"}`)
		}
	}))
	defer server.Close()
	client := github.Client{Client: &githubapi.Client{Config: githubapi.Config{BaseURL: server.URL + "/api"}}}
	repository, err := client.Repository("https://github.com", "owner/project")
	require.NoError(t, err)

	commits := repository.(forge.DatedRepository)
	when, err := commits.CommitTime(t.Context(), commit)
	require.NoError(t, err)
	require.Equal(t, time.Date(2026, 9, 30, 12, 34, 56, 0, time.UTC), when.UTC())
	_, err = commits.CommitTime(t.Context(), other)
	require.ErrorIs(t, err, forge.ErrNotFound)
	_, err = commits.CommitTime(t.Context(), strings.Repeat("c", 40))
	require.ErrorContains(t, err, "has no date")
	_, err = commits.CommitTime(t.Context(), "main")
	require.ErrorContains(t, err, "invalid commit")

	described, err := repository.(forge.DescribedRepository).Describe(t.Context())
	require.NoError(t, err)
	require.Equal(t, forge.Description{Name: "owner/project-renamed", Description: "a project", Homepage: "https://project.example", License: "MIT"}, described)
}
