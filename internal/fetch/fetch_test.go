package fetch

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLimitsKnownAndStreamingBodies(t *testing.T) {
	for _, stream := range []bool{false, true} {
		for _, size := range []int{0, 4, 5} {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if stream {
					w.(http.Flusher).Flush()
				}
				io.WriteString(w, strings.Repeat("x", size))
			}))
			req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, server.URL, nil)
			require.NoError(t, err)
			response, err := Open(server.Client(), req, 4)
			if err == nil {
				body, readErr := io.ReadAll(response.Body)
				require.NoError(t, response.Body.Close())
				if size > 4 {
					require.ErrorIs(t, readErr, ErrTooLarge)
					require.Len(t, body, 4)
				} else {
					require.NoError(t, readErr)
					require.Len(t, body, size)
				}
			} else {
				require.Greater(t, size, 4)
				require.ErrorIs(t, err, ErrTooLarge)
			}
			server.Close()
		}
	}
}

func TestRedirectPolicyAndCancellation(t *testing.T) {
	reached := false
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { reached = true; io.WriteString(w, "ok") }))
	defer target.Close()
	source := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, http.StatusFound) }))
	defer source.Close()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, source.URL, nil)
	require.NoError(t, err)
	_, err = Open(source.Client(), req, 10)
	require.ErrorContains(t, err, "downgraded HTTPS")
	require.False(t, reached)
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = Open(source.Client(), req.WithContext(canceled), 10)
	require.ErrorIs(t, err, context.Canceled)
	refusal := errors.New("caller refuses redirect")
	source.Client().CheckRedirect = func(*http.Request, []*http.Request) error { return refusal }
	// A same-scheme redirect must still respect the caller's policy.
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, http.StatusFound) }))
	defer redirect.Close()
	client := redirect.Client()
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return refusal }
	req, err = http.NewRequestWithContext(t.Context(), http.MethodGet, redirect.URL, nil)
	require.NoError(t, err)
	_, err = Open(client, req, 10)
	require.ErrorIs(t, err, refusal)
	require.False(t, reached)
}

func TestResponseStatusAndReaderErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNotFound) }))
	defer server.Close()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, server.URL, nil)
	require.NoError(t, err)
	_, err = Open(server.Client(), req, 10)
	require.ErrorContains(t, err, "HTTP 404")

	// A text or JSON body's first line says why, and a redirect that led to
	// the refusal names the URL that was asked for.
	explained := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/asset" {
			http.Redirect(w, r, "/gone", http.StatusFound)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusForbidden)
		fmt.Fprint(w, "  rate limit exceeded for this address\nsecond line is not shown")
	}))
	defer explained.Close()
	req, err = http.NewRequestWithContext(t.Context(), http.MethodGet, explained.URL+"/asset", nil)
	require.NoError(t, err)
	_, err = Open(explained.Client(), req, 10)
	var status *StatusError
	require.ErrorAs(t, err, &status)
	require.Equal(t, &StatusError{Status: http.StatusForbidden, URL: explained.URL + "/gone", Requested: explained.URL + "/asset", Reason: "rate limit exceeded for this address"}, status)
	require.Equal(t, "fetch: HTTP 403 for "+explained.URL+"/gone, redirected from "+explained.URL+"/asset: rate limit exceeded for this address", err.Error())
	failure := errors.New("broken stream")
	reader := &boundedBody{ReadCloser: failingBody{failure}, remaining: 4}
	_, err = io.ReadAll(reader)
	require.ErrorIs(t, err, failure)
}

type failingBody struct{ err error }

func (f failingBody) Read([]byte) (int, error) { return 0, f.err }
func (f failingBody) Close() error             { return nil }

// roundTrips stands in for the network: each request's answer.
type roundTrips func(*http.Request) (*http.Response, error)

func (f roundTrips) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func answer(r *http.Request, status int, location string) *http.Response {
	header := http.Header{}
	if location != "" {
		header.Set("Location", location)
	}
	return &http.Response{StatusCode: status, Header: header, Body: io.NopCloser(strings.NewReader("")), Request: r}
}

// Asking a URL follows redirects as Open does: one from HTTPS back to
// plain HTTP is no answer, where it had been counted as one (the
// helper-ownership review's finding 2). A server that refuses HEAD is
// asked for a byte; any status below 400 answers, and the URL that did is
// said.
func TestAskingAURLFollowsRedirectsAsFetchingDoes(t *testing.T) {
	client := &http.Client{Transport: roundTrips(func(r *http.Request) (*http.Response, error) {
		switch {
		case r.URL.Path == "/downgrade" && r.URL.Scheme == "https":
			return answer(r, http.StatusFound, "http://example.invalid/plain"), nil
		case r.URL.Path == "/moved":
			return answer(r, http.StatusMovedPermanently, "https://example.invalid/home/"), nil
		case r.URL.Path == "/nohead" && r.Method == http.MethodHead:
			return answer(r, http.StatusMethodNotAllowed, ""), nil
		case r.URL.Path == "/nohead":
			require.Equal(t, "bytes=0-0", r.Header.Get("Range"))
			return answer(r, http.StatusPartialContent, ""), nil
		case r.URL.Path == "/gone":
			return answer(r, http.StatusNotFound, ""), nil
		}
		return answer(r, http.StatusOK, ""), nil
	})}
	got := Ask(t.Context(), client, "https://example.invalid/downgrade")
	require.False(t, got.Answered)
	require.ErrorContains(t, got.Err, "redirect downgraded HTTPS")
	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "https://example.invalid/downgrade", nil)
	require.NoError(t, err)
	_, err = Open(client, request, 1)
	require.ErrorContains(t, err, "redirect downgraded HTTPS", "the rule Open keeps too")

	got = Ask(t.Context(), client, "https://example.invalid/moved")
	require.True(t, got.Answered)
	require.Equal(t, "https://example.invalid/home/", got.Final)
	require.True(t, Ask(t.Context(), client, "https://example.invalid/nohead").Answered)
	got = Ask(t.Context(), client, "https://example.invalid/gone")
	require.False(t, got.Answered)
	require.ErrorContains(t, got.Err, "HTTP 404")
	require.ErrorContains(t, Ask(t.Context(), client, "ftp://example.invalid/").Err, "unsupported URL")
}

// A failure another try may not meet is the network's, a timeout, or a
// busy or failing server's; a refusal, as a 404, isn't.
func TestAFailureWorthTryingAgain(t *testing.T) {
	for err, again := range map[error]bool{
		&StatusError{Status: 503}: true, &StatusError{Status: 429}: true, &StatusError{Status: 404}: false, &StatusError{Status: 403}: false,
		&url.Error{Op: "Get", URL: "https://example.org", Err: &net.DNSError{Err: "no such host", Name: "example.org"}}: true,
		fmt.Errorf("fetching: %w", context.DeadlineExceeded):                                                            true,
		errors.New("the server sent html"):                                                                              false,
		errors.Join(errors.New("upstream"), &StatusError{Status: 502}):                                                  true,
	} {
		require.Equal(t, again, Transient(err), err.Error())
	}
}
