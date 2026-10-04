package engine

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	forgegithub "github.com/herbygillot/dockhand/internal/forge/github"
	githubapi "github.com/herbygillot/dockhand/internal/github"
	"github.com/herbygillot/dockhand/internal/registry"
)

// create reads a GitHub project as GitHub's API gives it: its description,
// homepage, and license; its newest release that's neither a draft nor a
// prerelease; and the build files at that release's commit, a file it
// hasn't left out. A project with no release is refused, and so is an
// answer for another repository (the test plan's step 3).
func TestCreateReadsAGitHubProject(t *testing.T) {
	t.Parallel()
	commit := strings.Repeat("a", 40)
	cargo := "[package]\nname = \"txt\"\nversion = \"1.2.0\"\n"
	releases := `[{"tag_name":"v1.3.0-rc1","prerelease":true,"published_at":"2026-09-30T00:00:00Z"},{"tag_name":"v1.2.0","published_at":"2026-09-01T00:00:00Z"},{"tag_name":"v1.1.0","published_at":"2026-08-01T00:00:00Z"}]`
	fullName := "erik/txt"
	var mu sync.Mutex
	set := func(name, listed string) { mu.Lock(); fullName, releases = name, listed; mu.Unlock() }
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		fullName, releases := fullName, releases
		mu.Unlock()
		switch r.URL.Path {
		case "/api/repos/erik/txt":
			fmt.Fprintf(w, `{"full_name":%q,"description":"a text editor","homepage":"https://txt.example","license":{"spdx_id":"MIT"}}`, fullName)
		case "/api/repos/erik/txt/releases":
			fmt.Fprint(w, releases)
		case "/api/repos/erik/txt/releases/tags/v1.2.0":
			fmt.Fprint(w, `{"tag_name":"v1.2.0","assets":[{"name":"txt-1.2.0.tar.gz"},{"name":"txt-1.2.0-aarch64-apple-darwin.zip"}]}`)
		case "/api/repos/erik/txt/git/ref/tags/v1.2.0":
			fmt.Fprintf(w, `{"ref":"refs/tags/v1.2.0","object":{"type":"commit","sha":%q}}`, commit)
		case "/pypi/txt/json":
			fmt.Fprint(w, `{"info":{"project_urls":{"Source":"https://github.com/erik/txt"},"version":"1.2.0"}}`)
		case "/pypi/newer/json":
			fmt.Fprint(w, `{"info":{"project_urls":{"Source":"https://github.com/erik/txt"},"version":"9.9"}}`)
		case "/api/repos/erik/txt/contents/Cargo.toml":
			fmt.Fprintf(w, `{"type":"file","encoding":"base64","size":%d,"name":"Cargo.toml","path":"Cargo.toml","content":%q}`, len(cargo), base64.StdEncoding.EncodeToString([]byte(cargo)))
		default:
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprint(w, `{"message":"Not Found"}`)
		}
	}))
	defer server.Close()
	reader := githubProjects{client: &forgegithub.Client{Client: &githubapi.Client{Config: githubapi.Config{BaseURL: server.URL + "/api"}}}}

	project, err := reader.Project(t.Context(), "https://github.com/erik/txt")
	require.NoError(t, err)
	require.Equal(t, Project{Owner: "erik", Name: "txt", Description: "a text editor", Homepage: "https://txt.example", License: "MIT", Tag: "v1.2.0",
		Assets: []string{"txt-1.2.0.tar.gz", "txt-1.2.0-aarch64-apple-darwin.zip"}, Files: map[string][]byte{"Cargo.toml": []byte(cargo)}}, project)

	listed := releases
	set("someone/else", listed)
	_, err = reader.Project(t.Context(), "https://github.com/erik/txt")
	require.ErrorContains(t, err, "erik/txt answered for someone/else")

	set("erik/txt", `[{"tag_name":"v2.0.0","draft":true}]`)
	_, err = reader.Project(t.Context(), "https://github.com/erik/txt")
	require.ErrorContains(t, err, "erik/txt has no release on GitHub")
	// Named by its registry, it is created at the tag of the version the
	// registry has (the Vx port's field testing, 2026-10-03).
	reader.registry = registry.Client{HTTP: server.Client(), PyPI: server.URL + "/pypi/"}
	project, err = reader.Project(t.Context(), "pypi:txt")
	require.NoError(t, err)
	require.Equal(t, "v1.2.0", project.Tag)
	require.Equal(t, []byte(cargo), project.Files["Cargo.toml"])
	_, err = reader.Project(t.Context(), "pypi:newer")
	require.ErrorContains(t, err, "erik/txt has no release on GitHub, and no tag v9.9 or 9.9 for the version its registry has")

	_, err = reader.Project(t.Context(), "https://gitlab.com/erik/txt")
	require.ErrorContains(t, err, "create reads projects on GitHub so far")
}
