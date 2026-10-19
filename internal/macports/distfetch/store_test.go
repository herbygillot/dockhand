package distfetch

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/herbygillot/dockhand/internal/fetch"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/stretchr/testify/require"
)

func TestArchiveStoreKeepsBytesOnlyWithADirectory(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/missing" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = w.Write([]byte("archive bytes"))
	}))
	defer server.Close()
	client := Client{HTTP: server.Client()}
	info := macports.PortInfo{Options: map[string]string{}}
	hashed, err := client.Store("").Fetch(t.Context(), info, Source{Name: "a.tar.gz", URL: server.URL + "/a.tar.gz"})
	require.NoError(t, err)
	require.Empty(t, hashed.Path, "without a directory only the hashes are kept")
	require.Equal(t, int64(len("archive bytes")), hashed.Size)

	directory := t.TempDir()
	kept, err := client.Store(directory).Fetch(t.Context(), info, Source{Name: "a.tar.gz", URL: server.URL + "/a.tar.gz"})
	require.NoError(t, err)
	data, err := os.ReadFile(kept.Path)
	require.NoError(t, err)
	require.Equal(t, "archive bytes", string(data))
	require.Equal(t, hashed.SHA256, kept.SHA256)

	_, err = client.Store(directory).Fetch(t.Context(), info, Source{Name: "b.tar.gz", URL: server.URL + "/missing"})
	require.Error(t, err)
	entries, err := os.ReadDir(directory)
	require.NoError(t, err)
	require.Len(t, entries, 1, "a failed download leaves no partial file")

	first, err := client.Store("").FetchFirst(t.Context(), info, "c.tar.gz", []string{server.URL + "/missing", server.URL + "/c.tar.gz"})
	require.NoError(t, err)
	require.Equal(t, server.URL+"/c.tar.gz", first.URL, "the first working location wins")
	_, err = client.Store("").FetchFirst(t.Context(), info, "d.tar.gz", []string{server.URL + "/missing"})
	require.ErrorContains(t, err, "404")
}

// Where every site sends an HTML page, as SourceForge's mirrors do for a
// release that hasn't reached them, that's said, and another try later may
// find it: five updates failed naming the last of twenty mirrors (field
// testing at 6dd7745c).
func TestEveryMirrorsHTMLPageIsAReleaseNotThereYet(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<html>Problems downloading</html>"))
	}))
	defer server.Close()
	client := Client{HTTP: server.Client()}
	info := macports.PortInfo{Options: map[string]string{}}
	_, err := client.Store("").FetchFirst(t.Context(), info, "libpng-1.6.50.tar.xz", []string{server.URL + "/a/libpng-1.6.50.tar.xz", server.URL + "/b/libpng-1.6.50.tar.xz"})
	var behind *MirrorsBehind
	require.ErrorAs(t, err, &behind)
	require.ErrorIs(t, err, ErrHTMLPage)
	require.ErrorContains(t, err, "every one of its 2 sites sent an HTML page")
	require.True(t, fetch.Transient(err), "another try later may find it")

	_, err = client.Store("").FetchFirst(t.Context(), info, "libpng-1.6.50.tar.xz", []string{server.URL + "/a/libpng-1.6.50.tar.xz"})
	require.False(t, errors.As(err, &behind), "one site's page is that site's")
}
