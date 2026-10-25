package github

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	gh "github.com/google/go-github/v93/github"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/forge"
	"github.com/herbygillot/dockhand/internal/progress"
	"github.com/herbygillot/dockhand/internal/refusal"
)

// limitedServer answers each request with the next of its replies, the
// last again once they run out, and counts what it was asked.
type limitedServer struct {
	mu      sync.Mutex
	replies []func(http.ResponseWriter)
	asked   []string
}

func (s *limitedServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	reply := s.replies[min(len(s.asked), len(s.replies)-1)]
	s.asked = append(s.asked, r.Method+" "+r.URL.Path)
	s.mu.Unlock()
	reply(w)
}

func primaryRefusal(reset time.Time) func(http.ResponseWriter) {
	return func(w http.ResponseWriter) {
		w.Header().Set("X-RateLimit-Remaining", "0")
		w.Header().Set("X-RateLimit-Reset", fmt.Sprint(reset.Unix()))
		w.WriteHeader(http.StatusForbidden)
		fmt.Fprint(w, `{"message":"API rate limit exceeded"}`)
	}
}

func answer(remaining string, reset time.Time) func(http.ResponseWriter) {
	return func(w http.ResponseWriter) {
		w.Header().Set("X-RateLimit-Remaining", remaining)
		w.Header().Set("X-RateLimit-Reset", fmt.Sprint(reset.Unix()))
		fmt.Fprint(w, `{"full_name":"owner/project"}`)
	}
}

// limitedClient is a client of the server, with a login or without, whose
// waits are recorded rather than slept.
func limitedClient(t *testing.T, server *limitedServer, token string) (*Client, *[]time.Duration) {
	t.Helper()
	address := httptest.NewServer(server)
	t.Cleanup(address.Close)
	var waits []time.Duration
	saved := limitSleep
	t.Cleanup(func() { limitSleep = saved })
	limitSleep = func(ctx context.Context, d time.Duration) error {
		waits = append(waits, d)
		return ctx.Err()
	}
	client := &Client{HTTP: address.Client(), Config: Config{BaseURL: address.URL + "/", Token: token}}
	_, err := client.API(t.Context())
	require.NoError(t, err)
	return client, &waits
}

func get(t *testing.T, ctx context.Context, client *Client) error {
	t.Helper()
	api, err := client.API(ctx)
	require.NoError(t, err)
	_, _, err = api.Repositories.Get(ctx, "owner", "project")
	return RateLimitError(err)
}

// A read GitHub's rate limit refused waits for the limit to reset, when
// that's near, says so, and is asked once more; the reset GitHub gives in
// whole seconds is waited a second past.
func TestAReadWaitsOutARateLimitThatResetsSoon(t *testing.T) {
	reset := time.Now().Add(30 * time.Second).Truncate(time.Second)
	server := &limitedServer{replies: []func(http.ResponseWriter){primaryRefusal(reset), answer("4999", reset.Add(time.Hour))}}
	client, waits := limitedClient(t, server, "fixture-token")
	var said []string
	ctx := progress.WithReporter(t.Context(), func(u progress.Update) { said = append(said, u.Message) })
	require.NoError(t, get(t, ctx, client))
	require.Len(t, server.asked, 2, "asked again once the limit reset")
	require.Len(t, *waits, 1)
	require.InDelta(t, time.Until(reset.Add(time.Second)).Seconds(), (*waits)[0].Seconds(), 2)
	require.Equal(t, []string{"waiting for GitHub's rate limit for your login to reset at " + reset.Local().Format("15:04:05")}, said)
}

// A limit that lifts further off than two minutes isn't waited for: the
// read is refused with when it resets, in words, and what follows is
// refused without asking GitHub again while it's limited.
func TestALongRateLimitIsSaidWithItsReset(t *testing.T) {
	reset := time.Now().Add(23*time.Minute - time.Second).Truncate(time.Second)
	server := &limitedServer{replies: []func(http.ResponseWriter){primaryRefusal(reset)}}
	client, waits := limitedClient(t, server, "fixture-token")
	err := get(t, t.Context(), client)
	var limited *forge.RateLimitError
	require.ErrorAs(t, err, &limited)
	require.Equal(t, reset, limited.RetryAt)
	require.ErrorContains(t, err, "GitHub's rate limit for your login resets in 23 minutes, at "+reset.Local().Format("15:04 MST"))
	require.Empty(t, *waits)

	err = get(t, t.Context(), client)
	require.ErrorAs(t, err, &limited)
	require.Len(t, server.asked, 1, "a limited client doesn't ask again")
}

// The last request a window allows leaves the limit used up: the next read
// waits for the reset before it's sent, when that's near, and is refused
// before it's sent when it isn't.
func TestAUsedUpLimitIsWaitedBeforeAsking(t *testing.T) {
	reset := time.Now().Add(40 * time.Second).Truncate(time.Second)
	server := &limitedServer{replies: []func(http.ResponseWriter){answer("0", reset), answer("4999", reset.Add(time.Hour))}}
	client, waits := limitedClient(t, server, "fixture-token")
	require.NoError(t, get(t, t.Context(), client))
	require.NoError(t, get(t, t.Context(), client))
	require.Len(t, *waits, 1, "the second read waited for the reset")
	require.Len(t, server.asked, 2)

	far := time.Now().Add(time.Hour).Truncate(time.Second)
	server = &limitedServer{replies: []func(http.ResponseWriter){answer("0", far)}}
	client, waits = limitedClient(t, server, "")
	require.NoError(t, get(t, t.Context(), client))
	err := get(t, t.Context(), client)
	require.ErrorContains(t, err, "GitHub's rate limit for requests without a login resets in")
	require.ErrorContains(t, err, "dockhand setup github raises it")
	require.Len(t, server.asked, 1)
	require.Empty(t, *waits)
}

// A secondary limit's retry-after is waited when it's short, as a 429
// naming no wait is for GitHub's minute; a read refused again after its
// wait is refused, not asked a third time.
func TestASecondaryLimitIsWaitedOnceForItsRetryAfter(t *testing.T) {
	slow := func(w http.ResponseWriter) {
		w.Header().Set("Retry-After", "20")
		w.WriteHeader(http.StatusForbidden)
		fmt.Fprint(w, `{"message":"You have exceeded a secondary rate limit"}`)
	}
	server := &limitedServer{replies: []func(http.ResponseWriter){slow, answer("4000", time.Now().Add(time.Hour))}}
	client, waits := limitedClient(t, server, "fixture-token")
	require.NoError(t, get(t, t.Context(), client))
	require.Len(t, *waits, 1)
	require.InDelta(t, 20, (*waits)[0].Seconds(), 1)

	tooMany := func(w http.ResponseWriter) { w.WriteHeader(http.StatusTooManyRequests) }
	server = &limitedServer{replies: []func(http.ResponseWriter){tooMany}}
	client, waits = limitedClient(t, server, "fixture-token")
	err := get(t, t.Context(), client)
	var limited *forge.RateLimitError
	require.ErrorAs(t, err, &limited)
	require.ErrorContains(t, err, "GitHub's secondary rate limit, on requests made close together, lifts in")
	require.Len(t, server.asked, 2, "asked again once, after the wait")
	require.Len(t, *waits, 1)
	require.InDelta(t, 60, (*waits)[0].Seconds(), 1, "GitHub's minute, where it names no wait")
}

// A write is never asked again: it's refused with when the limit lifts,
// however near, since a write retried may be made twice.
func TestAWriteRefusedForARateLimitIsNotRetried(t *testing.T) {
	reset := time.Now().Add(10 * time.Second).Truncate(time.Second)
	server := &limitedServer{replies: []func(http.ResponseWriter){primaryRefusal(reset)}}
	client, waits := limitedClient(t, server, "fixture-token")
	api, err := client.API(t.Context())
	require.NoError(t, err)
	_, _, err = api.Issues.Create(t.Context(), "owner", "project", gh.CreateIssueRequest{Title: "update"})
	var limited *forge.RateLimitError
	require.ErrorAs(t, RateLimitError(err), &limited)
	require.Equal(t, []string{"POST /repos/owner/project/issues"}, server.asked)
	require.Empty(t, *waits)
}

// A read whose deadline comes before the limit lifts isn't kept waiting
// for nothing: it's refused with the reset at once.
func TestAReadIsNotWaitedPastItsDeadline(t *testing.T) {
	reset := time.Now().Add(90 * time.Second).Truncate(time.Second)
	server := &limitedServer{replies: []func(http.ResponseWriter){primaryRefusal(reset)}}
	client, waits := limitedClient(t, server, "fixture-token")
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	err := get(t, ctx, client)
	var limited *forge.RateLimitError
	require.ErrorAs(t, err, &limited)
	require.Empty(t, *waits)
}

// A wait is said rounded up, in the unit a person would use.
func TestAWaitIsSaidInWords(t *testing.T) {
	for d, words := range map[time.Duration]string{
		0: "a moment", time.Second: "1 second", 40*time.Second + time.Millisecond: "41 seconds",
		time.Minute: "1 minute", 22*time.Minute + time.Second: "23 minutes", 3 * time.Hour: "3 hours",
	} {
		require.Equal(t, words, duration(d), d)
	}
	require.True(t, strings.HasPrefix(limitWords(secondaryLimit, "", time.Now(), time.Now()), "GitHub's secondary rate limit"))
}

// A command's spend counts its own requests, and its log line names them.
func TestSpendCountsAndLogs(t *testing.T) {
	log := filepath.Join(t.TempDir(), "github.log")
	spend := StartSpend()
	apiRequests.Add(3)
	require.Equal(t, int64(3), spend.Requests())
	spend.Log(log, []string{"status", "--refresh"})
	data, err := os.ReadFile(log)
	require.NoError(t, err)
	require.Regexp(t, `^3\t\d+\tstatus --refresh\n$`, string(data))
}

// GitHub not reached is said as that, the network's own reason in
// brackets, where the request's address and dial led: offline said as
// offline (part 4, item 1). It's the network's kind, worth another try.
func TestGitHubNotReachedIsSaidAsThat(t *testing.T) {
	t.Parallel()
	reset := &url.Error{Op: "Get", URL: "https://api.github.com/repos/macports/macports-ports/pulls/1",
		Err: &net.OpError{Op: "read", Net: "tcp", Err: &os.SyscallError{Syscall: "read", Err: syscall.ECONNRESET}}}
	err := RateLimitError(reset)
	require.EqualError(t, err, "couldn't reach GitHub (connection reset by peer)")
	require.Equal(t, refusal.Network, refusal.KindOf(err))
	require.ErrorIs(t, err, syscall.ECONNRESET)

	offline := &url.Error{Op: "Get", URL: "https://api.github.com/user", Err: &net.OpError{Op: "dial", Net: "tcp", Err: &net.DNSError{Err: "no such host", Name: "api.github.com"}}}
	require.EqualError(t, RateLimitError(offline), "couldn't reach GitHub (no such host)")
}

// A login GitHub rejects is said as that, of the Auth kind, without the
// request's address, where D-C3 on rc5 read "couldn't reach GitHub (Post
// \"https://api.github.com/graphql\": forge: authentication is required:
// …)": http.Client's *url.Error is a net.Error, and was taken as the
// network's, offering another try none would meet.
func TestARejectedLoginIsSaidAsThatNotAsGitHubUnreached(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"message":"Bad credentials"}`)
	}))
	t.Cleanup(server.Close)
	keychain := TokenSourceFunc(func(context.Context) (Token, error) {
		return Token{Secret: "ghp_" + strings.Repeat("a", 36), Source: SourceKeychain}, nil
	})
	c := &Client{HTTP: server.Client(), Config: Config{BaseURL: server.URL + "/"}, Credentials: keychain}
	_, err := c.AuthenticatedUser(t.Context())
	require.ErrorIs(t, err, ErrAuthentication)
	require.Equal(t, refusal.Auth, refusal.KindOf(err))
	require.Equal(t, "GitHub rejected the login from dockhand's Keychain; run dockhand setup github to sign in again, or dockhand setup github --logout to remove it", refusal.WordsOf(err))
	require.NotContains(t, err.Error(), server.URL)
	require.NotContains(t, err.Error(), "couldn't reach GitHub")
	require.Equal(t, []refusal.Next{{Verb: refusal.VerbSetup, Args: []string{"github"}}}, refusal.Of(err).Next)

	// A connection reset is still the network's.
	dropping := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		connection, _, _ := w.(http.Hijacker).Hijack()
		_ = connection.(*net.TCPConn).SetLinger(0)
		_ = connection.Close()
	}))
	t.Cleanup(dropping.Close)
	c = &Client{HTTP: dropping.Client(), Config: Config{BaseURL: dropping.URL + "/"}, Credentials: keychain}
	_, err = c.AuthenticatedUser(t.Context())
	require.Equal(t, refusal.Network, refusal.KindOf(err))
	require.Contains(t, refusal.WordsOf(err), "couldn't reach GitHub (connection reset by peer)")
}

// A read GitHub answered with an ETag is asked again with If-None-Match,
// and GitHub's 304, which counts nothing against the hour, is answered
// with what was kept, in this process and, from the cache's directory, the
// next: a second look within the hour spends next to nothing, where the
// M1's spent 2,166 requests each time (Then's item 5).
func TestAReadAskedAgainCostsNothingWhereNothingMoved(t *testing.T) {
	var asked, conditional atomic.Int64
	login := "ada"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked.Add(1)
		etag := `"` + login + `"`
		if r.Header.Get("If-None-Match") == etag {
			conditional.Add(1)
			w.Header().Set("X-RateLimit-Remaining", "4000")
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("ETag", etag)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"login":"`+login+`"}`)
	}))
	t.Cleanup(server.Close)
	token := TokenSourceFunc(func(context.Context) (Token, error) {
		return Token{Secret: "ghp_" + strings.Repeat("a", 36), Source: SourceKeychain}, nil
	})
	directory := t.TempDir()
	client := func() *Client {
		return &Client{HTTP: server.Client(), Config: Config{BaseURL: server.URL + "/"}, Credentials: token, Conditional: NewConditionalCache(directory)}
	}
	c := client()
	spend := StartSpend()
	for range 3 {
		account, err := c.AuthenticatedUser(t.Context())
		require.NoError(t, err)
		require.Equal(t, "ada", account)
	}
	require.EqualValues(t, 3, asked.Load())
	require.EqualValues(t, 2, conditional.Load(), "asked again with its ETag")
	require.EqualValues(t, 1, spend.Requests(), "the 304s spent nothing")

	// The next process reads what this one kept.
	account, err := client().AuthenticatedUser(t.Context())
	require.NoError(t, err)
	require.Equal(t, "ada", account)
	require.EqualValues(t, 3, conditional.Load())

	// What moved is read anew.
	login = "bob"
	account, err = client().AuthenticatedUser(t.Context())
	require.NoError(t, err)
	require.Equal(t, "bob", account)
}
