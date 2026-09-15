package github_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/herbygillot/dockhand/internal/forge/github"
	githubapi "github.com/herbygillot/dockhand/internal/github"
	"github.com/stretchr/testify/require"
)

func TestClientCanBeSharedByConcurrentRepositories(t *testing.T) {
	var calls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		fmt.Fprintf(w, `{"ref":"refs/tags/v2","object":{"type":"commit","sha":%q}}`, strings.Repeat("a", 40))
	}))
	defer server.Close()
	client := &github.Client{Client: &githubapi.Client{Config: githubapi.Config{BaseURL: server.URL}}}
	results := make(chan error, 8)
	var workers sync.WaitGroup
	for n := range 8 {
		repository, err := client.Repository("https://github.com", fmt.Sprintf("owner/project-%d", n))
		require.NoError(t, err)
		workers.Go(func() { _, err := repository.Tag(t.Context(), "v2"); results <- err })
	}
	workers.Wait()
	close(results)
	for err := range results {
		require.NoError(t, err)
	}
	require.Equal(t, int64(8), calls.Load())
}
