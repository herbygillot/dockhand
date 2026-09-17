package github

import (
	"context"
	"fmt"
	forgegithub "github.com/herbygillot/dockhand/internal/forge/github"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	githubapi "github.com/herbygillot/dockhand/internal/github"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestActionsUseAuthenticatedSDKPaginationAndPinnedAttempt(t *testing.T) {
	t.Parallel()
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer fixture-token", r.Header.Get("Authorization"))
		switch r.URL.Path {
		case "/repos/author/ports/actions/workflows/main.yml":
			fmt.Fprint(w, `{"id":7,"path":".github/workflows/main.yml","state":"active"}`)
		case "/repos/author/ports/actions/workflows/7/runs":
			assert.Equal(t, "candidate", r.URL.Query().Get("branch"))
			assert.Equal(t, strings.Repeat("a", 40), r.URL.Query().Get("head_sha"))
			assert.Equal(t, "push", r.URL.Query().Get("event"))
			if r.URL.Query().Get("page") == "2" {
				fmt.Fprint(w, `{"workflow_runs":[{"id":2}]}`)
			} else {
				w.Header().Set("Link", "<"+server.URL+r.URL.Path+"?page=2>; rel=\"next\"")
				fmt.Fprint(w, `{"workflow_runs":[{"id":1}]}`)
			}
		case "/repos/author/ports/actions/runs/1/attempts/3":
			fmt.Fprint(w, `{"id":1,"run_attempt":3}`)
		case "/repos/author/ports/actions/runs/1/attempts/3/jobs":
			fmt.Fprint(w, `{"jobs":[{"id":11,"run_id":1,"run_attempt":3}]}`)
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	c := &githubapi.Client{Config: githubapi.Config{BaseURL: server.URL, Token: "fixture-token"}}
	api, err := newActions(t.Context(), c, "author/ports")
	require.NoError(t, err)
	flow, err := api.Workflow(t.Context(), "main.yml")
	require.NoError(t, err)
	require.Equal(t, int64(7), flow.GetID())
	runs, err := api.Runs(t.Context(), 7, "candidate", strings.Repeat("a", 40))
	require.NoError(t, err)
	require.Len(t, runs, 2)
	run, err := api.Run(t.Context(), 1, 3)
	require.NoError(t, err)
	require.Equal(t, 3, run.GetRunAttempt())
	jobs, err := api.Jobs(t.Context(), 1, 3)
	require.NoError(t, err)
	require.Len(t, jobs, 1)
	require.Equal(t, int64(3), jobs[0].GetRunAttempt())
}

func TestRepositoryAndActionsShareCredentialInitialization(t *testing.T) {
	t.Parallel()
	var credentials atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer shared-token", r.Header.Get("Authorization"))
		switch r.URL.Path {
		case "/repos/owner/ports/git/ref/tags/v2":
			fmt.Fprintf(w, `{"ref":"refs/tags/v2","object":{"type":"commit","sha":%q}}`, strings.Repeat("a", 40))
		case "/repos/owner/ports/actions/workflows/main.yml":
			fmt.Fprint(w, `{"id":7,"path":".github/workflows/main.yml","state":"active"}`)
		default:
			t.Errorf("unexpected request: %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client := &githubapi.Client{Config: githubapi.Config{BaseURL: server.URL}, Credentials: githubapi.TokenSourceFunc(func(context.Context) (githubapi.Token, error) {
		credentials.Add(1)
		return githubapi.Token{Secret: "shared-token", Source: githubapi.SourceKeychain}, nil
	})}
	repository, err := (&forgegithub.Client{Client: client}).Repository("https://github.com", "owner/ports")
	require.NoError(t, err)
	done := make(chan error, 2)
	go func() { _, err := repository.Tag(t.Context(), "v2"); done <- err }()
	go func() {
		api, err := newActions(t.Context(), client, "owner/ports")
		if err == nil {
			_, err = api.Workflow(t.Context(), "main.yml")
		}
		done <- err
	}()
	require.NoError(t, <-done)
	require.NoError(t, <-done)
	require.Equal(t, int64(1), credentials.Load())
}
