package github_test

import (
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/herbygillot/dockhand/internal/forge"
	"github.com/herbygillot/dockhand/internal/forge/github"
	githubapi "github.com/herbygillot/dockhand/internal/github"
	"github.com/stretchr/testify/require"
)

// cloneBase makes base/owner/project.git with an annotated tag v2.0 and
// returns base and the commit the tag names.
func cloneBase(t *testing.T) (string, string) {
	t.Helper()
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	base := t.TempDir()
	remote := filepath.Join(base, "owner", "project.git")
	require.NoError(t, os.MkdirAll(remote, 0o755))
	run := func(args ...string) string {
		t.Helper()
		out, err := exec.CommandContext(t.Context(), "git", append([]string{"-C", remote, "-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid"}, args...)...).CombinedOutput()
		require.NoError(t, err, "%s", out)
		return strings.TrimSpace(string(out))
	}
	run("init", "--bare", "-q")
	commit := run("commit-tree", run("mktree"), "-m", "release")
	run("tag", "-a", "-m", "release", "v2.0", commit)
	return base, commit
}

func refusingClient(t *testing.T, status int, base string) *github.Client {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(status) }))
	t.Cleanup(server.Close)
	return &github.Client{Client: &githubapi.Client{Config: githubapi.Config{BaseURL: server.URL, CloneURL: base}}}
}

// An API that refuses to answer, as a rate limit or a proxy does, leaves the
// tags to git, which reads the same tags the API would have listed.
func TestTagsFallBackToGitWhenTheAPICannotAnswer(t *testing.T) {
	base, commit := cloneBase(t)
	repository := testRepository(t, refusingClient(t, http.StatusForbidden, base))
	tag, err := repository.Tag(t.Context(), "v2.0")
	require.NoError(t, err)
	require.Equal(t, forge.Tag{Name: "v2.0", Commit: commit}, tag)
	tags, err := repository.ListTags(t.Context())
	require.NoError(t, err)
	require.Equal(t, []forge.Tag{{Name: "v2.0", Commit: commit}}, tags)
	_, err = repository.Tag(t.Context(), "v9.9")
	require.ErrorIs(t, err, forge.ErrNotFound, "git's answer that the tag is absent is an answer")
}

// The API's 404 is its answer that the tag does not exist, so git is not
// asked; a failure of both keeps the API's cause.
func TestAMissingTagIsTheAPIsAnswerAndBothFailuresAreKept(t *testing.T) {
	base, _ := cloneBase(t)
	_, err := testRepository(t, refusingClient(t, http.StatusNotFound, base)).Tag(t.Context(), "v2.0")
	require.ErrorIs(t, err, forge.ErrNotFound)
	refused := testRepository(t, refusingClient(t, http.StatusForbidden, t.TempDir()))
	_, err = refused.Tag(t.Context(), "v2.0")
	require.ErrorContains(t, err, "403")
	require.ErrorContains(t, err, "with git")
	require.NotErrorIs(t, err, forge.ErrNotFound)
	_, err = refused.ListTags(t.Context())
	require.ErrorContains(t, err, "403")
	require.ErrorContains(t, err, "with git")
}
