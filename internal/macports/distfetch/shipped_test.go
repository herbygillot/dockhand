package distfetch

import (
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/portfile"
)

// Shipped takes upstream's archive only as the Portfile's checksums declare
// it, and refuses one they declare nothing for. A legacy md5 alone can't
// tell, so upstream's stands; without a mirror to ask, a changed archive
// fails.
func TestShippedTakesOnlyWhatThePortfileDeclares(t *testing.T) {
	t.Parallel()
	body := "archive bytes"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, body) }))
	defer server.Close()
	plan := []macports.Distfile{{Name: "source-2.tar.gz", URLs: []string{server.URL + "/source-2.tar.gz"}}}
	store := Client{}.Store(t.TempDir())
	info := archiveInfo(server.URL)

	_, err := store.Shipped(t.Context(), info, plan)
	require.EqualError(t, err, "source-2.tar.gz has no checksums in the Portfile")

	info.Options["checksums"] = fmt.Sprintf("source-2.tar.gz sha256 %x size %d", sha256.Sum256([]byte(body)), len(body))
	shipped, err := store.Shipped(t.Context(), info, plan)
	require.NoError(t, err)
	require.False(t, shipped[0].Mirror)
	require.FileExists(t, shipped[0].Path)

	info.Options["checksums"] = "md5 0123456789abcdef0123456789abcdef"
	_, err = store.Shipped(t.Context(), info, plan)
	require.NoError(t, err, "md5 alone can't tell")

	info.Options["checksums"] = "sha256 " + strings.Repeat("0", 64) + " size 1"
	_, err = store.Shipped(t.Context(), info, plan)
	require.EqualError(t, err, "upstream no longer serves source-2.tar.gz as the Portfile's checksums describe it")

	_, err = store.Shipped(t.Context(), info, []macports.Distfile{{Name: "../source-2.tar.gz", URLs: plan[0].URLs}})
	require.ErrorContains(t, err, "ambiguous distfile ../source-2.tar.gz")
}

// Each archive comes from the first of its locations that serves it, in
// the order MacPorts' fetch plan gives them, as MacPorts' own fetch takes
// it; one with none is asked of the mirror.
func TestShippedTakesTheFirstLocationThatServes(t *testing.T) {
	t.Parallel()
	body := "archive bytes"
	var mu sync.Mutex
	var asked []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		asked = append(asked, r.URL.Path)
		mu.Unlock()
		if strings.HasPrefix(r.URL.Path, "/gone/") {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		fmt.Fprint(w, body)
	}))
	defer server.Close()
	info := archiveInfo(server.URL)
	info.Name = "fixture"
	info.Options["checksums"] = fmt.Sprintf("sha256 %x size %d", sha256.Sum256([]byte(body)), len(body))
	store := Client{Mirror: server.URL + "/mirror/"}.Store(t.TempDir())

	shipped, err := store.Shipped(t.Context(), info, []macports.Distfile{{Name: "source-2.tar.gz", URLs: []string{server.URL + "/gone/source-2.tar.gz", server.URL + "/second/source-2.tar.gz", server.URL + "/third/source-2.tar.gz"}}})
	require.NoError(t, err)
	require.False(t, shipped[0].Mirror)
	mu.Lock()
	require.Equal(t, []string{"/gone/source-2.tar.gz", "/second/source-2.tar.gz"}, asked)
	asked = nil
	mu.Unlock()

	shipped, err = store.Shipped(t.Context(), info, []macports.Distfile{{Name: "source-2.tar.gz"}})
	require.NoError(t, err)
	require.True(t, shipped[0].Mirror)
	mu.Lock()
	defer mu.Unlock()
	require.Equal(t, []string{"/mirror/fixture/source-2.tar.gz"}, asked, "under the port's dist_subdir, its name")
}

// MacPorts' mirror holding another file of an archive's name, under a
// dist_subdir other ports share, is said: gopls 0.24.0's
// tools-0.24.0.tar.gz met x/tools' own under go/ (field testing at
// f86cec28). The same file, none at all, or a subdir of the port's own
// isn't.
func TestASharedMirrorSubdirsOtherFileIsSaid(t *testing.T) {
	t.Parallel()
	mirrored := map[string]string{"/go/tools-0.24.0.tar.gz": "x/tools' own"}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, ok := mirrored[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		fmt.Fprint(w, body)
	}))
	defer server.Close()
	store := Client{Mirror: server.URL}.Store("")
	info := macports.PortInfo{Name: "gopls", Options: map[string]string{"dist_subdir": "go"}}
	download := Download{Checksum: portfile.Checksum{Name: "tools-0.24.0.tar.gz", SHA256: fmt.Sprintf("%x", sha256.Sum256([]byte("gopls's"))), Size: 7}}
	subdir, differs := store.SharedMirrorCopy(t.Context(), info, download)
	require.Equal(t, "go", subdir)
	require.True(t, differs)

	same := Download{Checksum: portfile.Checksum{Name: "tools-0.24.0.tar.gz", SHA256: fmt.Sprintf("%x", sha256.Sum256([]byte("x/tools' own"))), Size: 12}}
	_, differs = store.SharedMirrorCopy(t.Context(), info, same)
	require.False(t, differs)
	_, differs = store.SharedMirrorCopy(t.Context(), info, Download{Checksum: portfile.Checksum{Name: "absent.tar.gz"}})
	require.False(t, differs)
	info.Options["dist_subdir"] = "gopls"
	_, differs = store.SharedMirrorCopy(t.Context(), info, download)
	require.False(t, differs, "a subdir of the port's own isn't asked")
}
