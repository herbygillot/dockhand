package cli

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/git/gittest"
	"github.com/herbygillot/dockhand/internal/macports/build"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/tempdir"
)

// THE BASELINE OVERLAY IS A PORTS TREE, NOT A BAG OF PORTDIRS, and for
// the whole of the overhaul it was a bag of portdirs.
//
// Stage materializes build.ResourcesDir beside its members; Baseline did
// not — while the tart provider stages both overlays through one
// function that tars _resources out of whichever root it is handed. So
// the host tar was asked for a directory that was not there on EVERY
// baseline, for EVERY port, on every run, and the ABI comparison has
// never produced a measurement.
//
// The provider's own comment states the consequence in advance: an
// overlay without _resources "has no archive site at all", and that is
// "the baseline's entire second step, which means the ABI comparison
// cannot be made for any port anywhere until this is staged". It was
// written about the branch overlay and was true of this one throughout.
func TestBaselineStagesTheResourcesTreeBesideItsPortdirs(t *testing.T) {
	repo := gittest.PortsTree(t, testFinder())
	sha, err := repo.RevParse(t.Context(), "HEAD")
	require.NoError(t, err)

	root, err := tempdir.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = root.Remove() })

	s := &stager{repo: repo, temp: root}
	dirs, err := s.Baseline(context.Background(), sha,
		[]record.Subject{{Port: "jq", Portdir: "sysutils/jq"}})
	require.NoError(t, err)
	require.Len(t, dirs, 1)

	// The overlay root is the portdir's grandparent — the same root the
	// provider tars _resources out of.
	overlay := filepath.Dir(filepath.Dir(dirs[0]))
	_, statErr := os.Stat(filepath.Join(overlay, filepath.FromSlash(build.ResourcesDir)))
	assert.NoError(t, statErr,
		"the provider tars %s out of this root unconditionally; without it the host tar fails and no baseline is ever taken",
		build.ResourcesDir)
}

// AND A CHANGE WITH NO RECORDED BASE STAGES NOTHING AND SAYS SO WITH A
// NIL ERROR: that is a real absence, not a failure.
func TestBaselineWithNoBaseStagesNothing(t *testing.T) {
	repo := gittest.PortsTree(t, testFinder())
	root, err := tempdir.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = root.Remove() })

	s := &stager{repo: repo, temp: root}
	dirs, err := s.Baseline(context.Background(), "",
		[]record.Subject{{Port: "jq", Portdir: "sysutils/jq"}})
	require.NoError(t, err)
	assert.Empty(t, dirs)
}
