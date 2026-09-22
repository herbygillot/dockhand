package archives

import (
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

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
