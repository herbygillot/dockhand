package archives

import (
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
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
	sources := []Source{{Name: "source-2.tar.gz", URL: server.URL + "/source-2.tar.gz"}}
	store := Client{}.Store(t.TempDir())
	info := archiveInfo(server.URL)

	_, err := store.Shipped(t.Context(), info, sources)
	require.EqualError(t, err, "source-2.tar.gz has no checksums in the Portfile")

	info.Options["checksums"] = fmt.Sprintf("source-2.tar.gz sha256 %x size %d", sha256.Sum256([]byte(body)), len(body))
	shipped, err := store.Shipped(t.Context(), info, sources)
	require.NoError(t, err)
	require.False(t, shipped[0].Mirror)
	require.FileExists(t, shipped[0].Path)

	info.Options["checksums"] = "md5 0123456789abcdef0123456789abcdef"
	_, err = store.Shipped(t.Context(), info, sources)
	require.NoError(t, err, "md5 alone can't tell")

	info.Options["checksums"] = "sha256 " + strings.Repeat("0", 64) + " size 1"
	_, err = store.Shipped(t.Context(), info, sources)
	require.EqualError(t, err, "upstream no longer serves source-2.tar.gz as the Portfile's checksums describe it")
}
