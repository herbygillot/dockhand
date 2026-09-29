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

	sha := func(s string) string { sum := sha256.Sum256([]byte(s)); return hex.EncodeToString(sum[:]) }
	info := func(contents string) macports.PortInfo {
		return macports.PortInfo{Name: "croc", Options: map[string]string{
			"distfiles": "croc-10.2.4.tar.gz", "master_sites": upstream.URL + "/releases/",
			"checksums":  fmt.Sprintf("sha256 %s size %d", sha(contents), len(contents)),
			"fetch.type": "standard", "fetch.archive_compatible": "1", "fetch.ignore_sslcert": "0", "fetch.has_credentials": "0",
		}}
	}
	store := archives.Client{Mirror: mirror.URL + "/"}.Store(t.TempDir())
	// MacPorts' fetch plan, which knows its mirror groups, says where
	// upstream serves it.
	plan := macports.PortObservation{Distfiles: []macports.Distfile{{Name: "croc-10.2.4.tar.gz", URLs: []string{upstream.URL + "/releases/croc-10.2.4.tar.gz"}}}}

	// The branch declares what upstream serves now.
	fetched, err := fetchDeclared(t.Context(), store, info(now), plan, "")
	require.NoError(t, err)
	require.False(t, fetched[0].Mirror)

	// The base declares what upstream served before: the mirror has it,
	// under the port's dist_subdir.
	fetched, err = fetchDeclared(t.Context(), store, info(old), plan, "")
	require.NoError(t, err)
	require.True(t, fetched[0].Mirror)
	require.Equal(t, "/croc/croc-10.2.4.tar.gz", asked)
	data, err := os.ReadFile(fetched[0].Path)
	require.NoError(t, err)
	require.Equal(t, old, string(data))

	// Neither has what a Portfile declares: said so.
	_, err = fetchDeclared(t.Context(), store, info("something else"), plan, "")
	require.ErrorContains(t, err, "neither upstream nor MacPorts' mirror has croc-10.2.4.tar.gz")

	// A fetch the policy leaves to a dedicated preparer isn't made here.
	custom := info(now)
	custom.Options["fetch.type"] = "git"
	_, err = fetchDeclared(t.Context(), store, custom, plan, "")
	require.ErrorContains(t, err, "fetch customization or vendored source requires a dedicated preparer")

	// Without a plan, nothing is fetched, and MacPorts' reason is said.
	_, err = fetchDeclared(t.Context(), store, info(now), macports.PortObservation{Problems: []string{"native fetch plan unavailable: no sites"}}, "")
	require.EqualError(t, err, "MacPorts' fetch plan names no archives: native fetch plan unavailable: no sites")
}
