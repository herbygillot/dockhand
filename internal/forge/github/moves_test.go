package github

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	githubapi "github.com/herbygillot/dockhand/internal/github"
)

// webSite is a stub of GitHub's web: a repository's page answers 200, a
// moved one's a 301 naming where it went, and one past refusal 429. It
// counts the pages asked for, and refuses anything but HEAD.
type webSite struct {
	mu    sync.Mutex
	asked map[string]int
}

func newWebSite(t *testing.T) (*webSite, *httptest.Server) {
	t.Helper()
	site := &webSite{asked: map[string]int{}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		site.mu.Lock()
		site.asked[r.URL.Path]++
		site.mu.Unlock()
		if r.Method != http.MethodHead || r.Header.Get("Authorization") != "" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		switch r.URL.Path {
		case "/owner/project":
			w.WriteHeader(http.StatusOK)
		case "/extrawurst/gitui":
			http.Redirect(w, r, "https://github.com/gitui-org/gitui", http.StatusMovedPermanently)
		case "/cloudflare/wrangler":
			w.Header().Set("Location", "/cloudflare/wrangler-legacy")
			w.WriteHeader(http.StatusMovedPermanently)
		case "/elsewhere/project":
			http.Redirect(w, r, "https://example.com/elsewhere", http.StatusMovedPermanently)
		default:
			w.WriteHeader(http.StatusTooManyRequests)
		}
	}))
	t.Cleanup(server.Close)
	return site, server
}

func (s *webSite) count(path string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.asked[path]
}

// testMoves reads the stub site, its answers kept in a file of the test's,
// by a clock the test moves.
func testMoves(t *testing.T, server *httptest.Server, path string, clock *time.Time) *Moves {
	t.Helper()
	moves := NewMoves(path)
	moves.BaseURL, moves.HTTP, moves.now = server.URL, server.Client(), func() time.Time { return *clock }
	return moves
}

func readMove(t *testing.T, moves *Moves, name string) (string, bool) {
	t.Helper()
	return moves.start(t.Context(), name).wait(t.Context())
}

// A repository's web page answers 200 where it is, and a 301 naming the
// repository it moved to, which the redirect isn't followed to; a 301
// naming somewhere other than GitHub's own web says nothing.
func TestARepositorysWebPageSaysWhereItMoved(t *testing.T) {
	t.Parallel()
	site, server := newWebSite(t)
	clock := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	moves := testMoves(t, server, "", &clock)
	moved, ok := readMove(t, moves, "owner/project")
	require.True(t, ok)
	require.Empty(t, moved, "a 200 is the repository where it was")
	moved, ok = readMove(t, moves, "cloudflare/wrangler")
	require.True(t, ok)
	require.Equal(t, "cloudflare/wrangler-legacy", moved)
	_, ok = readMove(t, moves, "elsewhere/project")
	require.False(t, ok, "a redirect off GitHub's web names no repository")
	_, ok = readMove(t, moves, "extrawurst/gitui")
	require.False(t, ok, "a redirect to another host than the one asked names no repository of its")
	require.Equal(t, 1, site.count("/cloudflare/wrangler"))
	require.Zero(t, site.count("/cloudflare/wrangler-legacy"), "the redirect isn't followed")

	// Neither redirect paused the reads.
	moved, ok = readMove(t, moves, "Owner/Project")
	require.True(t, ok)
	require.Empty(t, moved)
}

// GitHub's web, by default, names github.com in a 301's Location, which
// is read against GitHub's own web where BaseURL is empty.
func TestARedirectOnGitHubsOwnWebNamesTheRepository(t *testing.T) {
	t.Parallel()
	site, server := newWebSite(t)
	clock := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	moves := testMoves(t, server, "", &clock)
	moves.BaseURL = ""
	// Every request goes to the stub, whatever its address says.
	moves.HTTP = &http.Client{Transport: redirectTo(server)}
	moved, ok := readMove(t, moves, "extrawurst/gitui")
	require.True(t, ok)
	require.Equal(t, "gitui-org/gitui", moved)
	require.Equal(t, 1, site.count("/extrawurst/gitui"))
}

// redirectTo sends every request to the stub server.
func redirectTo(server *httptest.Server) http.RoundTripper {
	return roundTripper(func(r *http.Request) (*http.Response, error) {
		r = r.Clone(r.Context())
		r.URL.Scheme, r.URL.Host = "http", server.Listener.Addr().String()
		return server.Client().Transport.RoundTrip(r)
	})
}

type roundTripper func(*http.Request) (*http.Response, error)

func (f roundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// Each answer, a 200's and a 301's alike, stands seven days by the clock,
// in the process and in the next one's file, and is asked again after.
func TestAWebPagesAnswerIsKeptSevenDays(t *testing.T) {
	t.Parallel()
	site, server := newWebSite(t)
	clock := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	path := filepath.Join(t.TempDir(), "github", "moves.json")
	moves := testMoves(t, server, path, &clock)
	_, ok := readMove(t, moves, "owner/project")
	require.True(t, ok)
	moved, ok := readMove(t, moves, "cloudflare/wrangler")
	require.True(t, ok)
	require.Equal(t, "cloudflare/wrangler-legacy", moved)

	clock = clock.Add(7*24*time.Hour - time.Minute)
	for _, next := range []*Moves{moves, testMoves(t, server, path, &clock)} {
		moved, ok = readMove(t, next, "cloudflare/wrangler")
		require.True(t, ok)
		require.Equal(t, "cloudflare/wrangler-legacy", moved)
		_, ok = readMove(t, next, "owner/project")
		require.True(t, ok)
	}
	require.Equal(t, 1, site.count("/owner/project"), "a 200 isn't asked again within seven days")
	require.Equal(t, 1, site.count("/cloudflare/wrangler"), "nor a 301")

	clock = clock.Add(2 * time.Minute)
	_, ok = readMove(t, moves, "owner/project")
	require.True(t, ok)
	_, ok = readMove(t, moves, "cloudflare/wrangler")
	require.True(t, ok)
	require.Equal(t, 2, site.count("/owner/project"), "asked again after seven days")
	require.Equal(t, 2, site.count("/cloudflare/wrangler"))
}

// A 429 ends the reads: what was read stands, the rest is unsaid until
// the pause lifts, and nothing fails.
func TestA429EndsTheLooksWebReads(t *testing.T) {
	t.Parallel()
	site, server := newWebSite(t)
	clock := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	moves := testMoves(t, server, "", &clock)
	moved, ok := readMove(t, moves, "cloudflare/wrangler")
	require.True(t, ok)
	require.Equal(t, "cloudflare/wrangler-legacy", moved)
	_, ok = readMove(t, moves, "throttled/one")
	require.False(t, ok, "a 429 says nothing")
	_, ok = readMove(t, moves, "owner/project")
	require.False(t, ok, "and ends the reads")
	require.Zero(t, site.count("/owner/project"))
	moved, ok = readMove(t, moves, "cloudflare/wrangler")
	require.True(t, ok, "the answers read before stand")
	require.Equal(t, "cloudflare/wrangler-legacy", moved)

	clock = clock.Add(movesPausedFor)
	_, ok = readMove(t, moves, "owner/project")
	require.True(t, ok, "the next look reads again")
	require.Equal(t, 1, site.count("/owner/project"))
}

// A repository whose tags came from git notes the name its web page
// redirects to, once the read is answered; one the API named, or whose
// page said nothing, keeps what it had.
func TestAGitListedRepositoryFindsItsMove(t *testing.T) {
	t.Parallel()
	_, server := newWebSite(t)
	clock := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	moves := testMoves(t, server, "", &clock)
	client := &Client{Client: &githubapi.Client{}, Moves: moves}

	moved := &repository{client: client, name: "cloudflare/wrangler", unread: true}
	moved.move = moves.start(t.Context(), moved.name)
	moved.FindMove(t.Context())
	require.Equal(t, "cloudflare/wrangler-legacy", moved.AnswersAs())

	named := &repository{client: client, name: "cloudflare/wrangler", unread: true, answered: "cloudflare/workers-sdk"}
	named.move = moves.start(t.Context(), named.name)
	named.FindMove(t.Context())
	require.Equal(t, "cloudflare/workers-sdk", named.AnswersAs(), "the API's answer stands")

	unasked := &repository{client: client, name: "cloudflare/wrangler"}
	unasked.FindMove(t.Context())
	require.Empty(t, unasked.AnswersAs(), "nothing was read where its tags didn't come from git")

	refused := &repository{client: client, name: "throttled/two", unread: true}
	refused.move = moves.start(t.Context(), refused.name)
	refused.FindMove(t.Context())
	require.Empty(t, refused.AnswersAs())
}

// A test's or an enterprise server's forge, with a base or clone URL of
// its own, lists tags through the API, and asks no web page.
func TestAConfiguredForgeAsksNoWebPage(t *testing.T) {
	t.Parallel()
	site, web := newWebSite(t)
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"name":"v1.0","commit":{"sha":"` + "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" + `"}}]`))
	}))
	t.Cleanup(api.Close)
	clock := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	client := &Client{Client: &githubapi.Client{HTTP: api.Client(), Config: githubapi.Config{BaseURL: api.URL + "/"}}, Moves: testMoves(t, web, "", &clock)}
	r := &repository{client: client, name: "owner/project"}
	tags, err := r.ListTags(t.Context())
	require.NoError(t, err)
	require.Len(t, tags, 1)
	r.FindMove(t.Context())
	require.Zero(t, site.count("/owner/project"))
	require.Nil(t, r.move)
}
