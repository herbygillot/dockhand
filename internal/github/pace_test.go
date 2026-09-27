package github

import (
	"context"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Requests asked for together go one interval apart, however many ask at
// once, and one that stops waiting is let go.
func TestThePacerSpacesRequests(t *testing.T) {
	p := &pacer{interval: 20 * time.Millisecond}
	started := time.Now()
	var group sync.WaitGroup
	for range 5 {
		group.Go(func() { require.NoError(t, p.wait(t.Context())) })
	}
	group.Wait()
	require.GreaterOrEqual(t, time.Since(started), 80*time.Millisecond, "five turns take four intervals")

	slow := &pacer{interval: time.Hour}
	require.NoError(t, slow.wait(t.Context()), "the first turn is now")
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Millisecond)
	defer cancel()
	require.ErrorIs(t, slow.wait(ctx), context.DeadlineExceeded)
}

// Only GitHub's own API is paced: an Enterprise server or a test's server
// is asked at once.
func TestOnlyGitHubsAPIIsPaced(t *testing.T) {
	saved := apiPace
	t.Cleanup(func() { apiPace = saved })
	apiPace = &pacer{interval: time.Hour}
	var sent []string
	transport := redirectTransport{next: roundTrip(func(req *http.Request) (*http.Response, error) {
		sent = append(sent, req.URL.Host)
		return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody, Request: req}, nil
	})}
	for _, url := range []string{"https://api.github.com/repos/a/b/tags", "https://github.example.org/api/v3/repos/a/b/tags", "http://127.0.0.1:8080/repos/a/b/tags"} {
		req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, url, nil)
		require.NoError(t, err)
		_, err = transport.RoundTrip(req)
		require.NoError(t, err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Millisecond)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.github.com/repos/a/b/releases", nil)
	require.NoError(t, err)
	_, err = transport.RoundTrip(req)
	require.ErrorIs(t, err, context.DeadlineExceeded, "the second request to GitHub waits its turn")
	require.Equal(t, []string{"api.github.com", "github.example.org", "127.0.0.1:8080"}, sent)
}

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }
