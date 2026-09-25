package engine

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/portedit/archives"
)

func TestTheOldArchiveComesFromTheMirrorAfterAStealthUpdate(t *testing.T) {
	old, now := "the old contents", "the new contents"
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, now) }))
	defer upstream.Close()
	var asked string
	mirror := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked = r.URL.Path
		fmt.Fprint(w, old)
	}))
	defer mirror.Close()
	saved := mirrorURL
	mirrorURL = mirror.URL + "/"
	t.Cleanup(func() { mirrorURL = saved })

	sha := func(s string) string { sum := sha256.Sum256([]byte(s)); return hex.EncodeToString(sum[:]) }
	info := func(contents string) macports.PortInfo {
		return macports.PortInfo{Name: "croc", Options: map[string]string{
			"distfiles": "croc-10.2.4.tar.gz", "master_sites": upstream.URL + "/releases/",
			"checksums":  fmt.Sprintf("sha256 %s size %d", sha(contents), len(contents)),
			"fetch.type": "standard", "fetch.archive_compatible": "1", "fetch.ignore_sslcert": "0", "fetch.has_credentials": "0",
		}}
	}
	store := archives.Client{}.Store(t.TempDir())

	// The branch declares what upstream serves now.
	fetched, err := fetchDeclared(t.Context(), store, info(now), "")
	require.NoError(t, err)
	require.False(t, fetched[0].Mirror)

	// The base declares what upstream served before: the mirror has it,
	// under the port's dist_subdir.
	fetched, err = fetchDeclared(t.Context(), store, info(old), "")
	require.NoError(t, err)
	require.True(t, fetched[0].Mirror)
	require.Equal(t, "/croc/croc-10.2.4.tar.gz", asked)
	data, err := os.ReadFile(fetched[0].Path)
	require.NoError(t, err)
	require.Equal(t, old, string(data))

	// Neither has what a Portfile declares: said so.
	_, err = fetchDeclared(t.Context(), store, info("something else"), "")
	require.ErrorContains(t, err, "neither upstream nor MacPorts' mirror has croc-10.2.4.tar.gz")
}
