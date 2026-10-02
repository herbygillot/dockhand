package ghactions

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	githubapi "github.com/herbygillot/dockhand/internal/github"
)

// A job's log past the 64 MiB dockhand keeps is kept to it, and says so at
// its end, where a person reading it looks, rather than reading as a log
// whose runner stopped there.
func TestAJobLogCutAtItsBoundSaysSo(t *testing.T) {
	for _, size := range []int{1 << 10, maxJobLogBytes, maxJobLogBytes + 1} {
		var server *httptest.Server
		server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/api/repos/ada/macports-ports/actions/jobs/7/logs":
				http.Redirect(w, r, server.URL+"/log", http.StatusFound)
			case "/log":
				w.Write(bytes.Repeat([]byte("l"), size))
			default:
				http.NotFound(w, r)
			}
		}))
		client := &githubapi.Client{HTTP: server.Client(), Config: githubapi.Config{BaseURL: server.URL + "/api/", Token: "fixture-token"}}
		log, err := GitHub{Client: client}.JobLog(t.Context(), "ada/macports-ports", 7)
		server.Close()
		require.NoError(t, err, size)
		if size <= maxJobLogBytes {
			require.Len(t, log, size, "a log within the bound is kept whole")
			continue
		}
		require.Equal(t, maxJobLogBytes+len(cutJobLog), len(log))
		require.True(t, bytes.HasSuffix(log, []byte(cutJobLog)))
		require.Contains(t, cutJobLog, "this log was cut at 64 MiB")
	}
}

// The Actions API as dockhand asks it, against a server answering as
// GitHub documents: a branch's runs of the workflow for a commit, across
// pages; one run; its jobs at an attempt, with their labels and runner;
// a rerun of the failed jobs; and a cancel, which GitHub accepts with
// 202 (the test plan's step 3).
func TestTheActionsAPIAsDockhandAsksIt(t *testing.T) {
	var asked []string
	var server *httptest.Server
	server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked = append(asked, r.Method+" "+r.URL.Path+"?"+r.URL.RawQuery)
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/api/repos/ada/macports-ports/actions/workflows/"+Workflow+"/runs" && r.URL.Query().Get("page") == "":
			w.Header().Set("Link", `<`+server.URL+`/api/repos/ada/macports-ports/actions/workflows/`+Workflow+`/runs?page=2>; rel="next"`)
			w.Write([]byte(`{"total_count":2,"workflow_runs":[{"id":11,"run_attempt":1,"status":"completed","conclusion":"failure","html_url":"https://github.com/ada/macports-ports/actions/runs/11"}]}`))
		case r.URL.Path == "/api/repos/ada/macports-ports/actions/workflows/"+Workflow+"/runs":
			w.Write([]byte(`{"total_count":2,"workflow_runs":[{"id":12,"run_attempt":2,"status":"in_progress"}]}`))
		case r.URL.Path == "/api/repos/ada/macports-ports/actions/runs/12":
			w.Write([]byte(`{"id":12,"run_attempt":2,"status":"completed","conclusion":"success"}`))
		case r.URL.Path == "/api/repos/ada/macports-ports/actions/runs/12/attempts/2/jobs":
			w.Write([]byte(`{"total_count":1,"jobs":[{"id":99,"name":"build (macos-15)","status":"completed","conclusion":"success","labels":["macos-15"],"runner_name":"GitHub Actions 1000","started_at":"2026-10-02T10:00:00Z","completed_at":"2026-10-02T10:30:00Z"}]}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/repos/ada/macports-ports/actions/runs/11/rerun-failed-jobs":
			w.WriteHeader(http.StatusCreated)
			w.Write([]byte(`{}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/repos/ada/macports-ports/actions/runs/12/cancel":
			w.WriteHeader(http.StatusAccepted)
			w.Write([]byte(`{}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	api := GitHub{Client: &githubapi.Client{HTTP: server.Client(), Config: githubapi.Config{BaseURL: server.URL + "/api/", Token: "fixture-token"}}}

	runs, err := api.Runs(t.Context(), "ada/macports-ports", "dockhand-check/0123456789ab", "0123456789abcdef0123456789abcdef01234567")
	require.NoError(t, err)
	require.Equal(t, []Run{
		{ID: 11, Attempt: 1, Status: "completed", Conclusion: "failure", URL: "https://github.com/ada/macports-ports/actions/runs/11"},
		{ID: 12, Attempt: 2, Status: "in_progress"},
	}, runs, "both pages")
	require.Contains(t, asked[0], "branch=dockhand-check%2F0123456789ab")
	require.Contains(t, asked[0], "event=push")
	require.Contains(t, asked[0], "head_sha=0123456789abcdef0123456789abcdef01234567")

	one, err := api.Run(t.Context(), "ada/macports-ports", 12)
	require.NoError(t, err)
	require.Equal(t, Run{ID: 12, Attempt: 2, Status: "completed", Conclusion: "success"}, one)

	jobs, err := api.Jobs(t.Context(), "ada/macports-ports", 12, 2)
	require.NoError(t, err)
	require.Len(t, jobs, 1)
	require.Equal(t, RunnerJob{ID: 99, Name: "build (macos-15)", Status: "completed", Conclusion: "success", Labels: []string{"macos-15"}, RunnerName: "GitHub Actions 1000",
		Started: time.Date(2026, 10, 2, 10, 0, 0, 0, time.UTC), Completed: time.Date(2026, 10, 2, 10, 30, 0, 0, time.UTC)}, jobs[0])

	require.NoError(t, api.Rerun(t.Context(), "ada/macports-ports", 11))
	require.NoError(t, api.Cancel(t.Context(), "ada/macports-ports", 12), "202 is the run stopping")
	require.ErrorContains(t, api.Cancel(t.Context(), "ada/macports-ports", 13), "404")
	_, err = api.Runs(t.Context(), "not a repository", "b", "c")
	require.ErrorContains(t, err, "invalid repository")
}
