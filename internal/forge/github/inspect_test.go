package github_test

import (
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
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInspectSummarizesMergeabilityReviewsAndChecks(t *testing.T) {
	head := strings.Repeat("a", 40)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/pulls/7/reviews"):
			fmt.Fprint(w, `[{"state":"CHANGES_REQUESTED","user":{"login":"alice"}},{"state":"APPROVED","user":{"login":"alice"}},{"state":"COMMENTED","user":{"login":"bob"}},{"state":"APPROVED","user":{"login":"carol"}},{"state":"DISMISSED","user":{"login":"carol"}}]`)
		case strings.HasSuffix(r.URL.Path, "/pulls/7"):
			fmt.Fprintf(w, `{"number":7,"state":"open","draft":false,"mergeable":false,"mergeable_state":"dirty","head":{"sha":%q}}`, head)
		case strings.HasSuffix(r.URL.Path, "/check-runs"):
			fmt.Fprint(w, `{"total_count":3,"check_runs":[{"name":"Build ports (macos-14)","status":"completed","conclusion":"failure"},{"name":"Build ports (macos-15)","status":"completed","conclusion":"success"},{"name":"Lint","status":"in_progress"}]}`)
		case strings.HasSuffix(r.URL.Path, "/status"):
			fmt.Fprint(w, `{"state":"failure","statuses":[{"context":"buildbot/ports-13","state":"error"}]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client := &github.Client{Client: &githubapi.Client{Config: githubapi.Config{BaseURL: server.URL}}}
	status, err := client.Inspect(t.Context(), forge.PullRequestRef{Forge: forge.GitHub, Repository: "macports/macports-ports", Number: 7})
	require.NoError(t, err)
	require.Equal(t, "no", status.Mergeable)
	require.Equal(t, "dirty", status.MergeableDetail)
	require.Equal(t, "approved", status.Review, "the latest review per reviewer counts, and dismissed reviews do not")
	require.Equal(t, 1, status.Approvals)
	require.Equal(t, forge.CheckSummary{Total: 4, Passed: 1, Failed: 2, Pending: 1, Failing: []string{"Build ports (macos-14)", "buildbot/ports-13"}}, status.Checks)
	require.Equal(t, "mergeable: no (dirty); review: approved; checks: 1 passed, 2 failed, 1 pending of 4 (failing: Build ports (macos-14), buildbot/ports-13)", status.Summary())
	_, err = client.Inspect(t.Context(), forge.PullRequestRef{Forge: "gitlab", Repository: "x/y", Number: 1})
	require.Error(t, err)
}

// A first-time contributor's CI, waiting for a maintainer to approve its
// workflows, which GitHub says as a check run concluded action_required,
// reads as pending, never failed (the rc6 full stage, F1: the test
// account owns its sandbox, whose pull requests never wait).
func TestCIAwaitingApprovalReadsAsWaiting(t *testing.T) {
	head := strings.Repeat("b", 40)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/pulls/8/reviews"):
			fmt.Fprint(w, `[]`)
		case strings.HasSuffix(r.URL.Path, "/pulls/8"):
			fmt.Fprintf(w, `{"number":8,"state":"open","draft":false,"head":{"sha":%q}}`, head)
		case strings.HasSuffix(r.URL.Path, "/check-runs"):
			fmt.Fprint(w, `{"total_count":2,"check_runs":[{"name":"Build ports (macos-14)","status":"completed","conclusion":"action_required"},{"name":"Build ports (macos-15)","status":"completed","conclusion":"action_required"}]}`)
		case strings.HasSuffix(r.URL.Path, "/status"):
			fmt.Fprint(w, `{"state":"pending","statuses":[]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client := &github.Client{Client: &githubapi.Client{Config: githubapi.Config{BaseURL: server.URL}}}
	status, err := client.Inspect(t.Context(), forge.PullRequestRef{Forge: forge.GitHub, Repository: "macports/macports-ports", Number: 8})
	require.NoError(t, err)
	require.Equal(t, forge.CheckSummary{Total: 2, Pending: 2}, status.Checks)
}

func TestInspectNamesWhoRequestedChangesAndReviewCanBeRequestedAgain(t *testing.T) {
	head := strings.Repeat("a", 40)
	var requested []any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/pulls/7/requested_reviewers"):
			var payload map[string]any
			assert.NoError(t, json.NewDecoder(r.Body).Decode(&payload))
			requested = payload["reviewers"].([]any)
			fmt.Fprint(w, `{"number":7}`)
		case strings.HasSuffix(r.URL.Path, "/pulls/7/reviews"):
			fmt.Fprint(w, `[{"state":"CHANGES_REQUESTED","user":{"login":"ryandesign"},"submitted_at":"2026-10-01T03:00:00Z"},{"state":"CHANGES_REQUESTED","user":{"login":"herbygillot"},"submitted_at":"2026-10-01T05:00:00Z"},{"state":"APPROVED","user":{"login":"carol"},"submitted_at":"2026-10-01T09:00:00Z"}]`)
		case strings.HasSuffix(r.URL.Path, "/pulls/7"):
			fmt.Fprintf(w, `{"number":7,"state":"open","head":{"sha":%q}}`, head)
		case strings.HasSuffix(r.URL.Path, "/check-runs"):
			fmt.Fprint(w, `{"total_count":0,"check_runs":[]}`)
		case strings.HasSuffix(r.URL.Path, "/status"):
			fmt.Fprint(w, `{"state":"success","statuses":[]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client := &github.Client{Client: &githubapi.Client{Config: githubapi.Config{BaseURL: server.URL, Token: "fixture-token"}}}
	ref := forge.PullRequestRef{Forge: forge.GitHub, Repository: "macports/macports-ports", Number: 7}
	status, err := client.Inspect(t.Context(), ref)
	require.NoError(t, err)
	require.Equal(t, "changes-requested", status.Review)
	require.Equal(t, []string{"herbygillot", "ryandesign"}, status.ChangesRequestedBy)
	require.Equal(t, time.Date(2026, 10, 1, 5, 0, 0, 0, time.UTC), status.ReviewedAt, "the latest request for changes, not carol's later approval (field testing, 2026-10-02)")
	require.NoError(t, client.RequestReviewers(t.Context(), ref, status.ChangesRequestedBy))
	require.Equal(t, []any{"herbygillot", "ryandesign"}, requested)
}
