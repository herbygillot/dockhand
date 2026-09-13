package prepare

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/herbygillot/dockhand/v2/internal/macports"
	"github.com/stretchr/testify/require"
)

func archiveInfo(site string) macports.PortInfo {
	return macports.PortInfo{Options: map[string]string{"master_sites": site, "distfiles": "source-2.tar.gz", "fetch.type": "standard", "fetch.has_credentials": "0", "fetch.customized": "0", "fetch.ignore_sslcert": "no"}}
}
func TestDownloadHashesExactBodyAndFollowsArchiveRedirect(t *testing.T) {
	body := strings.Repeat("archive bytes\x00", 200)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Accept-Encoding") != "identity" {
			w.WriteHeader(400)
			return
		}
		if r.URL.Path == "/source-2.tar.gz" {
			http.Redirect(w, r, "/asset", 302)
			return
		}
		fmt.Fprint(w, body)
	}))
	defer server.Close()
	service := Service{}
	result, err := service.download(t.Context(), archiveInfo(server.URL))
	require.NoError(t, err)
	require.Equal(t, int64(len(body)), result.Size)
	require.Equal(t, fmt.Sprintf("%x", sha256.Sum256([]byte(body))), result.SHA256)
	require.Len(t, result.RMD160, 40)
	// RIPEMD-160's published abc vector independently checks the legacy digest.
	abc := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "abc") }))
	defer abc.Close()
	result, err = service.download(t.Context(), archiveInfo(abc.URL))
	require.NoError(t, err)
	require.Equal(t, "8eb208f7e05d987a9b044a8e98c6b087f15a0bfc", result.RMD160)
}
func TestDownloadRejectsErrorBodiesAndSizeOverflow(t *testing.T) {
	for _, test := range []struct {
		name, body string
		status     int
		chunked    bool
	}{
		{"HTML", "<!doctype html><title>error</title>", 200, false}, {"empty", "", 200, false}, {"not found", "missing", 404, false},
		{"size header", strings.Repeat("x", 1025), 200, false}, {"size stream", strings.Repeat("x", 1025), 200, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(test.status)
				if test.chunked {
					w.(http.Flusher).Flush()
				}
				fmt.Fprint(w, test.body)
			}))
			defer server.Close()
			service := Service{MaxDownloadBytes: 1024}
			result, err := service.download(t.Context(), archiveInfo(server.URL))
			require.Error(t, err)
			require.Empty(t, result.SHA256)
		})
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := (&Service{}).download(ctx, archiveInfo("http://localhost"))
	require.ErrorIs(t, err, context.Canceled)
}
func TestDownloadSourceDeclinesUnsupportedFetchConventions(t *testing.T) {
	for key, value := range map[string]string{"distfiles": "one.tar.gz two.tar.gz", "master_sites": "https://example.invalid/site:tag", "fetch.type": "git", "fetch.customized": "1", "patchfiles": "remote.patch", "fetch.has_credentials": "1", "cargo.crates_github": "vendor", "go.vendors": "vendor", "fetch.ignore_sslcert": "yes"} {
		info := archiveInfo("https://example.invalid")
		info.Options[key] = value
		_, _, err := downloadSource(info)
		require.ErrorIs(t, err, ErrUnsupported, key)
	}
}
