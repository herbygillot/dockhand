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
// (the SQL study's O8). Its opening, author, description's mentions, and
// approvers come in the same request, for its merge's fitness.
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
    "createdAt": "2026-10-14T09:00:00Z", "body": "jq: update to 1.8.1\n\ncc @ryandesign", "author": {"login": "herbygillot"},
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
	require.Equal(t, []string{"ryandesign"}, open.Status.ApprovedBy, "who approved, which a merge's fitness reads")
	require.Equal(t, time.Date(2026, 10, 14, 9, 0, 0, 0, time.UTC), open.PullRequest.CreatedAt, "its opening, which the maintainer timeout counts from, in the same request")
	require.Equal(t, "herbygillot", open.PullRequest.Author)
	require.Equal(t, []string{"ryandesign"}, open.PullRequest.Mentions)
	require.Equal(t, time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC), open.Status.ReviewedAt)
	require.Equal(t, forge.CheckSummary{Total: 4, Passed: 2, Failed: 1, Pending: 1, Failing: []string{"macOS 15"}}, open.Status.Checks)
	require.Equal(t, "no", open.Status.Mergeable, "GitHub's conflict is read, where it was always unknown (the docs dig's I2)")
	require.Equal(t, "unknown", readings[1].Status.Mergeable, "until GitHub has computed the merge")

	require.Equal(t, forge.PullRequestMerged, readings[1].PullRequest.State)
	require.True(t, readings[1].Partial, "more checks than a reading takes")
	require.False(t, readings[2].Found)
}

// A signed-out look reads a pull request's opening, author, and
// description's mentions with the one request it makes of it, through the
// conditional cache: a second look is GitHub's 304, which spends nothing
// of the hour, and reads the same, so a merge's fitness costs no request
// a look didn't make.
func TestAPullRequestsOpeningIsReadAgainForNothing(t *testing.T) {
	head := strings.Repeat("a", 40)
	var asked, notModified int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked++
		if r.Method != http.MethodGet || r.URL.Path != "/repos/macports/macports-ports/pulls/34901" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			return
		}
		if r.Header.Get("If-None-Match") == `"v1"` {
			notModified++
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("ETag", `"v1"`)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"number": 34901, "state": "open", "title": "jq: update to 1.8.1", "html_url": "https://github.com/macports/macports-ports/pull/34901",
  "created_at": "2026-10-14T09:00:00Z", "body": "cc @ryandesign", "user": {"login": "herbygillot"},
  "head": {"ref": "jq-update", "sha": %q, "repo": {"full_name": "herbygillot/macports-ports"}},
  "base": {"ref": "master", "repo": {"full_name": "macports/macports-ports"}}}`, head)
	}))
	defer server.Close()
	client := &github.Client{Client: &githubapi.Client{Config: githubapi.Config{BaseURL: server.URL}, Conditional: githubapi.NewConditionalCache(t.TempDir())}}
	ref := forge.PullRequestRef{Forge: forge.GitHub, Repository: "macports/macports-ports", Number: 34901}
	for range 2 {
		observed, err := client.Observe(t.Context(), ref)
		require.NoError(t, err)
		require.Equal(t, time.Date(2026, 10, 14, 9, 0, 0, 0, time.UTC), observed.PullRequest.CreatedAt)
		require.Equal(t, "herbygillot", observed.PullRequest.Author)
		require.Equal(t, []string{"ryandesign"}, observed.PullRequest.Mentions)
	}
	require.Equal(t, 2, asked)
	require.Equal(t, 1, notModified, "the second look is GitHub's 304")
}

// A comment by someone other than the author, a person and after the
// head was committed, is a reviewer waiting, which GitHub's review state
// doesn't count: reneeotten's question on #36300 read "CI ✓" alone (#90).
// The author's own word, a bot's, or one before the head isn't.
func TestAReviewersLatestCommentIsRead(t *testing.T) {
	head := strings.Repeat("a", 40)
	pullRequest := func(number int, by, kind, at string) string {
		return fmt.Sprintf(`{"pullRequest": {"number": %d, "state": "OPEN", "isDraft": false, "headRefOid": %q, "mergeable": "MERGEABLE",
    "createdAt": "2026-11-01T09:00:00Z", "body": "", "author": {"login": "herbygillot"},
    "latestOpinionatedReviews": {"totalCount": 0, "nodes": []},
    "comments": {"nodes": [{"createdAt": %q, "author": {"__typename": %q, "login": %q}}]},
    "commits": {"nodes": [{"commit": {"committedDate": "2026-11-04T09:00:00Z", "statusCheckRollup": {"contexts": {"totalCount": 0, "nodes": []}}}}]}}}`, number, head, at, kind, by)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"data": {"p0": %s, "p1": %s, "p2": %s, "p3": %s}}`,
			pullRequest(1, "reneeotten", "User", "2026-11-05T14:00:09Z"),
			pullRequest(2, "herbygillot", "User", "2026-11-05T14:00:09Z"),
			pullRequest(3, "macportsbot", "Bot", "2026-11-05T14:00:09Z"),
			pullRequest(4, "reneeotten", "User", "2026-11-03T14:00:09Z"))
	}))
	defer server.Close()
	client := &github.Client{Client: &githubapi.Client{Config: githubapi.Config{BaseURL: server.URL, Token: "fixture-token"}}}
	var refs []forge.PullRequestRef
	for number := 1; number <= 4; number++ {
		refs = append(refs, forge.PullRequestRef{Forge: forge.GitHub, Repository: "macports/macports-ports", Number: number})
	}
	readings, err := client.ReadPullRequests(t.Context(), refs)
	require.NoError(t, err)
	var commented []string
	for _, reading := range readings {
		commented = append(commented, reading.Status.CommentedBy)
	}
	require.Equal(t, []string{"reneeotten", "", "", ""}, commented)
}
