package tart

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/buildenv"
	"github.com/herbygillot/dockhand/internal/engine"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/testsupport"
)

// A real check in a real clone of dockhand's Tahoe image, of two ports of
// the ports tree DOCKHAND_TEST_PORTS_TREE names at its HEAD: tree, with no
// dependencies, and pv, whose dependency the guest installs first. It takes
// several minutes and a VM slot, so it runs only with DOCKHAND_TEST_TART_LIVE
// set, on a Mac with the image.
func TestLiveCheckInATahoeGuest(t *testing.T) {
	if os.Getenv("DOCKHAND_TEST_TART_LIVE") == "" {
		t.Skip("set DOCKHAND_TEST_TART_LIVE to build in a real Tart guest")
	}
	tree := os.Getenv("DOCKHAND_TEST_PORTS_TREE")
	if tree == "" {
		t.Skip("set DOCKHAND_TEST_PORTS_TREE to a macports-ports checkout")
	}
	e, err := engine.Open(t.Context(), engine.Options{Tree: tree, Database: filepath.Join(t.TempDir(), "dockhand.db"), Tclsh: testsupport.MacPortsTclsh(t), Worktrees: filepath.Join(t.TempDir(), "worktrees")})
	require.NoError(t, err)
	defer e.Close()
	commit, err := e.Repo.Resolve(t.Context(), "HEAD")
	require.NoError(t, err)
	treeID, err := e.Repo.Resolve(t.Context(), "HEAD^{tree}")
	require.NoError(t, err)
	p := &Provider{Repo: e.Repo, Index: e.PortIndex}
	job := buildenv.Job{
		Run:         model.Run{ID: "run_live", Number: 1},
		Execution:   model.GuestExecution{ID: "ex_live", Attempt: 1},
		Revision:    model.Revision{Source: model.Source{Commit: model.ObjectID(commit), Tree: model.ObjectID(treeID), Base: model.ObjectID(commit)}},
		Plan:        model.Plan{Tests: model.TestsDeclared},
		Environment: model.Environment{Provider: "tart", Platform: tahoe},
		Commit:      commit,
		Directory:   t.TempDir(),
		Targets: []buildenv.Target{
			{PlanTarget: model.PlanTarget{ID: "tree", Target: model.Target{Name: "tree", Portfile: "sysutils/tree/Portfile"}}},
			{PlanTarget: model.PlanTarget{ID: "pv", Target: model.Target{Name: "pv", Portfile: "sysutils/pv/Portfile"}}},
		},
	}
	build := &fakeBuild{}
	err = p.Execute(t.Context(), job, build)
	for _, message := range build.progress {
		t.Log(message)
	}
	require.NoError(t, err)
	require.Len(t, build.results, 2)
	for _, result := range build.results {
		log, _ := os.ReadFile(result.Log)
		require.Equal(t, model.OutcomePassed, result.Outcome, "%s: %s", result.Target, tail(string(log), 2000))
		require.NotEmpty(t, log)
	}
	images, err := p.machine.Images(t.Context())
	require.NoError(t, err)
	require.NotContains(t, images, "dockhand-check-run-live-tahoe-1", "the clone is deleted")
}
