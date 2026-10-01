package fetch

import (
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
