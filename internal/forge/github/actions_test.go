package github_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/herbygillot/dockhand/internal/forge/github"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestActionsUseAuthenticatedSDKPaginationAndPinnedAttempt(t *testing.T) {
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
	c := &github.Client{Config: github.Config{BaseURL: server.URL, Token: "fixture-token"}}
	api, err := c.Actions(t.Context(), "author/ports")
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
