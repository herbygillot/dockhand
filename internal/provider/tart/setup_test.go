package tart

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/macos"
	tartvm "github.com/herbygillot/dockhand/internal/tart"
)

// Status lists the releases with a base image, and with an Xcode one,
// oldest first, beside this Mac's release; clones and golden copies aren't
// images a check uses.
func TestStatusListsReleasesWithImages(t *testing.T) {
	t.Parallel()
	mac := newMac()
	mac.images = []string{"dockhand-base-tahoe", "dockhand-base-sonoma", "dockhand-golden-ventura", "dockhand-xcode-sequoia", "dockhand-check-run-1-tahoe-1"}
	status, err := testProvider(mac).Status(t.Context())
	require.NoError(t, err)
	slugs := func(releases []macos.Release) (names []string) {
		for _, release := range releases {
			names = append(names, release.Slug)
		}
		return names
	}
	require.Equal(t, []string{"sonoma", "tahoe"}, slugs(status.Base))
	require.Equal(t, []string{"sequoia"}, slugs(status.Xcode))
	require.Equal(t, "tahoe", status.Host.Slug)
}

// Setup refuses contradictory options and unknown releases before it
// touches anything, and names the cost of making an image before it
// starts; one that exists, or has a golden copy, costs only a check.
func TestSetupNamesItsCost(t *testing.T) {
	t.Parallel()
	mac := newMac()
	mac.images = []string{"dockhand-base-tahoe", "dockhand-golden-sonoma"}
	p := testProvider(mac)
	// The provisioner finds no Tart, so it stops after the listing.
	p.Tart = tartvm.Client{Executable: "/nonexistent/tart", Home: t.TempDir()}

	_, err := p.Setup(t.Context(), SetupOptions{Check: true, Rebuild: true}, nil)
	require.ErrorContains(t, err, "--check and --rebuild ask for opposite things")
	_, err = p.Setup(t.Context(), SetupOptions{Release: "leopard"}, nil)
	require.ErrorContains(t, err, "unknown release")

	for options, costly := range map[SetupOptions]bool{
		{Release: "sequoia"}:              true,
		{Release: "tahoe", Rebuild: true}: true,
		{}:                                false, // this Mac's, which exists
		{Release: "sonoma"}:               false, // restored from its golden copy
		{Release: "sequoia", Check: true}: false,
	} {
		var progress bytes.Buffer
		_, err := p.Setup(t.Context(), options, &progress)
		require.Error(t, err, "%+v", options)
		if costly {
			require.Contains(t, progress.String(), "Making dockhand-base-"+options.Release+" for macOS ", "%+v", options)
			require.Contains(t, progress.String(), "takes up to 60 GB of disk", "%+v", options)
		} else {
			require.NotContains(t, progress.String(), "Making", "%+v", options)
		}
	}
}
