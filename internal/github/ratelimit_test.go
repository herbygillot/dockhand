package github

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	gh "github.com/google/go-github/v91/github"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/forge"
	"github.com/herbygillot/dockhand/internal/progress"
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
	require.ErrorContains(t, err, "GitHub's rate limit for your login resets at "+reset.Local().Format("15:04")+", in 23 minutes")
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
	require.ErrorContains(t, err, "GitHub's rate limit for requests without a login resets at")
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
	require.ErrorContains(t, err, "GitHub's secondary rate limit, on requests made close together, lifts at")
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
