package archives

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/portfile"
	"github.com/stretchr/testify/require"
)

func archiveInfo(site string) macports.PortInfo {
	return macports.PortInfo{Options: map[string]string{"master_sites": site, "distfiles": "source-2.tar.gz", "fetch.type": "standard", "fetch.has_credentials": "0", "fetch.archive_compatible": "1", "fetch.ignore_sslcert": "no"}}
}
func TestDownloadHashesExactBodyAndFollowsArchiveRedirect(t *testing.T) {
	t.Parallel()
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
	service := Client{}
	result, err := service.fetchOne(t.Context(), archiveInfo(server.URL))
	require.NoError(t, err)
	require.Equal(t, int64(len(body)), result.Size)
	require.Equal(t, fmt.Sprintf("%x", sha256.Sum256([]byte(body))), result.SHA256)
	require.Len(t, result.RMD160, 40)
	// RIPEMD-160's published abc vector independently checks the legacy digest.
	abc := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "abc") }))
	defer abc.Close()
	result, err = service.fetchOne(t.Context(), archiveInfo(abc.URL))
	require.NoError(t, err)
	require.Equal(t, "8eb208f7e05d987a9b044a8e98c6b087f15a0bfc", result.RMD160)
}
func TestDownloadRejectsErrorBodiesAndSizeOverflow(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name, body string
		status     int
		chunked    bool
		header     http.Header
		wording    []string
	}{
		{"HTML", "<!doctype html><title>error</title>", 200, false, nil, []string{"downloading source-2.tar.gz from ", "the server sent an HTML page instead of the file"}},
		{"empty", "", 200, false, nil, []string{"the server sent an empty file"}},
		{"not found", "missing", 404, false, nil, []string{"downloading source-2.tar.gz: fetch: HTTP 404 for ", ": missing; no archive is published at that location yet"}},
		{"size header", strings.Repeat("x", 1025), 200, false, nil, []string{"larger than the 1024 bytes limit"}},
		{"size stream", strings.Repeat("x", 1025), 200, true, nil, []string{"larger than the 1024 bytes limit"}},
		{"encoded", "x", 200, false, http.Header{"Content-Encoding": {"gzip"}}, []string{"the server sent gzip-encoded content instead of the file"}},
		{"truncated", "ten bytes!", 200, false, http.Header{"Content-Length": {"100"}}, []string{"transfer stopped after 10 bytes: unexpected EOF"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				for key, values := range test.header {
					w.Header()[key] = values
				}
				w.WriteHeader(test.status)
				if test.chunked {
					w.(http.Flusher).Flush()
				}
				fmt.Fprint(w, test.body)
			}))
			defer server.Close()
			service := Client{MaxBytes: 1024}
			result, err := service.fetchOne(t.Context(), archiveInfo(server.URL))
			require.Error(t, err)
			require.Empty(t, result.SHA256)
			for _, wording := range test.wording {
				require.ErrorContains(t, err, wording)
			}
			require.Equal(t, 1, strings.Count(err.Error(), server.URL+"/source-2.tar.gz"), "the URL is named once: %s", err)
		})
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := (Client{}).fetchOne(ctx, archiveInfo("http://localhost"))
	require.ErrorIs(t, err, context.Canceled)

	// Dockhand's own deadline is named as its own, with the URL and the file.
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	defer slow.Close()
	_, err = (Client{Timeout: 50 * time.Millisecond}).fetchOne(t.Context(), archiveInfo(slow.URL))
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.ErrorContains(t, err, "downloading source-2.tar.gz from "+slow.URL+"/source-2.tar.gz: no complete response within dockhand's 50ms limit")

	// A transport failure names the cause without repeating the URL.
	_, err = (Client{}).fetchOne(t.Context(), archiveInfo("http://127.0.0.1:1"))
	require.ErrorContains(t, err, "downloading source-2.tar.gz from http://127.0.0.1:1/source-2.tar.gz: dial tcp")
	require.Equal(t, 1, strings.Count(err.Error(), "http://127.0.0.1:1/source-2.tar.gz"), "%s", err)
}
func TestDownloadSourceDeclinesUnsupportedFetchConventions(t *testing.T) {
	t.Parallel()
	for key, value := range map[string]string{"distfiles": "one.tar.gz two.tar.gz", "master_sites": "https://example.invalid/site:tag", "fetch.type": "git", "fetch.archive_compatible": "0", "patchfiles": "remote.patch", "fetch.has_credentials": "1", "cargo.crates_github": "vendor", "go.vendors": "vendor", "fetch.ignore_sslcert": "yes"} {
		info := archiveInfo("https://example.invalid")
		info.Options[key] = value
		_, _, err := downloadSource(info)
		require.ErrorIs(t, err, portfile.ErrUnsupported, key)
	}
}

func TestMultipleSourcesUseExplicitMasterSiteTags(t *testing.T) {
	t.Parallel()
	info := archiveInfo("https://example.invalid/main:source https://example.invalid/assets:extras")
	info.Options["distfiles"] = "main.tar.gz:source extra.tar.gz:extras"
	sources, err := Sources(info, "")
	require.NoError(t, err)
	require.Equal(t, []Source{{Name: "main.tar.gz", URL: "https://example.invalid/main/main.tar.gz"}, {Name: "extra.tar.gz", URL: "https://example.invalid/assets/extra.tar.gz"}}, sources)
	for _, files := range []string{"main.tar.gz:missing", "main.tar.gz:source main.tar.gz:extras", "../main.tar.gz:source", "main.tar.gz:source,extras"} {
		info.Options["distfiles"] = files
		_, err = Sources(info, "")
		require.ErrorIs(t, err, portfile.ErrUnsupported)
	}
}

func TestCredentialsStopPreparationBeforeDownload(t *testing.T) {
	t.Parallel()
	var requests int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { requests++; fmt.Fprint(w, "archive") }))
	defer server.Close()
	for _, unknown := range []bool{false, true} {
		info := archiveInfo(server.URL)
		info.Options["fetch.has_credentials"] = "1"
		if unknown {
			info.OptionErrors = map[string]string{"fetch.has_credentials": "cannot determine applicable fetch credentials"}
		}
		_, err := (Client{}).fetchOne(t.Context(), info)
		require.ErrorIs(t, err, portfile.ErrUnsupported)
		require.ErrorContains(t, err, "prepare this update manually with MacPorts")
	}
	require.Zero(t, requests)
}

func TestFetchCompatibilityDiagnosisSurvivesPreparationBoundary(t *testing.T) {
	t.Parallel()
	info := archiveInfo("https://example.invalid")
	info.OptionErrors = map[string]string{"fetch.archive_compatible": "MacPorts Base 99.0: fetch target record is unavailable; prepare this port manually"}
	_, err := Sources(info, t.TempDir())
	require.ErrorIs(t, err, portfile.ErrUnsupported)
	require.ErrorContains(t, err, "MacPorts Base 99.0")
	require.ErrorContains(t, err, "prepare this port manually")
}

// Test-only conveniences over the single-archive path.
func downloadSource(info macports.PortInfo) (string, string, error) {
	files, err := Sources(info, "")
	if err != nil {
		return "", "", err
	}
	if len(files) != 1 {
		return "", "", fmt.Errorf("%w: expected one distfile", portfile.ErrUnsupported)
	}
	return files[0].Name, files[0].URL, nil
}

func (c Client) fetchOne(ctx context.Context, info macports.PortInfo) (Download, error) {
	name, address, err := downloadSource(info)
	if err != nil {
		return Download{}, err
	}
	return c.Store("").Fetch(ctx, info, Source{name, address})
}

// The options the download policy reads are the fetch phase's inputs; the
// guard grammar's effect table must name every one of them, or a hook that
// writes one could be accepted as harmless.
func TestDownloadPolicyOptionsAreFetchAffecting(t *testing.T) {
	t.Parallel()
	for _, key := range []string{"distfiles", "master_sites", "checksums", "fetch.type", "patchfiles", "filespath", "fetch.ignore_sslcert", "go.vendors", "cargo.crates", "cargo.crates_github"} {
		require.True(t, macports.AffectsFetch(key), key)
	}
}
