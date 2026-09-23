package app

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"path/filepath"
	"testing"

	forgegithub "github.com/herbygillot/dockhand/internal/forge/github"
	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/github"
	"github.com/herbygillot/dockhand/internal/publish"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/workflow"
	"github.com/stretchr/testify/require"
)

// The GitHub side of the provider choice builds through the person's own
// fork of the ports repository, and refuses any other head.
func TestGitHubBuildRequiresThePersonalFork(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name, user string
		refused    bool
	}{{"the fork's owner", "contributor", false}, {"someone else", "stranger", true}} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			for _, args := range [][]string{{"init", "-q", root}, {"-C", root, "remote", "add", "origin", "https://github.com/contributor/macports-ports.git"}} {
				out, err := exec.Command("git", args...).CombinedOutput()
				require.NoError(t, err, string(out))
			}
			repo, err := git.Open(t.Context(), root, "")
			require.NoError(t, err)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/user":
					fmt.Fprintf(w, `{"login":%q}`, test.user)
				case "/repos/contributor/macports-ports":
					fmt.Fprint(w, `{"full_name":"contributor/macports-ports","default_branch":"master","clone_url":"https://github.com/contributor/macports-ports.git","fork":true,"parent":{"full_name":"macports/macports-ports","default_branch":"master","clone_url":"https://github.com/macports/macports-ports.git"}}`)
				case "/repos/macports/macports-ports":
					fmt.Fprint(w, `{"full_name":"macports/macports-ports","default_branch":"master","clone_url":"https://github.com/macports/macports-ports.git"}`)
				case "/repos/contributor/macports-ports/actions/workflows/main.yml":
					fmt.Fprint(w, `{"id":7,"path":".github/workflows/main.yml","state":"active"}`)
				default:
					t.Errorf("unexpected request: %s", r.URL.Path)
					w.WriteHeader(404)
				}
			}))
			defer server.Close()
			client := &github.Client{Config: github.Config{BaseURL: server.URL, Token: "fixture-token"}}
			hosting := &forgegithub.Client{Client: client}
			services := &Services{githubClient: client, accounts: hosting, Workflow: &workflow.Engine{Publisher: &publish.Service{Repo: repo, Accounts: hosting, PullRequests: hosting, LockDirectory: filepath.Join(root, "locks")}}}
			config, err := services.githubBuild(t.Context(), record.Platform{OS: "darwin", Version: "25", Architecture: "arm64"}, false)
			if test.refused {
				// The publisher's destination resolution refuses the fork
				// first, by the login it recognizes the fork with.
				require.ErrorContains(t, err, "a fork that stranger owns")
				return
			}
			require.NoError(t, err)
			require.Equal(t, "github", config.Provider)
		})
	}
}
