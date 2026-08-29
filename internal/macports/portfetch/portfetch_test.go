package portfetch

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/distfile"
	"github.com/herbygillot/dockhand/internal/macports/prefix"
	"github.com/herbygillot/dockhand/internal/testenv"
)

func newFetcher(t *testing.T) *Fetcher {
	t.Helper()
	tclsh := testenv.PortTclsh(t)
	f, err := New(context.Background(), prefix.Prefix(filepath.Dir(filepath.Dir(tclsh))))
	require.NoError(t, err)
	t.Cleanup(f.Close)
	return f
}

func TestFetchSums(t *testing.T) {
	f := newFetcher(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("hello\n"))
	}))
	defer srv.Close()

	sums, err := f.Fetch(context.Background(), []string{srv.URL + "/f.tar.gz"}, distfile.Options{})
	require.NoError(t, err)
	assert.Equal(t, "5891b5b522d5df086d0ff0b110fbd9d21bb4fc7163af34d08286a2e846f6be03", sums.Sha256)
	assert.Equal(t, int64(6), sums.Size)
	assert.Len(t, sums.Rmd160, 40)
}

func TestFetchFallsBackAcrossURLs(t *testing.T) {
	f := newFetcher(t)
	bad := httptest.NewServer(http.NotFoundHandler())
	defer bad.Close()
	good := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("hello\n"))
	}))
	defer good.Close()

	sums, err := f.Fetch(context.Background(), []string{bad.URL + "/f", good.URL + "/f"}, distfile.Options{})
	require.NoError(t, err)
	assert.Equal(t, int64(6), sums.Size)
	// The session survives the failed URL and serves more fetches.
	sums, err = f.Fetch(context.Background(), []string{good.URL + "/g"}, distfile.Options{})
	require.NoError(t, err)
	assert.Equal(t, int64(6), sums.Size)
}

func TestFetchAllFail(t *testing.T) {
	f := newFetcher(t)
	bad := httptest.NewServer(http.NotFoundHandler())
	defer bad.Close()
	_, err := f.Fetch(context.Background(), []string{bad.URL + "/a"}, distfile.Options{})
	require.ErrorIs(t, err, distfile.ErrUnavailable)
	require.ErrorContains(t, err, "404")
}

func TestFetchHonorsPortUserAgent(t *testing.T) {
	f := newFetcher(t)
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("User-Agent")
		_, _ = w.Write([]byte("hello\n"))
	}))
	defer srv.Close()
	_, err := f.Fetch(context.Background(), []string{srv.URL + "/f"}, distfile.Options{UserAgent: "special-agent/2"})
	require.NoError(t, err)
	assert.Equal(t, "special-agent/2", got)
}

func TestVercmp(t *testing.T) {
	f := newFetcher(t)
	ctx := context.Background()
	n, err := f.Vercmp(ctx, "1.10", "1.9")
	require.NoError(t, err)
	assert.Positive(t, n)
	n, err = f.Vercmp(ctx, "1.0", "1.0")
	require.NoError(t, err)
	assert.Zero(t, n)
}

// livecheckPort builds a portdir whose livecheck fetches from url with
// an explicit regex.
func livecheckPort(t *testing.T, url, version string) string {
	t.Helper()
	dir := t.TempDir()
	portfile := fmt.Sprintf(`PortSystem 1.0
name lcprobe
version %s
categories devel
maintainers nomaintainer
license MIT
description livecheck probe
homepage http://127.0.0.1/lcprobe
long_description livecheck probe port
livecheck.type regex
livecheck.url %s
livecheck.regex {tags/([0-9.]+)\.tar\.gz}
`, version, url)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "Portfile"), []byte(portfile), 0o644))
	return dir
}

func TestLivecheckPhase(t *testing.T) {
	f := newFetcher(t)
	page := `<a href="tags/1.9.tar.gz"> <a href="tags/1.10.tar.gz">`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(page))
	}))
	defer srv.Close()
	ctx := context.Background()

	// A newer version upstream: found, vercmp-newest (1.10 > 1.9).
	r, err := f.Livecheck(ctx, livecheckPort(t, srv.URL+"/tags", "1.2"), "")
	require.NoError(t, err)
	assert.True(t, r.Ran)
	assert.Equal(t, "1.10", r.Version)

	// Already at the newest: up to date.
	r, err = f.Livecheck(ctx, livecheckPort(t, srv.URL+"/tags", "1.10"), "")
	require.NoError(t, err)
	assert.True(t, r.Ran)
	assert.True(t, r.UpToDate)
	assert.Empty(t, r.Version)

	// The rot signal: a regex matching nothing.
	empty := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("nothing versioned here"))
	}))
	defer empty.Close()
	r, err = f.Livecheck(ctx, livecheckPort(t, empty.URL+"/tags", "1.2"), "")
	require.NoError(t, err)
	assert.True(t, r.Ran)
	assert.True(t, r.NoMatch)
}

func TestLivecheckTypeNone(t *testing.T) {
	f := newFetcher(t)
	dir := t.TempDir()
	portfile := `PortSystem 1.0
name lcnone
version 1.0
categories devel
maintainers nomaintainer
license MIT
description no livecheck
homepage http://127.0.0.1/lcnone
long_description no livecheck at all
livecheck.type none
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "Portfile"), []byte(portfile), 0o644))
	r, err := f.Livecheck(context.Background(), dir, "")
	require.NoError(t, err)
	assert.False(t, r.Ran)
}
