package github

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	githubapi "github.com/herbygillot/dockhand/internal/github"
)

// The name GitHub answered a listing as is read from the URLs it gave, a
// tag's zipball or a release's page; one that names the repository asked
// is no answer of another's.
func TestTheNameAListingAnsweredAsIsRead(t *testing.T) {
	t.Parallel()
	r := &repository{name: "acorn-io/acorn"}
	require.Empty(t, r.AnswersAs())
	r.answer("https://api.github.com/repos/obot-platform/obot/zipball/refs/tags/v0.27.0")
	require.Equal(t, "obot-platform/obot", r.AnswersAs())
	r.answer("https://github.com/someone/else/releases/tag/v1")
	require.Equal(t, "obot-platform/obot", r.AnswersAs(), "the first answer stands")

	same := &repository{name: "jqlang/jq"}
	same.answer("https://github.com/JQLang/jq/releases/tag/jq-1.8.1")
	require.Empty(t, same.AnswersAs(), "the same name, however cased")
	same.answer("not a url")
	require.Empty(t, same.AnswersAs())
}

// A repository whose tags came from git, which serves a moved repository
// under its old name, reads the name it answers by from the API once, when
// asked; one an API listing named, or whose tags the API listed, asks
// nothing.
func TestAGitListedRepositoryConfirmsItsName(t *testing.T) {
	t.Parallel()
	asked := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked++
		if r.URL.Path != "/repos/acorn-io/acorn" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
			return
		}
		fmt.Fprint(w, `{"full_name": "obot-platform/obot"}`)
	}))
	defer server.Close()
	client := &Client{Client: &githubapi.Client{Config: githubapi.Config{BaseURL: server.URL + "/", Token: "fixture-token"}}}
	r := &repository{client: client, name: "acorn-io/acorn"}
	require.NoError(t, r.ConfirmName(t.Context()))
	require.Zero(t, asked, "the API listed its tags, so nothing is asked")

	r.unread = true
	require.NoError(t, r.ConfirmName(t.Context()))
	require.Equal(t, "obot-platform/obot", r.AnswersAs())
	require.NoError(t, r.ConfirmName(t.Context()))
	require.Equal(t, 1, asked, "asked once")
}
