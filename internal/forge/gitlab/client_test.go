package gitlab_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/herbygillot/dockhand/internal/forge"
	forgegitlab "github.com/herbygillot/dockhand/internal/forge/gitlab"
	"github.com/stretchr/testify/require"
)

func TestRepositoryMapsInstancePathIntoGitLabProjectIdentity(t *testing.T) {
	commit := strings.Repeat("a", 40)
	var escapedPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		escapedPath = r.URL.EscapedPath()
		json.NewEncoder(w).Encode(map[string]any{"name": "v2.0", "commit": map[string]string{"id": commit}})
	}))
	defer server.Close()
	client := forgegitlab.Client{HTTP: server.Client()}
	repository, err := client.Repository(server.URL+"/tpo", "core/project")
	require.NoError(t, err)
	require.Equal(t, "core/project", repository.Name())
	tag, err := repository.Tag(t.Context(), "v2.0")
	require.NoError(t, err)
	require.Equal(t, forge.Tag{Name: "v2.0", Commit: commit}, tag)
	require.Equal(t, "/api/v4/projects/tpo%2Fcore%2Fproject/repository/tags/v2%2E0", escapedPath)
}

// Every page is read, each asking for GitLab's largest, 100 tags.
func TestRepositoryReadsEveryGitLabTagPage(t *testing.T) {
	commits := []string{strings.Repeat("a", 40), strings.Repeat("b", 40)}
	var sizes []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sizes = append(sizes, r.URL.Query().Get("per_page"))
		page := r.URL.Query().Get("page")
		index := 0
		if page == "2" {
			index = 1
		} else {
			w.Header().Set("X-Next-Page", "2")
			w.Header().Set("X-Page", "1")
		}
		json.NewEncoder(w).Encode([]any{map[string]any{"name": fmt.Sprintf("v%d.0", index+1), "commit": map[string]string{"id": commits[index]}}})
	}))
	defer server.Close()
	repository, err := (&forgegitlab.Client{HTTP: server.Client()}).Repository(server.URL, "owner/project")
	require.NoError(t, err)
	tags, err := repository.ListTags(t.Context())
	require.NoError(t, err)
	require.Equal(t, []forge.Tag{{Name: "v1.0", Commit: commits[0]}, {Name: "v2.0", Commit: commits[1]}}, tags)
	require.Equal(t, []string{"100", "100"}, sizes)
}

func TestRepositoryRejectsDuplicateGitLabTags(t *testing.T) {
	commit := strings.Repeat("a", 40)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode([]any{
			map[string]any{"name": "v1.0", "commit": map[string]string{"id": commit}},
			map[string]any{"name": "v1.0", "commit": map[string]string{"id": commit}},
		})
	}))
	defer server.Close()
	repository, err := (&forgegitlab.Client{HTTP: server.Client()}).Repository(server.URL, "owner/project")
	require.NoError(t, err)
	_, err = repository.ListTags(t.Context())
	require.ErrorIs(t, err, forge.ErrIncomplete)
}

func TestGitLabTagFailuresRemainDistinct(t *testing.T) {
	for _, test := range []struct {
		name    string
		status  int
		body    string
		missing bool
	}{
		{name: "missing", status: http.StatusNotFound, missing: true},
		{name: "rejected", status: http.StatusForbidden},
		{name: "invalid", status: http.StatusOK, body: `{"name":"v2","commit":{"id":"short"}}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(test.status)
				fmt.Fprint(w, test.body)
			}))
			defer server.Close()
			repository, err := (&forgegitlab.Client{HTTP: server.Client()}).Repository(server.URL, "owner/project")
			require.NoError(t, err)
			_, err = repository.Tag(t.Context(), "v2")
			require.Error(t, err)
			if test.missing {
				require.ErrorIs(t, err, forge.ErrNotFound)
			} else {
				require.NotErrorIs(t, err, forge.ErrNotFound)
			}
		})
	}
}

// GitLab's rate limit, a 429 with RateLimit-Reset an hour away, is said
// at once as rate-limited until then, where the SDK waited for the reset
// and a look sat an hour on zix (the GraphQL item's baseline, 2026-10-30).
// A server's error is retried, a bounded number of times.
func TestARateLimitIsSaidUntilItsReset(t *testing.T) {
	t.Parallel()
	reset := time.Now().Add(time.Hour).Truncate(time.Second)
	asked := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked++
		w.Header().Set("RateLimit-Reset", strconv.FormatInt(reset.Unix(), 10))
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server.Close()
	client := forgegitlab.Client{HTTP: server.Client()}
	repository, err := client.Repository(server.URL, "drobilla/zix")
	require.NoError(t, err)
	start := time.Now()
	_, err = repository.ListTags(t.Context())
	require.Less(t, time.Since(start), 5*time.Second, "said at once, not waited for")
	limited := new(forge.RateLimitError)
	require.ErrorAs(t, err, &limited)
	require.True(t, limited.RetryAt.Equal(reset), "%v", limited.RetryAt)
	require.Equal(t, 1, asked, "a rate limit isn't retried")

	failing := 0
	broken := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		failing++
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer broken.Close()
	brokenClient := forgegitlab.Client{HTTP: broken.Client()}
	repository, err = brokenClient.Repository(broken.URL, "drobilla/zix")
	require.NoError(t, err)
	_, err = repository.ListTags(t.Context())
	require.Error(t, err)
	require.Equal(t, 4, failing, "the first ask and three retries")
}
