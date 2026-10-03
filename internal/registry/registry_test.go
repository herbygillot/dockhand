package registry

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

// Each registry name is read by its registry's documented answer: PyPI's
// project links, crates.io's repository, and a module path's own forge or
// its go-import meta tag. One that names no source, or a name that isn't
// one, is said.
func TestARegistryNameIsReadAsItsSource(t *testing.T) {
	t.Parallel()
	var mu sync.Mutex
	var agents []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		agents = append(agents, r.Header.Get("User-Agent"))
		mu.Unlock()
		switch r.URL.Path {
		case "/pypi/flatbuffers/json":
			fmt.Fprint(w, `{"info":{"home_page":"https://flatbuffers.dev","project_urls":{"Documentation":"https://flatbuffers.dev","Source":"https://github.com/google/flatbuffers"}}}`)
		case "/pypi/homepaged/json":
			fmt.Fprint(w, `{"info":{"home_page":"https://github.com/owner/homepaged","project_urls":{}}}`)
		case "/pypi/sourceless/json":
			fmt.Fprint(w, `{"info":{"home_page":"https://example.org","project_urls":null}}`)
		case "/crates/ripgrep":
			fmt.Fprint(w, `{"crate":{"name":"ripgrep","repository":"https://github.com/BurntSushi/ripgrep"}}`)
		case "/crates/bare":
			fmt.Fprint(w, `{"crate":{"name":"bare"}}`)
		case "/golang.org/x/tools", "/golang.org/x/tools/gopls":
			fmt.Fprint(w, `<html><head><meta name="go-import" content="golang.org/x/tools git https://go.googlesource.com/tools"></head></html>`)
		case "/sigs.k8s.io/yaml":
			fmt.Fprint(w, `<html><head><meta name="go-import" content="sigs.k8s.io/yaml git https://github.com/kubernetes-sigs/yaml.git"></head></html>`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	c := Client{HTTP: server.Client(), PyPI: server.URL + "/pypi/", Crates: server.URL + "/crates/", GoGet: server.URL + "/"}
	for name, want := range map[string]string{
		"pypi:flatbuffers":                    "https://github.com/google/flatbuffers",
		"pypi:homepaged":                      "https://github.com/owner/homepaged",
		"crates:ripgrep":                      "https://github.com/BurntSushi/ripgrep",
		"go:github.com/charmbracelet/mods/v2": "https://github.com/charmbracelet/mods",
		"go:codeberg.org/kevinschoon/pomo":    "https://codeberg.org/kevinschoon/pomo",
		"go:golang.org/x/tools":               "https://go.googlesource.com/tools",
		"go:sigs.k8s.io/yaml":                 "https://github.com/kubernetes-sigs/yaml",
		"https://github.com/o/p":              "https://github.com/o/p",
	} {
		source, err := c.Source(context.Background(), name)
		require.NoError(t, err, name)
		require.Equal(t, want, source, name)
	}
	for name, why := range map[string]error{"pypi:sourceless": ErrNoSource, "crates:bare": ErrNoSource} {
		_, err := c.Source(context.Background(), name)
		require.True(t, errors.Is(err, why), "%s: %v", name, err)
	}
	// A module below its repository's root is refused by its
	// subdirectory, whether its path names its forge or a go-import tag
	// says its root; a major version's suffix is no subdirectory.
	for name, below := range map[string]string{
		"go:github.com/hashicorp/vault/api": "subdirectory api",
		"go:github.com/o/p/tools/cmd/v3":    "subdirectory tools/cmd",
		"go:golang.org/x/tools/gopls":       "subdirectory gopls",
	} {
		_, err := c.Source(context.Background(), name)
		require.ErrorIs(t, err, ErrSubdirectory, name)
		require.ErrorContains(t, err, below, name)
	}
	_, err := c.Source(context.Background(), "crates:missing")
	require.ErrorContains(t, err, "crates:missing")
	for _, bad := range []string{"pypi:../etc", "crates:a b", "go:not a path"} {
		_, err := c.Source(context.Background(), bad)
		require.Error(t, err, bad)
	}
	mu.Lock()
	defer mu.Unlock()
	require.Contains(t, agents, "dockhand/2", "crates.io asks for a User-Agent")
	require.True(t, Named("crates:ripgrep"))
	require.False(t, Named("https://github.com/o/p"))
}
