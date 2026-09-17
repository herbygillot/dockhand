package preparation_test

import (
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/workflow/preparation"
	"github.com/stretchr/testify/require"
)

func TestRefreshChecksumsPreservesVersionsAndCanBeCurrent(t *testing.T) {
	t.Parallel()
	for _, multiple := range []bool{false, true} {
		t.Run(fmt.Sprint(multiple), func(t *testing.T) {
			var reads atomic.Int64
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { reads.Add(1); fmt.Fprint(w, r.URL.Path) }))
			defer server.Close()
			declarations := "distfiles source.tar.gz\nchecksums sha256 " + strings.Repeat("0", 64) + " size 0\n"
			if multiple {
				declarations = "distfiles source.tar.gz extra.tar.gz\nchecksums source.tar.gz sha256 " + strings.Repeat("0", 64) + " size 0 extra.tar.gz sha256 " + strings.Repeat("0", 64) + " size 0\n"
			}
			service, request := preparationFixture(t, "revision 4\nmaster_sites "+server.URL+"/\n"+declarations)
			request.Action = record.RefreshChecksums
			result, err := service.Prepare(t.Context(), request)
			require.NoError(t, err)
			require.Len(t, result.Commits, 1)
			require.Equal(t, "fixture: refresh checksums", result.Commits[0].Subject)
			require.Contains(t, string(result.Files[0].After), fmt.Sprintf("%x", sha256.Sum256([]byte("/source.tar.gz"))))
			final := result.Fidelity[len(result.Fidelity)-1].After.Ports["fixture"]
			require.Equal(t, "7.2", final.Version)
			require.Equal(t, 4, final.Revision)
			require.Equal(t, int64(len(result.Downloads)), reads.Load())
			request.Source = record.Source{Tree: result.PreparedTree}
			again, err := service.Prepare(t.Context(), request)
			require.NoError(t, err)
			require.Equal(t, result.PreparedTree, again.PreparedTree)
			require.Empty(t, again.Files)
			require.Empty(t, again.Commits)
		})
	}
}

func TestRefreshChecksumsRejectsCustomFetchBeforeDownload(t *testing.T) {
	t.Parallel()
	var reads atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { reads.Add(1) }))
	defer server.Close()
	service, request := preparationFixture(t, "master_sites "+server.URL+"/\ndistfiles source.tar.gz\nchecksums sha256 "+strings.Repeat("0", 64)+"\npre-fetch { error custom }\n")
	request.Action = record.RefreshChecksums
	result, err := service.Prepare(t.Context(), request)
	require.ErrorIs(t, err, preparation.ErrUnsupported)
	require.Empty(t, result.Commits)
	require.Zero(t, reads.Load())
}
