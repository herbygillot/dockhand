package gitlab

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/herbygillot/dockhand/internal/forge"
)

// LimitWords name GitLab's rate limit, as a look groups the ports it
// kept by.
const LimitWords = "GitLab's rate limit"

// limitRetries bounds the SDK's retries of a request that failed for a
// reason a retry may mend: a server's error or a connection's.
const limitRetries = 3

// limitedTransport answers GitLab's rate limit, a 429, with a
// forge.RateLimitError until its reset, which the look says and sets that
// port aside for, as GitHub's is: the SDK waited until RateLimit-Reset,
// unbounded, and a look over 1,114 ports sat an hour on zix (the GraphQL
// item's baseline, 2026-10-30).
type limitedTransport struct {
	next http.RoundTripper
	now  func() time.Time
}

func (t limitedTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	response, err := t.next.RoundTrip(req)
	if err != nil || response.StatusCode != http.StatusTooManyRequests {
		return response, err
	}
	response.Body.Close()
	until := resetAt(response.Header, t.now())
	return nil, &forge.RateLimitError{RetryAt: until, Err: fmt.Errorf("%s for %s is spent until %s", LimitWords, req.URL.Host, until.Local().Format("15:04"))}
}

// resetAt is when a 429's limit resets, by RateLimit-Reset, a Unix time,
// or Retry-After, in seconds; a minute on where neither says.
func resetAt(header http.Header, now time.Time) time.Time {
	if reset, err := strconv.ParseInt(header.Get("RateLimit-Reset"), 10, 64); err == nil && reset > 0 {
		return time.Unix(reset, 0)
	}
	if after, err := strconv.Atoi(header.Get("Retry-After")); err == nil && after >= 0 {
		return now.Add(time.Duration(after) * time.Second)
	}
	return now.Add(time.Minute)
}

// checkRetry is the SDK's retry decision: never a rate limit, which
// limitedTransport has said, nor a canceled request; a server's error or a
// connection's, up to limitRetries.
func checkRetry(ctx context.Context, response *http.Response, err error) (bool, error) {
	if ctx.Err() != nil {
		return false, ctx.Err()
	}
	if limited := new(forge.RateLimitError); errors.As(err, &limited) {
		return false, limited
	}
	if err != nil {
		return true, nil
	}
	return response.StatusCode >= 500 && response.StatusCode != http.StatusNotImplemented, nil
}

// limitedClient is the HTTP client the SDK asks GitLab with, its
// transport answering a rate limit (limitedTransport).
func limitedClient(client *http.Client) *http.Client {
	limited := http.Client{}
	if client != nil {
		limited = *client
	}
	next := limited.Transport
	if next == nil {
		next = http.DefaultTransport
	}
	limited.Transport = limitedTransport{next: next, now: time.Now}
	return &limited
}
