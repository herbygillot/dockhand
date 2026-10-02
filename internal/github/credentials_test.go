package github_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	gh "github.com/google/go-github/v91/github"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/github"
)

// tokens is a source giving each token in turn, the last one again once
// they run out, counting what it's asked.
type tokens struct {
	mu     sync.Mutex
	source github.CredentialSource
	given  []string
	asked  int
}

func (s *tokens) Token(context.Context) (github.Token, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	secret := s.given[min(s.asked, len(s.given)-1)]
	s.asked++
	return github.Token{Secret: secret, Source: s.source}, nil
}

// accepting is a GitHub that accepts the tokens named, and rejects the
// rest, recording what each request carried and its body.
func accepting(t *testing.T, accepted ...string) (*httptest.Server, *[]string) {
	var mu sync.Mutex
	var seen []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		seen = append(seen, r.Method+" "+r.Header.Get("Authorization")+" "+string(body))
		mu.Unlock()
		for _, token := range accepted {
			if r.Header.Get("Authorization") == "Bearer "+token {
				w.Header().Set("Content-Type", "application/json")
				if r.Method == http.MethodPost {
					_, _ = io.WriteString(w, `{"id": 1}`)
					return
				}
				_, _ = io.WriteString(w, `{"login": "ada"}`)
				return
			}
		}
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(server.Close)
	return server, &seen
}

func client(server *httptest.Server, source github.TokenSource) *github.Client {
	return &github.Client{HTTP: server.Client(), Config: github.Config{BaseURL: server.URL + "/"}, Credentials: source}
}

// A token GitHub rejects is asked for anew, and the request tried again
// with the new one, a write's body with it; GitHub did nothing with the
// rejected request (the GitHub auth flow review's plan, step 1).
func TestARejectedTokenIsReplacedAndTheRequestTriedAgain(t *testing.T) {
	server, seen := accepting(t, "B")
	source := &tokens{source: github.SourceKeychain, given: []string{"A", "B"}}
	c := client(server, source)
	login, err := c.AuthenticatedUser(t.Context())
	require.NoError(t, err)
	require.Equal(t, "ada", login)
	require.Equal(t, []string{"GET Bearer A ", "GET Bearer B "}, *seen)

	*seen = nil
	api, err := c.AuthenticatedAPI(t.Context())
	require.NoError(t, err)
	_, _, err = api.Issues.CreateComment(t.Context(), "macports", "macports-ports", 1, gh.IssueCommentRequest{Body: "hello"})
	require.NoError(t, err)
	require.Len(t, *seen, 1, "B is held now")
	require.Contains(t, (*seen)[0], "POST Bearer B ")
}

// A write rejected is tried again with its body.
func TestARejectedWriteIsTriedAgainWithItsBody(t *testing.T) {
	server, seen := accepting(t, "B")
	source := &tokens{source: github.SourceGitHubCLI, given: []string{"A", "B"}}
	api, err := client(server, source).AuthenticatedAPI(t.Context())
	require.NoError(t, err)
	_, _, err = api.Issues.CreateComment(t.Context(), "macports", "macports-ports", 1, gh.IssueCommentRequest{Body: "hello"})
	require.NoError(t, err)
	require.Len(t, *seen, 2)
	require.True(t, strings.HasPrefix((*seen)[0], "POST Bearer A {"), (*seen)[0])
	require.True(t, strings.HasPrefix((*seen)[1], "POST Bearer B {"), (*seen)[1])
	require.Equal(t, strings.TrimPrefix((*seen)[0], "POST Bearer A "), strings.TrimPrefix((*seen)[1], "POST Bearer B "), "the same body")
}

// A second rejection is the rejection, and a token from the environment
// is never replaced: it's the person's to change.
func TestATokenRejectedAgainOrFromTheEnvironmentIsRefused(t *testing.T) {
	server, seen := accepting(t)
	_, err := client(server, &tokens{source: github.SourceKeychain, given: []string{"A", "B"}}).AuthenticatedUser(t.Context())
	require.ErrorIs(t, err, github.ErrAuthentication)
	require.ErrorContains(t, err, "GitHub rejected the credential from Dockhand macOS Keychain")
	require.Len(t, *seen, 2)

	*seen = nil
	environment := &tokens{source: github.SourceGHEnvironment, given: []string{"A", "B"}}
	_, err = client(server, environment).AuthenticatedUser(t.Context())
	require.ErrorContains(t, err, "GitHub rejected the credential from GH_TOKEN")
	require.Len(t, *seen, 1, "never tried again")
	require.Equal(t, 1, environment.asked)
}

// A client already running carries a token its source has since replaced,
// once the one it holds expires, with nothing made again: serve made its
// client once, and a new login reached it only on a restart.
func TestARunningClientCarriesARenewedToken(t *testing.T) {
	server, seen := accepting(t, "A", "B")
	source := &renewing{}
	c := client(server, source)
	_, err := c.AuthenticatedUser(t.Context())
	require.NoError(t, err)
	source.next()
	_, err = c.AuthenticatedUser(t.Context())
	require.NoError(t, err)
	require.Equal(t, []string{"GET Bearer A ", "GET Bearer B "}, *seen)
	require.Equal(t, github.SourceKeychain, c.CredentialSource())
}

// renewing is a login whose token expires at once, so the client asks it
// again for each request; next replaces its token.
type renewing struct {
	mu     sync.Mutex
	secret string
}

func (r *renewing) Token(context.Context) (github.Token, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.secret == "" {
		r.secret = "A"
	}
	return github.Token{Secret: r.secret, Source: github.SourceKeychain, Expiry: time.Now().Add(time.Minute)}, nil
}

func (r *renewing) next() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.secret = "B"
}
