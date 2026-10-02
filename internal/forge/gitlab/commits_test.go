package gitlab_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/forge"
	forgegitlab "github.com/herbygillot/dockhand/internal/forge/gitlab"
)

// A commit's time on GitLab is its committed date; a commit GitLab hasn't
// is not found, and one with no date is refused (the test plan's step 3).
func TestGitLabsCommitTime(t *testing.T) {
	t.Parallel()
	commit := strings.Repeat("a", 40)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/repository/commits/"+commit):
			fmt.Fprintf(w, `{"id":%q,"committed_date":"2026-09-30T12:34:56Z"}`, commit)
		case strings.HasSuffix(r.URL.Path, "/repository/commits/"+strings.Repeat("c", 40)):
			fmt.Fprintf(w, `{"id":%q}`, strings.Repeat("c", 40))
		default:
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprint(w, `{"message":"404 Commit Not Found"}`)
		}
	}))
	defer server.Close()
	repository, err := (&forgegitlab.Client{HTTP: server.Client()}).Repository(server.URL, "group/project")
	require.NoError(t, err)
	dated := repository.(forge.DatedRepository)
	when, err := dated.CommitTime(t.Context(), commit)
	require.NoError(t, err)
	require.Equal(t, time.Date(2026, 9, 30, 12, 34, 56, 0, time.UTC), when.UTC())
	_, err = dated.CommitTime(t.Context(), strings.Repeat("b", 40))
	require.ErrorIs(t, err, forge.ErrNotFound)
	_, err = dated.CommitTime(t.Context(), strings.Repeat("c", 40))
	require.ErrorContains(t, err, "has no date")
	_, err = dated.CommitTime(t.Context(), "main")
	require.ErrorContains(t, err, "invalid commit")
}
