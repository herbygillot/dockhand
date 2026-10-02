package fetch

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// A host that accepts a request and never answers is given up on, not
// waited for as long as the command runs: outdated, and serve's daily
// look, could stall on one (the limits sweep, 2026-10-01).
func TestARequestNoHostAnswersIsGivenUp(t *testing.T) {
	transport := newTransport()
	require.Equal(t, ResponseWait, transport.ResponseHeaderTimeout)
	require.Same(t, Transport, Client.Transport)
	require.Zero(t, DownloadClient.Transport.(*http.Transport).ResponseHeaderTimeout, "a download's stall bounds its wait instead")

	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { <-release }))
	defer server.Close()
	defer close(release)
	transport.ResponseHeaderTimeout = 50 * time.Millisecond
	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, server.URL, nil)
	require.NoError(t, err)
	started := time.Now()
	_, err = (&http.Client{Transport: transport}).Do(request)
	require.ErrorContains(t, err, "timeout awaiting response headers")
	require.Less(t, time.Since(started), 5*time.Second)
}

// A body that stops after its headers is given up once the stall bound
// passes without a byte, where it held its caller as long as the command
// ran; one that goes on at any pace is read whole (batch 26).
func TestABodyThatStallsIsGivenUp(t *testing.T) {
	require.Equal(t, BodyStall, Transport.(*stalling).bound)
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for range 5 {
			_, _ = w.Write([]byte("slow "))
			w.(http.Flusher).Flush()
			time.Sleep(30 * time.Millisecond)
		}
		if r.URL.Path == "/stalls" {
			<-release
		}
	}))
	defer server.Close()
	defer close(release)
	client := &http.Client{Transport: &stalling{next: newTransport(), bound: 200 * time.Millisecond}}
	get := func(path string) ([]byte, error) {
		response, err := client.Get(server.URL + path)
		require.NoError(t, err)
		defer response.Body.Close()
		return io.ReadAll(response.Body)
	}
	body, err := get("/steady")
	require.NoError(t, err)
	require.Equal(t, "slow slow slow slow slow ", string(body), "a body that goes on, however slowly, is read")
	started := time.Now()
	body, err = get("/stalls")
	require.ErrorIs(t, err, ErrStalled)
	require.ErrorContains(t, err, "for 200ms after the response began")
	require.Equal(t, "slow slow slow slow slow ", string(body))
	require.Less(t, time.Since(started), 5*time.Second)
}
