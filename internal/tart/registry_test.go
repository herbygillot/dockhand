package tart

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

const pinned = "sha256:eeec54bfe1f076e27786c5d92b89187a05b1d109b5071eb2dcdf02d596e34640"

// A tag's digest is read from the registry as the distribution API gives
// it, through an anonymous token when the registry asks for one, as
// ghcr.io does; a reference already pinned is its own answer.
func TestTheRegistryNamesATagsDigest(t *testing.T) {
	var tokens, heads int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/token":
			tokens++
			require.Equal(t, "registry.test", r.URL.Query().Get("service"))
			require.Equal(t, "repository:cirruslabs/macos-tahoe-vanilla:pull", r.URL.Query().Get("scope"))
			_, _ = w.Write([]byte(`{"token":"anonymous"}`))
		case r.Method == http.MethodHead && r.URL.Path == "/v2/cirruslabs/macos-tahoe-vanilla/manifests/latest":
			heads++
			require.Contains(t, r.Header.Get("Accept"), "application/vnd.oci.image.manifest.v1+json")
			if r.Header.Get("Authorization") != "Bearer anonymous" {
				w.Header().Set("WWW-Authenticate", `Bearer realm="http://`+r.Host+`/token",service="registry.test",scope="repository:cirruslabs/macos-tahoe-vanilla:pull"`)
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			w.Header().Set("Docker-Content-Digest", pinned)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	host := strings.TrimPrefix(server.URL, "http://")
	registry := Registry{HTTP: server.Client(), Scheme: "http"}

	digest, err := registry.Digest(t.Context(), host+"/cirruslabs/macos-tahoe-vanilla:latest")
	require.NoError(t, err)
	require.Equal(t, pinned, digest)
	require.Equal(t, 1, tokens)
	require.Equal(t, 2, heads, "asked once, then once with the token")
	digest, err = registry.Digest(t.Context(), host+"/cirruslabs/macos-tahoe-vanilla")
	require.NoError(t, err)
	require.Equal(t, pinned, digest, "no tag is latest")

	digest, err = registry.Digest(t.Context(), host+"/cirruslabs/macos-tahoe-vanilla@"+pinned)
	require.NoError(t, err)
	require.Equal(t, pinned, digest, "a pinned reference asks nothing")
	require.Equal(t, 2, tokens)

	_, err = registry.Digest(t.Context(), host+"/cirruslabs/missing:latest")
	require.ErrorIs(t, err, ErrNoDigest)
	for _, bad := range []string{"macos-tahoe-vanilla:latest", "ghcr.io/", "ghcr.io/x@sha256:short"} {
		_, err = registry.Digest(t.Context(), bad)
		require.Error(t, err, bad)
	}
}
