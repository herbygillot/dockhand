package ghactions

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	githubapi "github.com/herbygillot/dockhand/internal/github"
)

// A job's log past the 64 MiB dockhand keeps is kept to it, and says so at
// its end, where a person reading it looks, rather than reading as a log
// whose runner stopped there.
func TestAJobLogCutAtItsBoundSaysSo(t *testing.T) {
	for _, size := range []int{1 << 10, maxJobLogBytes, maxJobLogBytes + 1} {
		var server *httptest.Server
		server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/api/repos/ada/macports-ports/actions/jobs/7/logs":
				http.Redirect(w, r, server.URL+"/log", http.StatusFound)
			case "/log":
				w.Write(bytes.Repeat([]byte("l"), size))
			default:
				http.NotFound(w, r)
			}
		}))
		client := &githubapi.Client{HTTP: server.Client(), Config: githubapi.Config{BaseURL: server.URL + "/api/", Token: "fixture-token"}}
		log, err := GitHub{Client: client}.JobLog(t.Context(), "ada/macports-ports", 7)
		server.Close()
		require.NoError(t, err, size)
		if size <= maxJobLogBytes {
			require.Len(t, log, size, "a log within the bound is kept whole")
			continue
		}
		require.Equal(t, maxJobLogBytes+len(cutJobLog), len(log))
		require.True(t, bytes.HasSuffix(log, []byte(cutJobLog)))
		require.Contains(t, cutJobLog, "this log was cut at 64 MiB")
	}
}
