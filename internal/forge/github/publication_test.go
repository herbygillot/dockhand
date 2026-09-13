package github_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/herbygillot/dockhand/v2/internal/forge"
	"github.com/herbygillot/dockhand/v2/internal/forge/github"
	"github.com/herbygillot/dockhand/v2/internal/record"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func prJSON() map[string]any {
	return map[string]any{"number": 3, "html_url": "https://github.com/upstream/ports/pull/3", "state": "open", "title": "port: update", "body": "details", "head": map[string]any{"ref": "candidate", "sha": strings.Repeat("a", 40), "repo": map[string]any{"full_name": "author/ports"}}, "base": map[string]any{"ref": "main", "repo": map[string]any{"full_name": "upstream/ports"}}}
}
func TestGitHubPublicationProtocol(t *testing.T) {
	var methods []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		methods = append(methods, r.Method)
		assert.Equal(t, "Bearer fixture-token", r.Header.Get("Authorization"))
		assert.Equal(t, "2026-03-10", r.Header.Get("X-GitHub-Api-Version"))
		if r.Method == http.MethodGet && r.URL.Path == "/repos/upstream/ports/pulls" {
			assert.Equal(t, "author:candidate", r.URL.Query().Get("head"))
			assert.Equal(t, "main", r.URL.Query().Get("base"))
			assert.Equal(t, "all", r.URL.Query().Get("state"))
			json.NewEncoder(w).Encode([]any{prJSON()})
			return
		}
		if r.Method != http.MethodGet {
			var payload map[string]any
			assert.NoError(t, json.NewDecoder(r.Body).Decode(&payload))
			assert.Equal(t, "port: update", payload["title"])
			assert.Equal(t, "details", payload["body"])
			if r.Method == http.MethodPost {
				assert.Equal(t, "author:candidate", payload["head"])
				assert.Equal(t, "ports", payload["head_repo"])
				assert.Equal(t, "main", payload["base"])
				w.WriteHeader(http.StatusCreated)
			} else {
				assert.Equal(t, "/repos/upstream/ports/pulls/3", r.URL.Path)
				assert.Len(t, payload, 2)
			}
		}
		json.NewEncoder(w).Encode(prJSON())
	}))
	defer server.Close()
	client := &github.Client{Config: github.Config{BaseURL: server.URL, Token: "fixture-token"}}
	query := forge.PullRequestQuery{Repository: "upstream/ports", HeadRepository: "author/ports", HeadBranch: "candidate", BaseBranch: "main"}
	found, err := client.Find(t.Context(), query)
	require.NoError(t, err)
	require.True(t, found.Found)
	observed, err := client.Observe(t.Context(), found.PullRequest.Ref)
	require.NoError(t, err)
	require.Equal(t, record.PullRequestOpen, observed.PullRequest.State)
	input := forge.PullRequestInput{Repository: query.Repository, HeadRepository: query.HeadRepository, HeadBranch: query.HeadBranch, BaseBranch: query.BaseBranch, Desired: record.PublicationContent{Head: record.ObjectID(strings.Repeat("a", 40)), Title: "port: update", Body: "details"}}
	_, err = client.Create(t.Context(), input)
	require.NoError(t, err)
	input.ExistingPR = &found.PullRequest.Ref
	_, err = client.Update(t.Context(), input)
	require.NoError(t, err)
	require.Equal(t, []string{"GET", "GET", "POST", "PATCH"}, methods)
}

func TestGitHubPRLookupRejectsAmbiguousAndIncompleteObservations(t *testing.T) {
	for _, mode := range []string{"duplicate", "null", "wrong URL", "missing head", "closed", "merged", "none"} {
		t.Run(mode, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				row := prJSON()
				rows := []any{row}
				switch mode {
				case "duplicate":
					rows = append(rows, row)
				case "null":
					rows = nil
				case "wrong URL":
					row["html_url"] = "https://example.invalid/pr/3"
				case "missing head":
					row["head"] = nil
				case "closed":
					row["state"] = "closed"
				case "merged":
					row["state"] = "closed"
					row["merged_at"] = "2026-01-01T00:00:00Z"
				case "none":
					rows = []any{}
				}
				json.NewEncoder(w).Encode(rows)
			}))
			defer server.Close()
			client := &github.Client{Config: github.Config{BaseURL: server.URL}}
			observed, err := client.Find(t.Context(), forge.PullRequestQuery{Repository: "upstream/ports", HeadRepository: "author/ports", HeadBranch: "candidate", BaseBranch: "main"})
			switch mode {
			case "none":
				require.NoError(t, err)
				require.False(t, observed.Found)
			case "closed":
				require.NoError(t, err)
				require.Equal(t, record.PullRequestClosed, observed.PullRequest.State)
			case "merged":
				require.NoError(t, err)
				require.Equal(t, record.PullRequestMerged, observed.PullRequest.State)
			default:
				require.Error(t, err)
			}
		})
	}
}

func TestGitHubWritesDistinguishRejectionFromUnknownOutcomesAndDoNotRedirect(t *testing.T) {
	for _, status := range []int{http.StatusForbidden, http.StatusUnprocessableEntity, http.StatusInternalServerError, http.StatusTemporaryRedirect} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				w.Header().Set("Location", "/unexpected")
				w.WriteHeader(status)
			}))
			defer server.Close()
			client := &github.Client{Config: github.Config{BaseURL: server.URL}}
			_, err := client.Create(t.Context(), forge.PullRequestInput{Repository: "upstream/ports", HeadRepository: "author/ports", HeadBranch: "candidate", BaseBranch: "main", Desired: record.PublicationContent{Title: "update"}})
			require.Error(t, err)
			require.Equal(t, 1, calls)
			if status == 403 || status == 422 {
				require.ErrorIs(t, err, forge.ErrRejected)
			} else {
				require.NotErrorIs(t, err, forge.ErrRejected)
			}
		})
	}
}

func TestGitHubRemoteNamesAndForkMetadata(t *testing.T) {
	client := &github.Client{}
	for _, remote := range []string{"git@github.com:Owner/ports.git", "https://github.com/Owner/ports", "ssh://git@github.com/Owner/ports.git"} {
		name, err := client.NameFromRemote(remote)
		require.NoError(t, err)
		require.Equal(t, "Owner/ports", name)
	}
	for _, remote := range []string{"https://github.com.evil/owner/ports", "https://token@github.com/owner/ports", "https://github.com/owner/ports?x=y", "git@github.com:owner/../ports", "file:///repo"} {
		_, err := client.NameFromRemote(remote)
		require.Error(t, err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"full_name":"author/ports","default_branch":"master","fork":true,"parent":{"full_name":"upstream/ports"}}`)
	}))
	defer server.Close()
	client.Config.BaseURL = server.URL
	info, err := client.RepositoryInfo(t.Context(), "author/ports")
	require.NoError(t, err)
	require.Equal(t, "upstream/ports", info.Parent)
	require.Equal(t, "master", info.DefaultBranch)
	require.Equal(t, "https://github.com/author/ports.git", info.CloneURL)
}
