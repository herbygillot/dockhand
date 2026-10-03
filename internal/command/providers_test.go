package command

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/buildenv/tart"
	"github.com/herbygillot/dockhand/internal/macos"
	"github.com/herbygillot/dockhand/internal/model"
)

// fakeImages stands in for dockhand's Tart images.
type fakeImages struct {
	status tart.Status
	result tart.SetupResult
	err    error
	asked  []tart.SetupOptions
	// errs are Setup's errors, one per call, before err.
	errs []error
	// xcodes is whether xcodes is installed, and downloaded what it was
	// asked for.
	xcodes     bool
	downloaded []string
	// failure is what xcodes says as it fails, when it does.
	failure string
}

func (f *fakeImages) Xcodes() (string, bool) { return "/opt/local/bin/xcodes", f.xcodes }

func (f *fakeImages) DownloadXcode(_ context.Context, missing *tart.MissingXcode, _ io.Reader, out, _ io.Writer) (string, error) {
	f.downloaded = append(f.downloaded, missing.Version)
	if f.failure != "" {
		fmt.Fprint(out, f.failure)
		return "", fmt.Errorf("%w: xcodes download %s: exit status 1", tart.ErrXcodes, missing.Version)
	}
	return filepath.Join(missing.Folder, "Xcode-"+missing.Version+".0+15F31d.xip"), nil
}

func (f *fakeImages) Status(context.Context) (tart.Status, error) { return f.status, f.err }

func (f *fakeImages) Setup(_ context.Context, options tart.SetupOptions, progress io.Writer) (tart.SetupResult, error) {
	f.asked = append(f.asked, options)
	fmt.Fprintln(progress, "Checking Tart images for Sonoma...")
	if len(f.errs) > 0 {
		err := f.errs[0]
		f.errs = f.errs[1:]
		return tart.SetupResult{}, err
	}
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
	require.Equal(t, "tart     · needs Tart: sudo port install tart, then dockhand setup tart\n"+
		"github   · needs a GitHub login and your fork's Actions enabled\n", out)

	_, _, err = dockhand(t, "providers", "setup", "tart")
	require.ErrorContains(t, err, "Tart isn't installed: sudo port install tart")
}

func TestProvidersShowTheImages(t *testing.T) {
	images := &fakeImages{}
	useImages(t, images)
	images.status = tart.Status{Host: release(t, "tahoe")}
	out, _, err := dockhand(t, "providers")
	require.NoError(t, err)
	require.Contains(t, out, "tart     · not set up: dockhand setup tart   (macOS 26, up to 60 GB)\n")

	images.status.Base = []macos.Release{release(t, "sonoma"), release(t, "sequoia")}
	out, _, err = dockhand(t, "providers")
	require.NoError(t, err)
	require.Contains(t, out, "tart     ✓ images for macOS 14, 15; none for this Mac's macOS 26: dockhand setup tart\n")

	images.status.Base = append(images.status.Base, release(t, "tahoe"))
	out, _, err = dockhand(t, "providers")
	require.NoError(t, err)
	require.Contains(t, out, "tart     ✓ images for macOS 14, 15, 26\n")

	images.status.Xcode = []macos.Release{release(t, "tahoe")}
	out, _, err = dockhand(t, "providers")
	require.NoError(t, err)
	require.Contains(t, out, "tart     ✓ images for macOS 14, 15, 26, with Xcode for 26\n")

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

	images.result = tart.SetupResult{Image: "dockhand-xcode-tahoe", Release: release(t, "tahoe"), MacPorts: tart.DefaultMacPorts, CommandLineTools: "26.6", Xcode: "26.1"}
	out, _, err = dockhand(t, "providers", "setup", "tart", "tahoe", "--xcode", "/Volumes/Xcodes")
	require.NoError(t, err)
	require.Equal(t, tart.SetupOptions{Release: "tahoe", Xcode: "/Volumes/Xcodes"}, images.asked[2])
	require.Contains(t, out, "Made dockhand-xcode-tahoe: macOS 26 (Tahoe) with Xcode 26.1, Command Line Tools 26.6, and MacPorts "+tart.DefaultMacPorts+".\n")

	images.err = errors.New("setup: image dockhand-base-sonoma failed validation")
	_, _, err = dockhand(t, "providers", "setup", "tart", "--rebuild")
	require.ErrorContains(t, err, "failed validation")
	require.Equal(t, tart.SetupOptions{Rebuild: true}, images.asked[3])
}

func TestInitShowsTheProviders(t *testing.T) {
	newWorld(t)
	images := &fakeImages{status: tart.Status{Host: release(t, "tahoe")}}
	useImages(t, images)
	out, _, err := dockhand(t, "init")
	require.NoError(t, err)
	require.Contains(t, out, "  Providers    tart     · not set up: dockhand setup tart   (macOS 26, up to 60 GB)\n"+
		"               github   · needs a GitHub login and your fork's Actions enabled\n"+
		"  Publishing   ")
}

// The plan says before a check what an environment can't build, what it
// needs, and, once for the environment, what would give it that.
func TestThePlanSaysWhatWontBeBuilt(t *testing.T) {
	tahoe := model.Environment{Provider: "tart", Platform: model.Platform{OS: "darwin", Version: "25", Architecture: "arm64"}, DeveloperTools: model.DeveloperToolsCommandLine}
	plan := model.Plan{Environments: []model.Environment{tahoe}, Tests: model.TestsDeclared,
		Targets: []model.PlanTarget{
			{ID: "libharbor", Target: model.Target{Name: "libharbor"}},
			{ID: "harbor-cli", Target: model.Target{Name: "harbor-cli"}},
		},
		Builds: []model.EnvironmentPlan{{Environment: tahoe, Order: []model.TargetID{"libharbor", "harbor-cli"},
			Dependencies: map[model.TargetID][]model.TargetID{"harbor-cli": {"libharbor"}},
			NeedsXcode:   []model.TargetID{"libharbor"},
			Unmet: []model.Unmet{
				{Target: "libharbor", Environment: tahoe, Needs: model.RequiresXcode},
				{Target: "harbor-cli", Environment: tahoe, Needs: model.RequiresXcode, Through: "libharbor"},
			}}}}
	var out bytes.Buffer
	writePlan(&out, plan, nil, func(model.Unmet) string { return "make an Xcode image" })
	require.Contains(t, out.String(), "Provider    tart macOS 26 (Tahoe) arm64 with the Command Line Tools · tests declared\n")
	require.Contains(t, out.String(), "Not built   libharbor on tart macOS 26 (Tahoe) arm64 with the Command Line Tools: needs Xcode\n"+
		"Not built   harbor-cli on tart macOS 26 (Tahoe) arm64 with the Command Line Tools: needs Xcode, through libharbor\n"+
		"            make an Xcode image\n")
	require.False(t, plan.Runnable())
	require.Equal(t, "nothing in it can be built where it asks; see Not built", unrunnable(plan))
}

// A missing Xcode is downloaded with xcodes when the person at the
// terminal agrees, and setup runs again; without a terminal, or without
// xcodes, setup says what to download and how xcodes would.
func TestSetupOffersToDownloadAMissingXcode(t *testing.T) {
	newWorld(t)
	missing := &tart.MissingXcode{Release: release(t, "sonoma"), Version: "15.4", Path: "/Volumes/Xcodes", Folder: "/Volumes/Xcodes"}
	images := &fakeImages{xcodes: true, errs: []error{missing}, result: tart.SetupResult{Image: "dockhand-xcode-sonoma", Release: release(t, "sonoma"),
		MacPorts: tart.DefaultMacPorts, CommandLineTools: "16.2", Xcode: "15.4"}}
	useImages(t, images)
	var out, errs bytes.Buffer
	err := Run(t.Context(), []string{"providers", "setup", "tart", "sonoma", "--xcode", "/Volumes/Xcodes"}, Streams{In: strings.NewReader("y\n"), Out: &out, Err: &errs, interactive: true})
	require.NoError(t, err, errs.String())
	require.Equal(t, []string{"15.4"}, images.downloaded)
	require.Len(t, images.asked, 2, "setup runs again once it's downloaded")
	require.Contains(t, out.String(), "Sonoma's Xcode is 15.4, and /Volumes/Xcodes has no archive of it.\n")
	require.Contains(t, errs.String(), "Download Xcode 15.4 with xcodes, signing in with your Apple ID? [y/N] ")
	require.Contains(t, out.String(), "Downloaded Xcode-15.4.0+15F31d.xip.\n")
	require.Contains(t, out.String(), "Made dockhand-xcode-sonoma: macOS 14 (Sonoma) with Xcode 15.4")

	images.errs, images.downloaded = []error{missing}, nil
	out.Reset()
	err = Run(t.Context(), []string{"providers", "setup", "tart", "sonoma", "--xcode", "/Volumes/Xcodes"}, Streams{In: strings.NewReader("n\n"), Out: &out, Err: &errs, interactive: true})
	require.ErrorContains(t, err, "Sonoma's Xcode is 15.4", "declined")
	require.Empty(t, images.downloaded)

	// Without a terminal, setup downloads with the sign-in xcodes keeps,
	// and shows what xcodes said when it can't.
	images.errs = []error{missing}
	out2, _, err := dockhand(t, "providers", "setup", "tart", "sonoma", "--xcode", "/Volumes/Xcodes")
	require.NoError(t, err)
	require.Equal(t, []string{"15.4"}, images.downloaded)
	require.Contains(t, out2, "Downloading Xcode 15.4 with xcodes...\n")
	require.Contains(t, out2, "Downloaded Xcode-15.4.0+15F31d.xip.\n")

	images.errs, images.downloaded = []error{missing}, nil
	images.failure = "Apple ID: Missing username or a password. Please try again.\n"
	_, _, err = dockhand(t, "providers", "setup", "tart", "sonoma", "--xcode", "/Volumes/Xcodes")
	require.ErrorIs(t, err, tart.ErrXcodes)
	require.ErrorContains(t, err, "xcodes said:\n  Apple ID: Missing username or a password. Please try again.\n")
	require.ErrorContains(t, err, "If it needs you to sign in, run it once at a terminal: xcodes download 15.4 --directory /Volumes/Xcodes")

	images.errs, images.xcodes, images.failure = []error{missing}, false, ""
	_, _, err = dockhand(t, "providers", "setup", "tart", "sonoma", "--xcode", "/Volumes/Xcodes")
	require.ErrorContains(t, err, "download Xcode 15.4 from https://developer.apple.com/download/all/; or install xcodes (sudo port install xcodes), and setup downloads it with your Apple ID")
	require.Len(t, images.downloaded, 1, "only what xcodes was asked for")
}
