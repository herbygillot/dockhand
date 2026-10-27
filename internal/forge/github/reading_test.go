package github_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/forge"
	"github.com/herbygillot/dockhand/internal/forge/github"
	githubapi "github.com/herbygillot/dockhand/internal/github"
)

// Many pull requests are read in one GraphQL request, with their reviews
// and checks counted as Inspect counts them: the latest opinion of each
// reviewer, changes requested deciding it, and checks still running or
// waiting on a maintainer pending. One GitHub hasn't is not found, and one
// with more checks than a reading takes is partial, for Inspect to read
// (the SQL study's O8).
func TestPullRequestsAreReadAtOnce(t *testing.T) {
	head := strings.Repeat("a", 40)
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.Method != http.MethodPost || r.URL.Path != "/graphql" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			return
		}
		var payload struct{ Query string }
		assert.NoError(t, json.NewDecoder(r.Body).Decode(&payload))
		assert.Contains(t, payload.Query, `p0: repository(owner: "macports", name: "macports-ports") { pullRequest(number: 34901) { ...fields } }`)
		assert.Contains(t, payload.Query, `p2: repository(owner: "macports", name: "macports-ports") { pullRequest(number: 9) { ...fields } }`)
		fmt.Fprintf(w, `{"data": {
  "p0": {"pullRequest": {"number": 34901, "state": "OPEN", "isDraft": false, "headRefOid": %[1]q, "mergeable": "CONFLICTING",
    "latestOpinionatedReviews": {"totalCount": 2, "nodes": [
      {"state": "APPROVED", "submittedAt": "2026-10-15T08:00:00Z", "author": {"login": "ryandesign"}},
      {"state": "CHANGES_REQUESTED", "submittedAt": "2026-10-15T09:00:00Z", "author": {"login": "jmroot"}}]},
    "commits": {"nodes": [{"commit": {"statusCheckRollup": {"contexts": {"totalCount": 4, "nodes": [
      {"__typename": "CheckRun", "name": "macOS 15", "status": "COMPLETED", "conclusion": "FAILURE"},
      {"__typename": "CheckRun", "name": "macOS 14", "status": "COMPLETED", "conclusion": "SUCCESS"},
      {"__typename": "CheckRun", "name": "macOS 13", "status": "COMPLETED", "conclusion": "ACTION_REQUIRED"},
      {"__typename": "StatusContext", "context": "trac", "state": "SUCCESS"}]}}}}]}}},
  "p1": {"pullRequest": {"number": 34902, "state": "MERGED", "isDraft": false, "headRefOid": %[1]q, "mergeable": "UNKNOWN",
    "latestOpinionatedReviews": {"totalCount": 0, "nodes": []},
    "commits": {"nodes": [{"commit": {"statusCheckRollup": {"contexts": {"totalCount": 150, "nodes": []}}}}]}}},
  "p2": {"pullRequest": null}
}, "errors": [{"type": "NOT_FOUND", "path": ["p2", "pullRequest"], "message": "Could not resolve to a PullRequest with the number of 9."}]}`, head)
	}))
	defer server.Close()
	client := &github.Client{Client: &githubapi.Client{Config: githubapi.Config{BaseURL: server.URL, Token: "fixture-token"}}}
	ref := func(number int) forge.PullRequestRef {
		return forge.PullRequestRef{Forge: forge.GitHub, Repository: "macports/macports-ports", Number: number}
	}
	readings, err := client.ReadPullRequests(t.Context(), []forge.PullRequestRef{ref(34901), ref(34902), ref(9)})
	require.NoError(t, err)
	require.Equal(t, 1, requests, "one request for all of them")
	require.Len(t, readings, 3)

	open := readings[0]
	require.True(t, open.Found)
	require.False(t, open.Partial)
	require.Equal(t, forge.PullRequestOpen, open.PullRequest.State)
	require.EqualValues(t, head, open.PullRequest.RemoteHead)
	require.Equal(t, "changes-requested", open.Status.Review)
	require.Equal(t, []string{"jmroot"}, open.Status.ChangesRequestedBy)
	require.Equal(t, time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC), open.Status.ReviewedAt)
	require.Equal(t, forge.CheckSummary{Total: 4, Passed: 2, Failed: 1, Pending: 1, Failing: []string{"macOS 15"}}, open.Status.Checks)
	require.Equal(t, "no", open.Status.Mergeable, "GitHub's conflict is read, where it was always unknown (the docs dig's I2)")
	require.Equal(t, "unknown", readings[1].Status.Mergeable, "until GitHub has computed the merge")

	require.Equal(t, forge.PullRequestMerged, readings[1].PullRequest.State)
	require.True(t, readings[1].Partial, "more checks than a reading takes")
	require.False(t, readings[2].Found)
}
