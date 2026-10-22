package github_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/forge"
	"github.com/herbygillot/dockhand/internal/forge/github"
	githubapi "github.com/herbygillot/dockhand/internal/github"
)

// A head's failed CI jobs are its failed runs' failed jobs, through the
// documented endpoints; a job's log is where its link sends it, without
// the time before each line, and one GitHub no longer keeps is not found;
// running a run's failed jobs again without write access is rejected.
func TestAHeadsFailedCIJobsTheirLogsAndARerun(t *testing.T) {
	t.Parallel()
	var server *httptest.Server
	var rerun []string
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/repos/macports/macports-ports/actions/runs" && r.URL.Query().Get("head_sha") == "abc":
			fmt.Fprint(w, `{"total_count": 2, "workflow_runs": [{"id": 7, "conclusion": "failure"}, {"id": 8, "conclusion": "success"}]}`)
		case r.URL.Path == "/api/repos/macports/macports-ports/actions/runs/7/jobs":
			fmt.Fprint(w, `{"total_count": 2, "jobs": [{"id": 11, "run_id": 7, "run_attempt": 1, "name": "macos-15", "conclusion": "failure"}, {"id": 12, "run_id": 7, "run_attempt": 1, "name": "macos-14", "conclusion": "success"}]}`)
		case r.URL.Path == "/api/repos/macports/macports-ports/actions/jobs/11/logs":
			http.Redirect(w, r, server.URL+"/blob/11", http.StatusFound)
		case r.URL.Path == "/blob/11":
			fmt.Fprint(w, "2026-10-03T14:00:17.7588890Z Error: Failed to fetch jni-sys-macros-0.4.1.crate: The requested URL returned error: 403\n2026-10-03T14:00:20.0727630Z Error: Failed to fetch garage: Failed to fetch distfiles\n")
		case r.URL.Path == "/api/repos/macports/macports-ports/actions/runs/7/rerun-failed-jobs" && r.Method == http.MethodPost:
			rerun = append(rerun, "7")
			w.WriteHeader(http.StatusCreated)
		case r.URL.Path == "/api/repos/macports/macports-ports/actions/runs/9/rerun-failed-jobs":
			w.WriteHeader(http.StatusForbidden)
			fmt.Fprint(w, `{"message":"Must have admin rights to Repository."}`)
		default:
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprint(w, `{"message":"Not Found"}`)
		}
	}))
	defer server.Close()
	client := &github.Client{Client: &githubapi.Client{Config: githubapi.Config{BaseURL: server.URL + "/api", Token: "token"}}}

	jobs, err := client.FailedJobs(t.Context(), "macports/macports-ports", "abc")
	require.NoError(t, err)
	require.Equal(t, []forge.CIJob{{ID: 11, Run: 7, Attempt: 1, Name: "macos-15"}}, jobs)

	log, err := client.JobLog(t.Context(), "macports/macports-ports", 11)
	require.NoError(t, err)
	require.Equal(t, "Error: Failed to fetch jni-sys-macros-0.4.1.crate: The requested URL returned error: 403\nError: Failed to fetch garage: Failed to fetch distfiles\n", string(log))
	_, err = client.JobLog(t.Context(), "macports/macports-ports", 12)
	require.ErrorIs(t, err, forge.ErrNotFound, "a log GitHub no longer keeps")

	require.NoError(t, client.RerunFailed(t.Context(), "macports/macports-ports", 7))
	require.Equal(t, []string{"7"}, rerun)
	require.ErrorIs(t, client.RerunFailed(t.Context(), "macports/macports-ports", 9), forge.ErrRejected, "without write access")
}
