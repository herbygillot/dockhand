package distfile

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/testenv"
)

func TestFetchSums(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("hello\n"))
	}))
	defer srv.Close()

	sums, err := Fetch(context.Background(), []string{srv.URL + "/f.tar.gz"}, Options{})
	require.NoError(t, err)
	// sha256 and size are independent ground truth for "hello\n".
	assert.Equal(t, "5891b5b522d5df086d0ff0b110fbd9d21bb4fc7163af34d08286a2e846f6be03", sums.Sha256)
	assert.Equal(t, int64(6), sums.Size)
	assert.Len(t, sums.Rmd160, 40)
}

func TestFetchFallsBackAcrossURLs(t *testing.T) {
	bad := httptest.NewServer(http.NotFoundHandler())
	defer bad.Close()
	good := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("hello\n"))
	}))
	defer good.Close()

	sums, err := Fetch(context.Background(), []string{bad.URL + "/f", good.URL + "/f"}, Options{})
	require.NoError(t, err)
	assert.Equal(t, int64(6), sums.Size)
}

func TestFetchAllFail(t *testing.T) {
	bad := httptest.NewServer(http.NotFoundHandler())
	defer bad.Close()
	_, err := Fetch(context.Background(), []string{bad.URL + "/a", bad.URL + "/b"}, Options{})
	require.ErrorIs(t, err, ErrUnavailable)
	require.ErrorContains(t, err, "404")
}

func TestCurlFetchSums(t *testing.T) {
	testenv.Tool(t, "curl")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("hello\n"))
	}))
	defer srv.Close()

	sums, err := curlFetch(context.Background(), srv.URL+"/f.tar.gz", Options{})
	require.NoError(t, err)
	assert.Equal(t, "5891b5b522d5df086d0ff0b110fbd9d21bb4fc7163af34d08286a2e846f6be03", sums.Sha256)
	assert.Equal(t, int64(6), sums.Size)
}

func TestCurlFetchReportsFailure(t *testing.T) {
	testenv.Tool(t, "curl")
	srv := httptest.NewServer(http.NotFoundHandler())
	defer srv.Close()
	_, err := curlFetch(context.Background(), srv.URL+"/f", Options{})
	require.Error(t, err)
	require.ErrorContains(t, err, "curl")
}

func TestFetchRoutesSchemes(t *testing.T) {
	// A non-http scheme routes to curl; with curl present the failure
	// comes from the connection, not the router.
	testenv.Tool(t, "curl")
	_, err := Fetch(context.Background(), []string{"ftp://127.0.0.1:1/nope.tar.gz"}, Options{})
	require.ErrorIs(t, err, ErrUnavailable)
	require.ErrorContains(t, err, "curl")
}

// The recorded checksums are of the on-the-wire bytes: a server that
// compresses must NOT be transparently decoded, or our sums disagree
// with what port records (base fetches with compression off).
func TestFetchDoesNotDecodeCompression(t *testing.T) {
	var gz bytes.Buffer
	w := gzip.NewWriter(&gz)
	_, _ = w.Write([]byte("hello\n"))
	require.NoError(t, w.Close())
	wire := gz.Bytes()

	srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		rw.Header().Set("Content-Encoding", "gzip")
		_, _ = rw.Write(wire)
	}))
	defer srv.Close()

	sums, err := Fetch(context.Background(), []string{srv.URL + "/f.tar.gz"}, Options{})
	require.NoError(t, err)
	wireSha := sha256.Sum256(wire)
	assert.Equal(t, hex.EncodeToString(wireSha[:]), sums.Sha256, "sums must be of the wire bytes")
	assert.Equal(t, int64(len(wire)), sums.Size)
}

// Base allows redirect chains to run to 50; Go's default gives up at 10.
func TestFetchFollowsLongRedirectChains(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var hop int
		_, _ = fmt.Sscanf(r.URL.Path, "/hop/%d", &hop)
		if hop < 15 {
			http.Redirect(w, r, fmt.Sprintf("%s/hop/%d", srv.URL, hop+1), http.StatusFound)
			return
		}
		_, _ = w.Write([]byte("hello\n"))
	}))
	defer srv.Close()

	sums, err := Fetch(context.Background(), []string{srv.URL + "/hop/0"}, Options{})
	require.NoError(t, err)
	assert.Equal(t, int64(6), sums.Size)
}

// Some mirror redirect chains require cookies (base runs the cookie
// engine with a throwaway jar for exactly this).
func TestFetchCarriesCookiesAcrossRedirects(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/start":
			http.SetCookie(w, &http.Cookie{Name: "token", Value: "42"})
			http.Redirect(w, r, srv.URL+"/file", http.StatusFound)
		case "/file":
			if c, err := r.Cookie("token"); err != nil || c.Value != "42" {
				http.Error(w, "no cookie", http.StatusForbidden)
				return
			}
			_, _ = w.Write([]byte("hello\n"))
		}
	}))
	defer srv.Close()

	sums, err := Fetch(context.Background(), []string{srv.URL + "/start"}, Options{})
	require.NoError(t, err)
	assert.Equal(t, int64(6), sums.Size)
}

// A port's own user agent rides along.
func TestFetchHonorsPortUserAgent(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("User-Agent")
		_, _ = w.Write([]byte("hello\n"))
	}))
	defer srv.Close()
	_, err := Fetch(context.Background(), []string{srv.URL + "/f"}, Options{UserAgent: "special-agent/1"})
	require.NoError(t, err)
	assert.Equal(t, "special-agent/1", got)
}

// Without a port UA override, the Go fetchers identify as dockhand.
func TestFetchDefaultUserAgentIsDockhand(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("User-Agent")
		_, _ = w.Write([]byte("hello\n"))
	}))
	defer srv.Close()
	_, err := Fetch(context.Background(), []string{srv.URL + "/f"}, Options{})
	require.NoError(t, err)
	assert.Contains(t, got, "dockhand")
}
