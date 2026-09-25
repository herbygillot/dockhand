package github_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/herbygillot/dockhand/internal/forge"
	"github.com/herbygillot/dockhand/internal/forge/github"
	githubapi "github.com/herbygillot/dockhand/internal/github"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func prJSON() map[string]any {
	return map[string]any{"number": 3, "html_url": "https://github.com/upstream/ports/pull/3", "state": "open", "title": "port: update", "body": "details", "head": map[string]any{"ref": "candidate", "sha": strings.Repeat("a", 40), "repo": map[string]any{"full_name": "author/ports"}}, "base": map[string]any{"ref": "main", "repo": map[string]any{"full_name": "upstream/ports"}}}
}
func TestPullRequestsMapQueriesContentAndObservations(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer fixture-token", r.Header.Get("Authorization"))
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
	client := &github.Client{Client: &githubapi.Client{Config: githubapi.Config{BaseURL: server.URL, Token: "fixture-token"}}}
	query := forge.PullRequestQuery{Repository: "upstream/ports", HeadRepository: "author/ports", HeadBranch: "candidate", BaseBranch: "main"}
	found, err := client.Find(t.Context(), query)
	require.NoError(t, err)
	require.True(t, found.Found)
	observed, err := client.Observe(t.Context(), found.PullRequest.Ref)
	require.NoError(t, err)
	require.Equal(t, record.PullRequestOpen, observed.PullRequest.State)
	require.Equal(t, "https://github.com/upstream/ports/pull/3", observed.PullRequest.Ref.URL)
	input := forge.PullRequestInput{Repository: query.Repository, HeadRepository: query.HeadRepository, HeadBranch: query.HeadBranch, BaseBranch: query.BaseBranch, Desired: record.PublicationContent{Head: record.ObjectID(strings.Repeat("a", 40)), Title: "port: update", Body: "details"}}
	_, err = client.Create(t.Context(), input)
	require.NoError(t, err)
	input.ExistingPR = &found.PullRequest.Ref
	_, err = client.Update(t.Context(), input)
	require.NoError(t, err)
}

func TestGitHubPRLookupRejectsAmbiguousAndIncompleteObservations(t *testing.T) {
	for _, mode := range []string{"duplicate", "null row", "missing URL", "wrong repository", "missing head", "closed", "merged", "none"} {
		t.Run(mode, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				row := prJSON()
				rows := []any{row}
				switch mode {
				case "duplicate":
					rows = append(rows, row)
				case "null row":
					rows = []any{nil}
				case "missing URL":
					delete(row, "html_url")
				case "wrong repository":
					row["base"].(map[string]any)["repo"] = map[string]any{"full_name": "another/ports"}
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
			client := &github.Client{Client: &githubapi.Client{Config: githubapi.Config{BaseURL: server.URL}}}
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
	for _, status := range []int{http.StatusForbidden, http.StatusUnprocessableEntity, http.StatusInternalServerError, http.StatusMovedPermanently, http.StatusFound, http.StatusSeeOther, http.StatusTemporaryRedirect, http.StatusPermanentRedirect} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				w.Header().Set("Location", "/unexpected")
				w.WriteHeader(status)
			}))
			defer server.Close()
			client := &github.Client{Client: &githubapi.Client{Config: githubapi.Config{BaseURL: server.URL, Token: "fixture-token"}}}
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
	client := &github.Client{Client: &githubapi.Client{}}
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
		fmt.Fprint(w, `{"full_name":"author/ports","default_branch":"master","clone_url":"https://github.com/author/ports.git","fork":true,"parent":{"full_name":"upstream/ports"}}`)
	}))
	defer server.Close()
	client.Config.BaseURL = server.URL
	info, err := client.RepositoryInfo(t.Context(), "author/ports")
	require.NoError(t, err)
	require.Equal(t, "upstream/ports", info.Parent)
	require.Equal(t, "master", info.DefaultBranch)
	require.Equal(t, "https://github.com/author/ports.git", info.CloneURL)
}

func TestPullRequestLookupChecksEveryPageBeforeAcceptingAMatch(t *testing.T) {
	for _, mode := range []string{"match", "ambiguous", "later failure"} {
		t.Run(mode, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if calls == 1 {
					w.Header().Set("Link", fmt.Sprintf(`<http://%s%s?page=2>; rel="next"`, r.Host, r.URL.Path))
					json.NewEncoder(w).Encode([]any{prJSON()})
					return
				}
				switch mode {
				case "match":
					fmt.Fprint(w, "[]")
				case "ambiguous":
					json.NewEncoder(w).Encode([]any{prJSON()})
				case "later failure":
					w.WriteHeader(http.StatusServiceUnavailable)
				}
			}))
			defer server.Close()
			client := &github.Client{Client: &githubapi.Client{Config: githubapi.Config{BaseURL: server.URL}}}
			found, err := client.Find(t.Context(), forge.PullRequestQuery{Repository: "upstream/ports", HeadRepository: "author/ports", HeadBranch: "candidate", BaseBranch: "main"})
			require.Equal(t, 2, calls)
			if mode == "match" {
				require.NoError(t, err)
				require.True(t, found.Found)
			} else {
				require.Error(t, err)
				require.False(t, found.Found)
			}
		})
	}
}

func TestRepositoryInfoUsesTheReturnedCloneURL(t *testing.T) {
	for _, test := range []struct {
		name      string
		address   string
		wantError bool
	}{
		{"returned spelling", "https://github.com/Author/ports", false},
		{"missing", "", true},
		{"different repository", "https://github.com/another/ports.git", true},
		{"different host", "https://example.invalid/author/ports.git", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				json.NewEncoder(w).Encode(map[string]any{"full_name": "author/ports", "default_branch": "main", "clone_url": test.address})
			}))
			defer server.Close()
			client := &github.Client{Client: &githubapi.Client{Config: githubapi.Config{BaseURL: server.URL}}}
			info, err := client.RepositoryInfo(t.Context(), "author/ports")
			if test.wantError {
				require.Error(t, err)
				require.Empty(t, info.CloneURL)
				return
			}
			require.NoError(t, err)
			require.Equal(t, test.address, info.CloneURL)
		})
	}
}

func TestCanceledPublicationWriteRemainsUncertain(t *testing.T) {
	client := &github.Client{Client: &githubapi.Client{HTTP: &http.Client{Transport: transportFunc(func(*http.Request) (*http.Response, error) { return nil, context.Canceled })}, Config: githubapi.Config{Token: "fixture-token"}}}
	_, err := client.Create(t.Context(), forge.PullRequestInput{Repository: "upstream/ports", HeadRepository: "author/ports", HeadBranch: "candidate", BaseBranch: "main", Desired: record.PublicationContent{Title: "update"}})
	require.ErrorIs(t, err, context.Canceled)
	require.NotErrorIs(t, err, forge.ErrRejected)
}

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestRateLimitedWritesRemainDistinctFromPermissionRejections(t *testing.T) {
	for _, status := range []int{403, 429} {
		for _, kind := range []string{"primary", "secondary"} {
			t.Run(fmt.Sprintf("%d/%s", status, kind), func(t *testing.T) {
				reset := time.Now().Add(2 * time.Minute).Truncate(time.Second)
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if kind == "primary" {
						w.Header().Set("X-RateLimit-Remaining", "0")
						w.Header().Set("X-RateLimit-Reset", fmt.Sprint(reset.Unix()))
					} else {
						w.Header().Set("Retry-After", "120")
					}
					w.WriteHeader(status)
					fmt.Fprint(w, `{"message":"rate limited","documentation_url":"https://docs.github.com/rest/using-the-rest-api/rate-limits-for-the-rest-api#about-secondary-rate-limits"}`)
				}))
				defer server.Close()
				client := &github.Client{Client: &githubapi.Client{Config: githubapi.Config{BaseURL: server.URL, Token: "fixture-token"}}}
				_, err := client.Create(t.Context(), forge.PullRequestInput{Repository: "upstream/ports", HeadRepository: "author/ports", HeadBranch: "candidate", BaseBranch: "main", Desired: record.PublicationContent{Title: "update"}})
				var limited *forge.RateLimitError
				require.ErrorAs(t, err, &limited)
				require.NotErrorIs(t, err, forge.ErrRejected)
				require.WithinDuration(t, reset, limited.RetryAt, 2*time.Second)
			})
		}
	}
}

func TestObserveTerminalPRFromDeletedFork(t *testing.T) {
	for _, merged := range []bool{false, true} {
		row := prJSON()
		row["state"] = "closed"
		if merged {
			row["merged_at"] = "2026-09-16T12:00:00Z"
		}
		row["head"].(map[string]any)["repo"] = nil
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { json.NewEncoder(w).Encode(row) }))
		client := &github.Client{Client: &githubapi.Client{Config: githubapi.Config{BaseURL: server.URL, Token: "fixture-token"}}}
		result, err := client.Observe(t.Context(), record.PullRequestRef{Forge: forge.GitHub, Repository: "upstream/ports", Number: 3})
		server.Close()
		require.NoError(t, err)
		expected := record.PullRequestClosed
		if merged {
			expected = record.PullRequestMerged
		}
		require.Equal(t, expected, result.PullRequest.State)
		require.Empty(t, result.PullRequest.HeadRepository)
		require.Equal(t, "candidate", result.PullRequest.HeadBranch)
	}
}

func TestMarkReadyTakesADraftOutOfDraft(t *testing.T) {
	draft := true
	var mutations int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/repos/upstream/ports/pulls/3":
			row := prJSON()
			row["draft"], row["node_id"] = draft, "PR_node3"
			json.NewEncoder(w).Encode(row)
		case r.Method == http.MethodPost && r.URL.Path == "/graphql":
			var payload struct {
				Query     string
				Variables map[string]any
			}
			assert.NoError(t, json.NewDecoder(r.Body).Decode(&payload))
			assert.Contains(t, payload.Query, "markPullRequestReadyForReview")
			assert.Equal(t, "PR_node3", payload.Variables["id"])
			mutations++
			draft = false
			fmt.Fprint(w, `{"data": {"markPullRequestReadyForReview": {"pullRequest": {"isDraft": false}}}}`)
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()
	client := &github.Client{Client: &githubapi.Client{Config: githubapi.Config{BaseURL: server.URL, Token: "fixture-token"}}}
	ref := record.PullRequestRef{Forge: forge.GitHub, Repository: "upstream/ports", Number: 3}
	_, err := client.MarkReady(t.Context(), ref)
	require.NoError(t, err)
	require.Equal(t, 1, mutations)
	_, err = client.MarkReady(t.Context(), ref)
	require.NoError(t, err)
	require.Equal(t, 1, mutations, "a pull request already ready is left alone")
}
