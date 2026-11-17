package github_test

import (
	"encoding/json"
	"fmt"
	"io"
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
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/pulls/7/reviews"):
			fmt.Fprint(w, `[{"state":"CHANGES_REQUESTED","user":{"login":"alice"},"submitted_at":"2026-11-01T10:00:00Z"},{"state":"APPROVED","user":{"login":"alice"},"submitted_at":"2026-11-02T10:00:00Z"},{"state":"COMMENTED","user":{"login":"bob"},"submitted_at":"2026-11-03T10:00:00Z"},{"state":"APPROVED","user":{"login":"carol"}},{"state":"DISMISSED","user":{"login":"carol"}}]`)
		case strings.HasSuffix(r.URL.Path, "/pulls/7/requested_reviewers"):
			fmt.Fprint(w, `{"users":[{"login":"dave"},{"login":"bob"}],"teams":[{"slug":"ports"}]}`)
		case strings.HasSuffix(r.URL.Path, "/pulls/7"):
			fmt.Fprintf(w, `{"number":7,"state":"open","draft":false,"mergeable":false,"mergeable_state":"dirty","head":{"sha":%q}}`, head)
		case strings.HasSuffix(r.URL.Path, "/check-runs"):
			fmt.Fprint(w, `{"total_count":3,"check_runs":[{"name":"Build ports (macos-14)","status":"completed","conclusion":"failure"},{"name":"Build ports (macos-15)","status":"completed","conclusion":"success"},{"name":"Lint","status":"in_progress"}]}`)
		case strings.HasSuffix(r.URL.Path, "/status"):
			fmt.Fprint(w, `{"state":"failure","statuses":[{"context":"buildbot/ports-13","state":"error"}]}`)
		default:
			http.NotFound(w, r)
		}
	})
	client := &github.Client{Client: &githubapi.Client{Config: githubapi.Config{BaseURL: "https://example.test/"}, HTTP: &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, r)
		return response.Result(), nil
	})}}}
	status, err := client.Inspect(t.Context(), forge.PullRequestRef{Forge: forge.GitHub, Repository: "macports/macports-ports", Number: 7})
	require.NoError(t, err)
	require.Equal(t, "no", status.Mergeable)
	require.Equal(t, "dirty", status.MergeableDetail)
	require.Equal(t, "approved", status.Review, "the latest review per reviewer counts, and dismissed reviews do not")
	require.Equal(t, 1, status.Approvals)
	require.Equal(t, forge.CheckSummary{Total: 4, Passed: 1, Failed: 2, Pending: 1, Failing: []string{"Build ports (macos-14)", "buildbot/ports-13"}}, status.Checks)
	// The reviews still requested, the whole list, and each login's own
	// latest review of any kind, a comment-only one included (#191).
	require.True(t, status.RequestedRead)
	require.Equal(t, []string{"bob", "dave"}, status.Requested)
	require.Equal(t, map[string]time.Time{"alice": time.Date(2026, 11, 2, 10, 0, 0, 0, time.UTC), "bob": time.Date(2026, 11, 3, 10, 0, 0, 0, time.UTC)}, status.LatestReview)
	_, err = client.Inspect(t.Context(), forge.PullRequestRef{Forge: "gitlab", Repository: "x/y", Number: 1})
	require.Error(t, err)
}

// A first-time contributor's CI, waiting for a maintainer to approve its
// workflows, which GitHub says as a check run concluded action_required,
// reads as pending, never failed (the rc6 full stage, F1: the test
// account owns its sandbox, whose pull requests never wait).
func TestCIAwaitingApprovalReadsAsWaiting(t *testing.T) {
	head := strings.Repeat("b", 40)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/pulls/8/reviews"):
			fmt.Fprint(w, `[]`)
		case strings.HasSuffix(r.URL.Path, "/pulls/8/requested_reviewers"):
			fmt.Fprint(w, `{"users":[],"teams":[]}`)
		case strings.HasSuffix(r.URL.Path, "/pulls/8"):
			fmt.Fprintf(w, `{"number":8,"state":"open","draft":false,"head":{"sha":%q}}`, head)
		case strings.HasSuffix(r.URL.Path, "/check-runs"):
			fmt.Fprint(w, `{"total_count":2,"check_runs":[{"name":"Build ports (macos-14)","status":"completed","conclusion":"action_required"},{"name":"Build ports (macos-15)","status":"completed","conclusion":"action_required"}]}`)
		case strings.HasSuffix(r.URL.Path, "/status"):
			fmt.Fprint(w, `{"state":"pending","statuses":[]}`)
		default:
			http.NotFound(w, r)
		}
	})
	client := &github.Client{Client: &githubapi.Client{Config: githubapi.Config{BaseURL: "https://example.test/"}, HTTP: &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, r)
		return response.Result(), nil
	})}}}
	status, err := client.Inspect(t.Context(), forge.PullRequestRef{Forge: forge.GitHub, Repository: "macports/macports-ports", Number: 8})
	require.NoError(t, err)
	require.Equal(t, forge.CheckSummary{Total: 2, Pending: 2}, status.Checks)
}

func TestInspectNamesWhoRequestedChangesAndReviewCanBeRequestedAgain(t *testing.T) {
	head := strings.Repeat("a", 40)
	var requested []any
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/user":
			fmt.Fprint(w, `{"login":"ada"}`)
		case strings.HasSuffix(r.URL.Path, "/issues/7/comments"):
			fmt.Fprint(w, `[]`)
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/pulls/7/requested_reviewers"):
			var payload map[string]any
			assert.NoError(t, json.NewDecoder(r.Body).Decode(&payload))
			requested = payload["reviewers"].([]any)
			fmt.Fprint(w, `{"number":7}`)
		case strings.HasSuffix(r.URL.Path, "/pulls/7/requested_reviewers"):
			fmt.Fprint(w, `{"users":[],"teams":[]}`)
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
	})
	client := &github.Client{Client: &githubapi.Client{Config: githubapi.Config{BaseURL: "https://example.test/", Token: "fixture-token"}, HTTP: &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, r)
		return response.Result(), nil
	})}}}
	ref := forge.PullRequestRef{Forge: forge.GitHub, Repository: "macports/macports-ports", Number: 7}
	status, err := client.Inspect(t.Context(), ref)
	require.NoError(t, err)
	require.Equal(t, "changes-requested", status.Review)
	require.Equal(t, []string{"herbygillot", "ryandesign"}, status.ChangesRequestedBy)
	require.Equal(t, time.Date(2026, 10, 1, 5, 0, 0, 0, time.UTC), status.ReviewedAt, "the latest request for changes, not carol's later approval (field testing, 2026-10-02)")
	require.NoError(t, client.RequestReviewers(t.Context(), ref, status.ChangesRequestedBy))
	require.Equal(t, []any{"herbygillot", "ryandesign"}, requested)
}

// Inspect reads the same conversation as the bulk reader (#248).
func TestInspectConversation(t *testing.T) {
	for _, tc := range []struct{ name, comments, want string }{
		{"adopted reply", `[{"user":{"type":"User","login":"ada"},"created_at":"2026-11-06T00:00:00Z"}]`, ""},
		{"bot after reviewer", `[{"user":{"type":"User","login":"reviewer"},"created_at":"2026-11-05T00:00:00Z"},{"user":{"type":"Bot","login":"bot"},"created_at":"2026-11-06T00:00:00Z"}]`, "reviewer"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := &github.Client{Client: &githubapi.Client{Config: githubapi.Config{BaseURL: "https://example.test/", Token: "fixture-token"}, HTTP: &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
				body := `{}`
				switch {
				case r.URL.Path == "/user":
					body = `{"login":"ada"}`
				case strings.HasSuffix(r.URL.Path, "/issues/7/comments"):
					body = tc.comments
				case strings.HasSuffix(r.URL.Path, "/reviews"):
					body = `[]`
				case strings.HasSuffix(r.URL.Path, "/requested_reviewers"):
					body = `{"users":[]}`
				case strings.HasSuffix(r.URL.Path, "/pulls/7"):
					body = `{"number":7,"head":{"sha":"abc"}}`
				case strings.HasSuffix(r.URL.Path, "/check-runs"):
					body = `{"check_runs":[]}`
				case strings.HasSuffix(r.URL.Path, "/status"):
					body = `{"statuses":[]}`
				default:
					t.Errorf("unexpected request %s", r.URL.Path)
				}
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
			})}}}
			status, err := client.Inspect(t.Context(), forge.PullRequestRef{Forge: forge.GitHub, Repository: "owner/repo", Number: 7})
			require.NoError(t, err)
			require.True(t, status.CommentsRead)
			require.Equal(t, tc.want, status.CommentedBy)
			if tc.want != "" {
				require.Equal(t, time.Date(2026, 11, 5, 0, 0, 0, 0, time.UTC), status.CommentedAt)
			}
		})
	}
}

// Without an account, Inspect still reads the pull request but cannot say
// whether its conversation awaits the account's answer (#248).
func TestInspectWithoutCredential(t *testing.T) {
	for _, token := range []string{"", "fixture-token"} {
		t.Run(map[bool]string{true: "no credential", false: "identity unavailable"}[token == ""], func(t *testing.T) {
			client := &github.Client{Client: &githubapi.Client{Config: githubapi.Config{BaseURL: "https://example.test/", Token: token}, HTTP: &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
				var body string
				if r.URL.Path == "/user" {
					return &http.Response{StatusCode: 500, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{}`)), Request: r}, nil
				}
				switch {
				case strings.HasSuffix(r.URL.Path, "/pulls/7"):
					body = `{"number":7,"mergeable":false,"head":{"sha":"abc"}}`
				case strings.HasSuffix(r.URL.Path, "/reviews"):
					body = `[{"state":"APPROVED","user":{"login":"reviewer"},"submitted_at":"2026-11-03T10:00:00Z"}]`
				case strings.HasSuffix(r.URL.Path, "/requested_reviewers"):
					body = `{"users":[{"login":"bob"}]}`
				case strings.HasSuffix(r.URL.Path, "/check-runs"):
					body = `{"check_runs":[{"name":"build","status":"completed","conclusion":"failure"}]}`
				case strings.HasSuffix(r.URL.Path, "/status"):
					body = `{"statuses":[]}`
				default:
					t.Errorf("unexpected request %s", r.URL.Path)
				}
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
			})}}}
			status, err := client.Inspect(t.Context(), forge.PullRequestRef{Forge: forge.GitHub, Repository: "owner/repo", Number: 7})
			require.NoError(t, err)
			require.False(t, status.CommentsRead)
			require.Empty(t, status.CommentedBy)
			require.Equal(t, "no", status.Mergeable)
			require.Equal(t, "approved", status.Review)
			require.Equal(t, []string{"bob"}, status.Requested)
			require.Equal(t, forge.CheckSummary{Total: 1, Failed: 1, Failing: []string{"build"}}, status.Checks)
		})
	}
}
