package command

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/macos"
	"github.com/herbygillot/dockhand/internal/provider/tart"
)

// fakeImages stands in for dockhand's Tart images.
type fakeImages struct {
	status tart.Status
	result tart.SetupResult
	err    error
	asked  []tart.SetupOptions
}

func (f *fakeImages) Status(context.Context) (tart.Status, error) { return f.status, f.err }

func (f *fakeImages) Setup(_ context.Context, options tart.SetupOptions, progress io.Writer) (tart.SetupResult, error) {
	f.asked = append(f.asked, options)
	fmt.Fprintln(progress, "Checking Tart images for Sonoma...")
	return f.result, f.err
}

func useImages(t *testing.T, images *fakeImages) {
	t.Helper()
	testTartImages = images
	t.Cleanup(func() { testTartImages = nil })
}

func release(t *testing.T, name string) macos.Release {
	t.Helper()
	found, err := macos.ParseRelease(name)
	require.NoError(t, err)
	return found
}

func TestProvidersWithoutTart(t *testing.T) {
	out, _, err := dockhand(t, "providers")
	require.NoError(t, err)
	require.Equal(t, "tart     · needs Tart: brew install cirruslabs/cli/tart, then dockhand providers setup tart\n"+
		"github   · needs a GitHub login and your fork's Actions enabled\n", out)

	_, _, err = dockhand(t, "providers", "setup", "tart")
	require.ErrorContains(t, err, "Tart isn't installed: brew install cirruslabs/cli/tart")
}

func TestProvidersShowTheImages(t *testing.T) {
	images := &fakeImages{}
	useImages(t, images)
	images.status = tart.Status{Host: release(t, "tahoe")}
	out, _, err := dockhand(t, "providers")
	require.NoError(t, err)
	require.Contains(t, out, "tart     · not set up: dockhand providers setup tart   (macOS 26, up to 60 GB)\n")

	images.status.Base = []macos.Release{release(t, "sonoma"), release(t, "sequoia")}
	out, _, err = dockhand(t, "providers")
	require.NoError(t, err)
	require.Contains(t, out, "tart     ✓ images for macOS 14, 15; none for this Mac's macOS 26: dockhand providers setup tart\n")

	images.status.Base = append(images.status.Base, release(t, "tahoe"))
	out, _, err = dockhand(t, "providers")
	require.NoError(t, err)
	require.Contains(t, out, "tart     ✓ images for macOS 14, 15, 26\n")

	images.err = errors.New("listing dockhand's Tart images: tart list failed")
	out, _, err = dockhand(t, "providers")
	require.NoError(t, err, "a provider that can't say is shown, not fatal")
	require.Contains(t, out, "tart     ! listing dockhand's Tart images: tart list failed\n")
}

func TestProvidersShowACommandProvider(t *testing.T) {
	w := newWorld(t)
	require.NoError(t, os.MkdirAll(filepath.Join(w.home, ".dockhand"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(w.home, ".dockhand", "config.toml"), []byte("[providers.command]\nrun = \"~/bin/build-ports\"\nname = \"my build box\"\n"), 0o644))
	out, _, err := dockhand(t, "providers")
	require.NoError(t, err)
	require.Contains(t, out, "command  ✓ my build box: ~/bin/build-ports\n")
}

func TestSetupTart(t *testing.T) {
	images := &fakeImages{result: tart.SetupResult{Image: "dockhand-base-sonoma", Release: release(t, "sonoma"),
		MacPorts: tart.DefaultMacPorts, CommandLineTools: "16.2.0.0.1.1733547573"}}
	useImages(t, images)
	out, _, err := dockhand(t, "providers", "setup", "tart", "sonoma", "--macports-version", "2.12.5")
	require.NoError(t, err)
	require.Equal(t, []tart.SetupOptions{{Release: "sonoma", MacPortsVersion: "2.12.5"}}, images.asked)
	require.Contains(t, out, "Checking Tart images for Sonoma...\n", "the provisioner's progress is shown")
	require.Contains(t, out, "Made dockhand-base-sonoma: macOS 14 (Sonoma) with Command Line Tools 16.2.0.0.1.1733547573 and MacPorts "+tart.DefaultMacPorts+".\n")
	require.Contains(t, out, "Next: dockhand check --on tart:sonoma\n")

	images.result.Reused = true
	out, _, err = dockhand(t, "providers", "setup", "tart", "--check")
	require.NoError(t, err)
	require.Equal(t, tart.SetupOptions{Check: true}, images.asked[1], "no release is this Mac's")
	require.Contains(t, out, "Ready: dockhand-base-sonoma, macOS 14 (Sonoma) with Command Line Tools 16.2.0.0.1.1733547573 and MacPorts "+tart.DefaultMacPorts+", checked in a disposable clone.\n")

	images.err = errors.New("setup: image dockhand-base-sonoma failed validation")
	_, _, err = dockhand(t, "providers", "setup", "tart", "--rebuild")
	require.ErrorContains(t, err, "failed validation")
	require.Equal(t, tart.SetupOptions{Rebuild: true}, images.asked[2])
}

func TestInitShowsTheProviders(t *testing.T) {
	newWorld(t)
	images := &fakeImages{status: tart.Status{Host: release(t, "tahoe")}}
	useImages(t, images)
	out, _, err := dockhand(t, "init")
	require.NoError(t, err)
	require.Contains(t, out, "  Providers    tart     · not set up: dockhand providers setup tart   (macOS 26, up to 60 GB)\n"+
		"               github   · needs a GitHub login and your fork's Actions enabled\n"+
		"  Publishing   ")
}
