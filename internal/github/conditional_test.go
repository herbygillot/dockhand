package github

import (
	"bytes"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// answering is a round tripper answering every request with a body and an
// ETag, and counting what it was asked.
type answering struct {
	body  []byte
	asked int
}

func (a *answering) RoundTrip(req *http.Request) (*http.Response, error) {
	a.asked++
	return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Etag": {`"v1"`}}, Body: io.NopCloser(bytes.NewReader(a.body)), Request: req}, nil
}

// An answer past what's kept goes through whole and isn't kept, where it
// was cut at keptLimit+1 bytes: wasmtime's releases read as JSON that ends
// early (rc1's acceptance). One within the limit is kept.
func TestAnAnswerPastTheLimitGoesThroughWhole(t *testing.T) {
	t.Parallel()
	large := []byte(strings.Repeat("x", keptLimit+100))
	next := &answering{body: large}
	transport := conditional{next: next, cache: NewConditionalCache(t.TempDir())}
	req, err := http.NewRequest(http.MethodGet, "https://api.github.com/repos/bytecodealliance/wasmtime/releases", nil)
	require.NoError(t, err)
	response, hit, err := transport.roundTrip(req)
	require.NoError(t, err)
	require.False(t, hit)
	read, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())
	require.Equal(t, len(large), len(read), "whole")
	_, kept := transport.cache.load(conditionalKey(req))
	require.False(t, kept, "not kept")

	next.body = []byte(`[{"tag_name":"v1"}]`)
	response, _, err = transport.roundTrip(req)
	require.NoError(t, err)
	read, _ = io.ReadAll(response.Body)
	require.Equal(t, next.body, read)
	_, kept = transport.cache.load(conditionalKey(req))
	require.True(t, kept, "within the limit, kept")
}
