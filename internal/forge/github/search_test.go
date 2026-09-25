package github_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/forge"
	"github.com/herbygillot/dockhand/internal/forge/github"
	githubapi "github.com/herbygillot/dockhand/internal/github"
)

func TestOpenPullRequestsMatchTheSubjectsPortList(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/search/issues", r.URL.Path)
		assert.Equal(t, `repo:macports/macports-ports is:pr is:open in:title "jq"`, r.URL.Query().Get("q"))
		pr := map[string]any{"url": "https://api.github.com/pulls/1"}
		json.NewEncoder(w).Encode(map[string]any{"total_count": 4, "items": []any{
			map[string]any{"number": 1, "title": "jq: update to 1.8.0", "html_url": "https://github.com/macports/macports-ports/pull/1", "pull_request": pr},
			map[string]any{"number": 2, "title": "gojq, jq: rebuild", "html_url": "u2", "pull_request": pr},
			map[string]any{"number": 3, "title": "jqp: update to 0.8", "html_url": "u3", "pull_request": pr},
			map[string]any{"number": 4, "title": "jq: an issue, not a pull request", "html_url": "u4"},
		}})
	}))
	defer server.Close()
	client := &github.Client{Client: &githubapi.Client{Config: githubapi.Config{BaseURL: server.URL, Token: "fixture-token"}}}
	found, err := client.OpenPullRequests(t.Context(), "macports/macports-ports", "jq")
	require.NoError(t, err)
	require.Equal(t, []forge.PullRequestSummary{
		{Number: 1, Title: "jq: update to 1.8.0", URL: "https://github.com/macports/macports-ports/pull/1"},
		{Number: 2, Title: "gojq, jq: rebuild", URL: "u2"},
	}, found)
	_, err = client.OpenPullRequests(t.Context(), "macports/macports-ports", `jq" OR`)
	require.Error(t, err)
}

func TestADraftIsRequested(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		assert.NoError(t, json.NewDecoder(r.Body).Decode(&payload))
		assert.Equal(t, true, payload["draft"])
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(prJSON())
	}))
	defer server.Close()
	client := &github.Client{Client: &githubapi.Client{Config: githubapi.Config{BaseURL: server.URL, Token: "fixture-token"}}}
	input := forge.PullRequestInput{Repository: "upstream/ports", HeadRepository: "author/ports", HeadBranch: "candidate", BaseBranch: "main", Draft: true}
	input.Desired.Title = "port: update"
	_, err := client.Create(t.Context(), input)
	require.NoError(t, err)
}
