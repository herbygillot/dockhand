package cmdtest

import (
	"path/filepath"
	"testing"

	"github.com/herbygillot/dockhand/internal/engine"
	"github.com/herbygillot/dockhand/internal/forge"
	"github.com/herbygillot/dockhand/internal/forge/forgetest"
	"github.com/herbygillot/dockhand/internal/testsupport"
)

func WithGitHub(t *testing.T, w World) *forgetest.GitHub {
	fork := filepath.Join(filepath.Dir(w.Upstream), "fork.git")
	testsupport.Git(t, filepath.Dir(w.Upstream), "clone", "-q", "--bare", w.Upstream, fork)
	testsupport.Git(t, w.Clone, "remote", "add", "fork", fork)
	g := forgetest.New(w.Upstream, fork)
	g.Others = []forge.PullRequestSummary{{Number: 34777, Title: "jq: update to 1.8.0"}}
	Stands(t).Forge = func(*engine.Engine) engine.Forge { return g }
	return g
}
