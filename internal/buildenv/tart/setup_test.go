package tart

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/macos"
	tartvm "github.com/herbygillot/dockhand/internal/tart"
	"github.com/herbygillot/dockhand/internal/testsupport"
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
	require.Equal(t, []string{"ventura"}, slugs(status.Restorable), "a golden copy with no base image beside it is restored, not made")
	require.False(t, status.HostRestorable())
	mac.images = []string{"dockhand-golden-tahoe"}
	status, err = testProvider(mac).Status(t.Context())
	require.NoError(t, err)
	require.True(t, status.HostRestorable())
	require.False(t, status.HasHostImage())
}

// Setup refuses contradictory options and unknown releases before it
// touches anything, and names the cost of making an image before it
// starts; one that exists, or has a golden copy, costs only a check.
func TestSetupNamesItsCost(t *testing.T) {
	t.Parallel()
	mac := newMac()
	mac.images = []string{"dockhand-base-tahoe", "dockhand-golden-sonoma", "dockhand-golden-xcode-ventura"}
	p := testProvider(mac)
	// The provisioner finds no Tart, so it stops after the listing.
	p.Tart = tartvm.Client{Env: os.Environ(), Executable: "/nonexistent/tart", Home: t.TempDir()}

	_, err := p.Setup(t.Context(), SetupOptions{Check: true, Rebuild: true}, nil)
	require.ErrorContains(t, err, "--check and --rebuild cannot be used together")
	_, err = p.Setup(t.Context(), SetupOptions{Release: "leopard"}, nil)
	require.ErrorContains(t, err, "unknown release")

	for _, test := range []struct {
		options SetupOptions
		costly  bool
	}{
		{SetupOptions{Release: "sequoia"}, true},
		{SetupOptions{Release: "tahoe", Rebuild: true}, true},
		{SetupOptions{}, false},                  // this Mac's, which exists
		{SetupOptions{Release: "sonoma"}, false}, // restored from its golden copy
		{SetupOptions{Release: "sequoia", Check: true}, false},
		{SetupOptions{Release: "tahoe", Xcode: "/x"}, true}, // its Xcode image, beside the base
		{SetupOptions{Release: "ventura", Xcode: "/x"}, false},
	} {
		options, costly := test.options, test.costly
		var progress bytes.Buffer
		_, err := p.Setup(t.Context(), options, &progress)
		require.Error(t, err, "%+v", options)
		image, disk := "dockhand-base-", "60 GB"
		if options.Xcode != "" {
			image, disk = "dockhand-xcode-", "65 GB"
		}
		if costly {
			require.Contains(t, progress.String(), "Creating "+image+options.Release+" for macOS ", "%+v", options)
			require.Contains(t, progress.String(), "requires up to "+disk+" of disk space", "%+v", options)
		} else {
			require.NotContains(t, progress.String(), "Creating", "%+v", options)
		}
	}

	// An Xcode image without its base image is refused before it costs
	// anything, since no check could build on it (the rc3 full run).
	var progress bytes.Buffer
	_, err = p.Setup(t.Context(), SetupOptions{Release: "sequoia", Xcode: "/x"}, &progress)
	require.ErrorContains(t, err, "is missing; run dockhand setup tart sequoia first, then rerun with --xcode")
	require.Empty(t, progress.String())
}

// A release's Xcode image installs what MacPorts' arm64 buildbot for it
// runs, unless the configuration names another for it, by name or number;
// a name that is no release is refused rather than ignored.
func TestAnXcodeImageFollowsMacPortsBuildbots(t *testing.T) {
	t.Parallel()
	sonoma, err := macos.ParseRelease("sonoma")
	require.NoError(t, err)
	require.Equal(t, "15.4", sonoma.Xcode, "what ports-14_arm64-builder runs")
	version, err := XcodeFor(sonoma, nil)
	require.NoError(t, err)
	require.Equal(t, "15.4", version)
	version, err = XcodeFor(sonoma, map[string]string{"14": "16.2", "tahoe": "26.4"})
	require.NoError(t, err)
	require.Equal(t, "16.2", version, "the configuration's, by release number")
	_, err = XcodeFor(sonoma, map[string]string{"leopard": "3.1"})
	require.ErrorContains(t, err, "providers.tart.xcode")
}

// A base image has no Xcode, whatever each release's Xcode is; an Xcode
// image has the one XcodeFor chooses.
func TestOnlyAnXcodeImageAsksForXcode(t *testing.T) {
	t.Parallel()
	p := testProvider(newMac())
	tahoe, err := macos.ParseRelease("tahoe")
	require.NoError(t, err)
	base, err := p.provisionConfig(tahoe, SetupOptions{Xcodes: map[string]string{"tahoe": "26.4"}})
	require.NoError(t, err)
	require.Empty(t, base.XcodeVersion)
	require.Empty(t, base.Xcode)
	xcode, err := p.provisionConfig(tahoe, SetupOptions{Xcode: "/archives"})
	require.NoError(t, err)
	require.Equal(t, "26.6", xcode.XcodeVersion)
	configured, err := p.provisionConfig(tahoe, SetupOptions{Xcode: "/archives", Xcodes: map[string]string{"26": "26.4"}})
	require.NoError(t, err)
	require.Equal(t, "26.4", configured.XcodeVersion)
}

// xcodes downloads what setup is missing into the folder setup looked in,
// where setup finds it; a failure is xcodes', and says why.
func TestXcodesDownloadsTheMissingXcode(t *testing.T) {
	t.Parallel()
	folder := t.TempDir()
	xcodes := filepath.Join(t.TempDir(), "xcodes")
	testsupport.WriteExecutable(t, xcodes, `#!/bin/sh
[ "$1" = download ] && [ "$3" = --directory ] || exit 2
echo "asked for $2" >&2
: > "$4/Xcode-$2.0+15F31d.xip"
`)
	p := testProvider(newMac())
	p.xcodes = xcodes
	sonoma, err := macos.ParseRelease("sonoma")
	require.NoError(t, err)
	_, _, err = macos.SelectXcode(folder, sonoma, "15.4")
	var missing *MissingXcode
	require.ErrorAs(t, err, &missing)

	var out, errs bytes.Buffer
	path, err := p.DownloadXcode(t.Context(), missing, strings.NewReader(""), &out, &errs)
	require.NoError(t, err)
	require.Equal(t, "Xcode-15.4.0+15F31d.xip", filepath.Base(path))
	require.Equal(t, "asked for 15.4\n", errs.String(), "xcodes talks to the person directly")

	failing := filepath.Join(t.TempDir(), "xcodes")
	testsupport.WriteExecutable(t, failing, "#!/bin/sh\necho 'Apple ID: Missing username or a password. Please try again.'\nexit 1\n")
	p.xcodes = failing
	errs.Reset()
	_, err = p.DownloadXcode(t.Context(), missing, strings.NewReader(""), &errs, &errs)
	require.ErrorIs(t, err, ErrXcodes)
	require.Contains(t, errs.String(), "Missing username or a password", "xcodes says why itself")

	missing.Folder = ""
	_, err = p.DownloadXcode(t.Context(), missing, strings.NewReader(""), &out, &errs)
	require.ErrorContains(t, err, "is a file; choose a download directory")
}

// What the images let a check do, read off which images there are.
func TestReadiness(t *testing.T) {
	t.Parallel()
	tahoe, err := macos.ReleaseForDarwin(25)
	require.NoError(t, err)
	sequoia, err := macos.ReleaseForDarwin(24)
	require.NoError(t, err)
	require.Equal(t, NoImage, Status{Host: tahoe}.Readiness())
	require.Equal(t, XcodeOnly, Status{Host: tahoe, Xcode: []macos.Release{tahoe}}.Readiness())
	require.Equal(t, NotForHost, Status{Host: tahoe, Base: []macos.Release{sequoia}}.Readiness())
	require.Equal(t, Ready, Status{Host: tahoe, Base: []macos.Release{sequoia, tahoe}}.Readiness())
	require.Equal(t, Ready, Status{Base: []macos.Release{sequoia}}.Readiness(), "a host release not known")
}

// An image made now installs this Mac's MacPorts where it's newer than the
// floor, so a stale image's rebuild clears its staleness: a hard 2.12.6
// made it again as stale (field testing, 2026-10-16).
func TestANewImageInstallsThisMacsMacPorts(t *testing.T) {
	t.Parallel()
	require.Equal(t, "2.12.7", (&Provider{macPorts: "2.12.7"}).newMacPorts(t.Context()))
	require.Equal(t, DefaultMacPorts, (&Provider{macPorts: "2.11.5"}).newMacPorts(t.Context()), "never older than the floor")
	require.Equal(t, DefaultMacPorts, (&Provider{macPorts: DefaultMacPorts}).newMacPorts(t.Context()))
}
