package fetch

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
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
	failure := errors.New("broken stream")
	reader := &boundedBody{ReadCloser: failingBody{failure}, remaining: 4}
	_, err = io.ReadAll(reader)
	require.ErrorIs(t, err, failure)
}

type failingBody struct{ err error }

func (f failingBody) Read([]byte) (int, error) { return 0, f.err }
func (f failingBody) Close() error             { return nil }
