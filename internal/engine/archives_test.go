package engine

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/buildenv"
	"github.com/herbygillot/dockhand/internal/model"
)

// A check's artifacts are the archives its passed targets built, a line a
// port and platform with where each is kept; a failed target built none,
// and an archive cleanup removed says it's gone (field testing's
// outdated-ports session, "dockhand artifacts").
func TestACheckSaysWhereItsArchivesAre(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e := f.open(t)
	provider := &scriptedProvider{keep: true, outcomes: map[model.TargetID]model.Outcome{"harbor-viewer": model.OutcomeFailed}}
	e.Providers = map[string]buildenv.Provider{"command": provider}
	revision := harborBranch(t, e)
	usePorts(e, harborPorts())
	branch, err := e.Branch(t.Context(), revision.Branch)
	require.NoError(t, err)
	plan, err := e.PlanCheck(t.Context(), PlanRequest{Revision: revision, Environments: []model.Environment{tahoeArm, tahoeX86}, Only: []string{"libharbor", "harbor-viewer"}})
	require.NoError(t, err)
	queued, err := e.Enqueue(t.Context(), branch, plan, model.OriginPerson)
	require.NoError(t, err)
	run, err := e.Drive(t.Context(), session(t, e), queued.ID)
	require.NoError(t, err)

	artifacts, err := e.Artifacts(t.Context(), run)
	require.NoError(t, err)
	var ports []string
	for _, artifact := range artifacts {
		ports = append(ports, artifact.Port)
		require.Empty(t, artifact.Missing, "%+v", artifact)
		require.FileExists(t, artifact.Path)
	}
	require.Equal(t, []string{"libharbor", "libharbor", "harbor-cli", "harbor-cli"}, ports, "each on both releases; harbor-viewer failed, and built none")

	require.NoError(t, os.Remove(artifacts[0].Path))
	artifacts, err = e.Artifacts(t.Context(), run)
	require.NoError(t, err)
	require.Contains(t, artifacts[0].Missing, "its archive is gone")
}
