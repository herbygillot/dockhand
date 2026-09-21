package github_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/herbygillot/dockhand/internal/forge/github"
	githubapi "github.com/herbygillot/dockhand/internal/github"
	"github.com/stretchr/testify/require"
)

// A livecheck URL on the API is fetched through the client, path and query
// intact, with the headers Base's curl sends; a URL elsewhere is not served.
func TestDocumentFetchesAPIURLsThroughTheClient(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/repos/owner/project/tags", r.URL.Path)
		require.Equal(t, "per_page=100", r.URL.RawQuery)
		require.Equal(t, "*/*", r.Header.Get("Accept"), "Base's curl fetch accepts anything, and the API's layout follows")
		require.Empty(t, r.Header.Get("X-GitHub-Api-Version"))
		require.Equal(t, "MacPorts/2.12.6 libcurl dockhand/2", r.Header.Get("User-Agent"))
		require.Equal(t, "identity", r.Header.Get("Accept-Encoding"))
		fmt.Fprint(w, "[\n  {\n    \"name\": \"v1.2\"\n  }\n]\n")
	}))
	defer server.Close()
	client := &github.Client{Client: &githubapi.Client{Config: githubapi.Config{BaseURL: server.URL}}}
	body, served, err := client.Document(t.Context(), "https://api.github.com/repos/owner/project/tags?per_page=100", baseHeaders())
	require.NoError(t, err)
	require.True(t, served)
	require.Contains(t, string(body), `"name": "v1.2"`)
	for _, address := range []string{"https://github.com/owner/project/tags", "https://raw.githubusercontent.com/owner/project/master/Info.plist", "http://api.github.com/repos/owner/project/tags", "https://user@api.github.com/repos/owner/project/tags"} {
		_, served, err := client.Document(t.Context(), address, baseHeaders())
		require.NoError(t, err)
		require.False(t, served, address)
	}
	failing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNotFound) }))
	defer failing.Close()
	client = &github.Client{Client: &githubapi.Client{Config: githubapi.Config{BaseURL: failing.URL}}}
	_, served, err = client.Document(t.Context(), "https://api.github.com/repos/owner/project/releases/latest", baseHeaders())
	require.True(t, served)
	require.ErrorContains(t, err, "404")
}

// baseHeaders are the headers Base's curl fetch sends for a livecheck.
func baseHeaders() http.Header {
	return http.Header{"User-Agent": {"MacPorts/2.12.6 libcurl dockhand/2"}, "Accept": {"*/*"}, "Accept-Encoding": {"identity"}}
}
