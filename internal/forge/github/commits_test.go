package github_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/forge"
	"github.com/herbygillot/dockhand/internal/forge/github"
	githubapi "github.com/herbygillot/dockhand/internal/github"
)

// GitHub is asked for a commit by a revision expression, which its
// GraphQL API documents as "suitable for rev-parse": a whole commit name,
// or the twelve characters a Generated-By's build abbreviates it to, which
// its REST API documents no lookup by. A commit it doesn't have, an object
// that isn't one, and its refusal are each told apart, and a name that is
// no commit's isn't sent.
func TestHasCommitAsksGitHubByARevisionExpression(t *testing.T) {
	full := "2bbcfdb76480" + strings.Repeat("a", 28)
	var asked []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/graphql" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			return
		}
		var payload struct {
			Query     string
			Variables map[string]string
		}
		assert.NoError(t, json.NewDecoder(r.Body).Decode(&payload))
		assert.Contains(t, payload.Query, "object(expression: $expression) { ... on Commit { oid } }")
		assert.Equal(t, "herbygillot", payload.Variables["owner"])
		assert.Equal(t, "dockhand", payload.Variables["name"])
		asked = append(asked, payload.Variables["expression"])
		switch payload.Variables["expression"] {
		case full, "2bbcfdb76480":
			fmt.Fprintf(w, `{"data": {"repository": {"object": {"oid": %q}}}}`, full)
		case "1a2b3c4d5e6f":
			fmt.Fprint(w, `{"data": {"repository": {"object": null}}}`)
		case "3c4d5e6f7a8b":
			// A tree's name begins so: no field of a commit is read.
			fmt.Fprint(w, `{"data": {"repository": {"object": {}}}}`)
		default:
			fmt.Fprint(w, `{"data": null, "errors": [{"type": "RATE_LIMITED", "message": "API rate limit exceeded for user ID 1."}]}`)
		}
	}))
	defer server.Close()
	client := &github.Client{Client: &githubapi.Client{Config: githubapi.Config{BaseURL: server.URL, Token: "fixture-token"}}}
	repository, err := client.Repository("https://github.com", "herbygillot/dockhand")
	require.NoError(t, err)
	commits, ok := repository.(forge.CommitRepository)
	require.True(t, ok)

	for _, test := range []struct {
		commit string
		has    bool
	}{{full, true}, {"2bbcfdb76480", true}, {"1a2b3c4d5e6f", false}, {"3c4d5e6f7a8b", false}} {
		has, err := commits.HasCommit(t.Context(), test.commit)
		require.NoError(t, err, test.commit)
		require.Equal(t, test.has, has, test.commit)
	}
	_, err = commits.HasCommit(t.Context(), "4d5e6f7a8b9c")
	require.EqualError(t, err, "github: API rate limit exceeded for user ID 1.")
	for _, name := range []string{"v0.9.0", "HEAD~1", "2BBCFDB76480", "abc"} {
		_, err = commits.HasCommit(t.Context(), name)
		require.ErrorContains(t, err, "github: invalid commit", name)
	}
	require.Equal(t, []string{full, "2bbcfdb76480", "1a2b3c4d5e6f", "3c4d5e6f7a8b", "4d5e6f7a8b9c"}, asked)

	// GitHub's GraphQL API answers only a signed-in request.
	anonymous := &github.Client{Client: &githubapi.Client{Config: githubapi.Config{BaseURL: server.URL}}}
	repository, err = anonymous.Repository("https://github.com", "herbygillot/dockhand")
	require.NoError(t, err)
	_, err = repository.(forge.CommitRepository).HasCommit(t.Context(), full)
	require.ErrorIs(t, err, forge.ErrAuthentication)
	require.Len(t, asked, 5)
}
