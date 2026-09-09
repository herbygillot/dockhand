package staging

import (
	"context"
	"github.com/herbygillot/dockhand/internal/tool"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/git/gittest"
	"github.com/herbygillot/dockhand/internal/macports/build"
	"github.com/herbygillot/dockhand/internal/platform"
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
	repo := gittest.PortsTree(t, tool.NewFinder(nil))
	sha, err := repo.RevParse(t.Context(), "HEAD")
	require.NoError(t, err)

	root, err := tempdir.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = root.Remove() })

	s := &Stager{repo: repo, temp: root}
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
	repo := gittest.PortsTree(t, tool.NewFinder(nil))
	root, err := tempdir.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = root.Remove() })

	s := &Stager{repo: repo, temp: root}
	dirs, err := s.Baseline(context.Background(), "",
		[]record.Subject{{Port: "jq", Portdir: "sysutils/jq"}})
	require.NoError(t, err)
	assert.Empty(t, dirs)
}

// "I WAS NOT TOLD WHICH PLATFORM" IS NOT "THE PORT DECLINES", and the
// gap between those two sentences cost a whole cohort.
//
// A zero platform.Release means os.major 0. Nothing has ever shipped as
// macOS 0, so qt5's min-version callback fires, qt6's fires, and every
// compiler.cxx_standard port declines against it — and run.Plan reads a
// known_fail member as the PORT refusing the platform and records
// record.Unsupported without booting a guest. Measured on cmark's
// dependents: Aseprite, PrismLauncher and nheko all came back
// "unsupported", and not one of them has known_fail in its Portfile.
//
// The severity is that unsupported is not an error. It exits 0, it is
// documented as frequently being the change working exactly as
// intended, and publish counts it as an outcome about the port that a
// promotion may proceed on. A cohort that built nothing looked fine.
//
// Unread is the safe direction: run.Plan schedules a member whose
// preflight could not answer, because a preflight exists to save a VM
// and never to invent a verdict.
func TestAPreflightWithNoPlatformDoesNotDeclineThePort(t *testing.T) {
	s := &Stager{}
	pf := s.preflight(context.Background(), t.TempDir(), record.Subject{Port: "nheko"}, platform.Release{})

	assert.False(t, pf.Read, "an unanswerable preflight is unread, never a decline")
	assert.False(t, pf.KnownFail, "and it must not claim the port declares known_fail")
	require.Error(t, pf.Err, "and it says why, rather than answering silently")
	assert.Contains(t, pf.Err.Error(), "no platform")
}
