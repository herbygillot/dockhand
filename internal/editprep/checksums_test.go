package editprep_test

import (
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/herbygillot/dockhand/internal/editprep"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/testsupport"
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
			request.Action = model.EditChecksums
			request.Subject = ""
			result, err := service.Prepare(t.Context(), request)
			require.NoError(t, err)
			require.Len(t, result.Commits, 1)
			require.Equal(t, "fixture: refresh checksums", result.Commits[0].Subject)
			require.Contains(t, string(result.Files[0].After), fmt.Sprintf("%x", sha256.Sum256([]byte("/source.tar.gz"))))
			final := result.Fidelity[len(result.Fidelity)-1].After.Ports["fixture"]
			require.Equal(t, "7.2", final.Version)
			require.Equal(t, 4, final.Revision)
			require.Equal(t, int64(len(result.Downloads)), reads.Load())
			request.Source = model.Source{Tree: result.PreparedTree}
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
	service, request := preparationFixture(t, "master_sites "+server.URL+"/\ndistfiles source.tar.gz\nchecksums sha256 "+strings.Repeat("0", 64)+"\npre-fetch { set distfiles other.tar.gz }\n")
	request.Action, request.Subject = model.EditChecksums, ""
	result, err := service.Prepare(t.Context(), request)
	require.ErrorIs(t, err, editprep.ErrUnsupported)
	require.Empty(t, result.Commits)
	require.Zero(t, reads.Load())
}

// A checksum refresh switches the port's plain-HTTP links whose https
// forms answer, a site as a distfile it serves there, as part of the edit
// it evaluates, so the stored candidate's evaluation proves it with none of
// its own; one that doesn't answer is left (the person's "Let's try to get
// HTTP to HTTPS into 0.7.0", 2026-10-29).
func TestAnEditSwitchesTheLinksThatAnswerOverHTTPS(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, r.URL.Path) }))
	defer server.Close()
	site := server.URL + "/"
	service, request := preparationFixture(t, "homepage http://fixture.example/\nlong_description see ${homepage}docs\nlivecheck.url http://fixture.example/releases\nmaster_sites "+site+"\ndistfiles source.tar.gz\nchecksums sha256 "+strings.Repeat("0", 64)+" size 0\n")
	request.Action, request.Subject = model.EditChecksums, ""
	request.HTTPS = testsupport.HTTPSAnswers{"https://fixture.example/": true, "https://" + strings.TrimPrefix(site, "http://") + "source.tar.gz": true}
	result, err := service.Prepare(t.Context(), request)
	require.NoError(t, err)
	after := string(result.Files[0].After)
	require.Contains(t, after, "homepage https://fixture.example/\n")
	require.Contains(t, after, "livecheck.url http://fixture.example/releases\n", "it doesn't answer")
	require.Contains(t, after, "master_sites https://"+strings.TrimPrefix(site, "http://")+"\n")
	require.Len(t, result.HTTPS, 2)
	final := result.Prepared.Ports["fixture"]
	require.Equal(t, "https://fixture.example/", final.Options["homepage"], "the stored candidate evaluates so")
	require.Equal(t, "see https://fixture.example/docs", final.Options["long_description"], "a value derived from it with it")
}
