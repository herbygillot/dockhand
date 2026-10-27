package setup

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
	"github.com/herbygillot/dockhand/internal/command/cmdtest"
	"github.com/herbygillot/dockhand/internal/command/view"
	"github.com/herbygillot/dockhand/internal/config"
	"github.com/herbygillot/dockhand/internal/engine"
	"github.com/herbygillot/dockhand/internal/macos"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/term"
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
	cmdtest.Hosted(t).TartImages = images
}

func release(t *testing.T, name string) macos.Release {
	t.Helper()
	found, err := macos.ParseRelease(name)
	require.NoError(t, err)
	return found
}

// localLine is the local provider's line, as this Mac has MacPorts at
// /opt/local or not.
const localLine = `local    (✓ in /opt/local, among the ports active there: check --on local|· no MacPorts at /opt/local; \[providers.local\] prefix names another)\n`

// onIntel says this Mac is an Intel one, which Tart's macOS guests
// don't run on, whichever Mac runs the test.
func onIntel(t *testing.T) {
	cmdtest.Hosted(t).TartSupported = func() bool { return false }
}

// An Intel Mac is told Tart isn't for it, and where its checks build,
// rather than to install Tart (the Intel CI job, 2026-10-03).
func TestAnIntelMacChecksOnGitHub(t *testing.T) {
	t.Parallel()
	onIntel(t)
	out := providers(t)
	require.Contains(t, out, "tart       needs Apple silicon; checks here build in a MacPorts prefix, with --on local, or on GitHub, with --on github\n")
	_, _, err := cmdtest.Dockhand(t, "setup", "tart")
	require.ErrorContains(t, err, "Tart macOS images require Apple silicon. To check on this Mac, use --on local; to use GitHub Actions, use --on github")
}

func TestProviderLinesWithoutTart(t *testing.T) {
	t.Parallel()
	out := providers(t)
	require.Regexp(t, "^tart     ! needs Tart: sudo port install tart, then dockhand setup tart\n"+
		"github   ! needs a GitHub login, dockhand setup github, and your fork's Actions enabled\n"+
		localLine+"$", out)

	_, _, err := cmdtest.Dockhand(t, "setup", "tart")
	require.ErrorContains(t, err, "Tart isn't installed: sudo port install tart")
}

func TestProviderLinesShowTheImages(t *testing.T) {
	t.Parallel()
	images := &fakeImages{}
	useImages(t, images)
	images.status = tart.Status{Host: release(t, "tahoe")}
	out := providers(t)
	require.Contains(t, out, "tart     ! not set up: dockhand setup tart   (macOS 26, up to 60 GB)\n")

	// A base image deleted with its golden copy kept is a restore, not a
	// download (Codex's review, checked against a268b6e5).
	images.status.Restorable = []macos.Release{release(t, "tahoe")}
	out = providers(t)
	require.Contains(t, out, "tart     ! no image: macOS 26's image was deleted and its golden copy kept: dockhand setup tart tahoe restores it, in about a minute\n")
	images.status.Base = []macos.Release{release(t, "sequoia")}
	out = providers(t)
	require.Contains(t, out, "tart     ✓ ready for macOS 15\n"+
		"           macOS 15 Sequoia   base image\n"+
		"           macOS 26's image was deleted and its golden copy kept: dockhand setup tart tahoe restores it, in about a minute\n", "and none for this Mac's isn't said again")
	images.status.Base, images.status.Restorable = nil, nil

	// An Xcode image made without the plain one is said (the rc1 full
	// stage's A2).
	images.status.Xcode = []macos.Release{release(t, "tahoe")}
	out = providers(t)
	require.Contains(t, out, "tart     ! Xcode image for macOS 26 ready; no plain image, which a check needs beside it: dockhand setup tart   (macOS 26, up to 60 GB)\n")
	images.status.Xcode = nil

	images.status.Base = []macos.Release{release(t, "sonoma"), release(t, "sequoia")}
	out = providers(t)
	require.Contains(t, out, "tart     ✓ ready for macOS 14 and 15\n"+
		"           macOS 14 Sonoma    base image\n"+
		"           macOS 15 Sequoia   base image\n"+
		"           none for this Mac's macOS 26: dockhand setup tart\n")

	images.status.Base = append(images.status.Base, release(t, "tahoe"))
	out = providers(t)
	require.Contains(t, out, "tart     ✓ ready for macOS 14, 15, and 26\n"+
		"           macOS 14 Sonoma    base image\n"+
		"           macOS 15 Sequoia   base image\n"+
		"           macOS 26 Tahoe     base image\n")

	images.status.Xcode = []macos.Release{release(t, "tahoe")}
	out = providers(t)
	require.Contains(t, out, "tart     ✓ ready for macOS 14, 15, and 26\n"+
		"           macOS 14 Sonoma    base image\n"+
		"           macOS 15 Sequoia   base image\n"+
		"           macOS 26 Tahoe     Xcode image\n", "a release's checks build in its Xcode image")

	// A provider that can't say is shown, not fatal.
	images.err = errors.New("listing dockhand's Tart images: tart list failed")
	out = providers(t)
	require.Contains(t, out, "tart     ! listing dockhand's Tart images: tart list failed\n")
}

func TestProviderLinesShowACommandProvider(t *testing.T) {
	t.Parallel()
	w := cmdtest.NewWorld(t)
	require.NoError(t, os.MkdirAll(filepath.Join(w.Home, ".dockhand"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(w.Home, ".dockhand", "config.toml"), []byte("[providers.command]\nrun = \"~/bin/build-ports\"\nname = \"my build box\"\n"), 0o644))
	out := providers(t)
	require.Contains(t, out, "command  ✓ my build box: ~/bin/build-ports\n")
}

func TestSetupTart(t *testing.T) {
	t.Parallel()
	images := &fakeImages{result: tart.SetupResult{Image: "dockhand-base-sonoma", Release: release(t, "sonoma"),
		MacPorts: tart.DefaultMacPorts, CommandLineTools: "16.2.0.0.1.1733547573"}}
	useImages(t, images)
	out, _, err := cmdtest.Dockhand(t, "setup", "tart", "sonoma", "--macports-version", "2.12.5")
	require.NoError(t, err)
	require.Equal(t, []tart.SetupOptions{{Release: "sonoma", MacPortsVersion: "2.12.5"}}, images.asked)
	require.Contains(t, out, "Checking Tart images for Sonoma...\n", "the provisioner's progress is shown")
	require.Contains(t, cmdtest.Flat(out), cmdtest.Flat("Made dockhand-base-sonoma: macOS 14 (Sonoma) with Command Line Tools 16.2.0.0.1.1733547573 and MacPorts "+tart.DefaultMacPorts+".\n"))
	require.Contains(t, out, "To check with this image, use --on tart:sonoma.\n")

	images.result.Reused = true
	out, _, err = cmdtest.Dockhand(t, "setup", "tart", "--check")
	require.NoError(t, err)
	require.Equal(t, tart.SetupOptions{Check: true}, images.asked[1], "no release is this Mac's")
	require.Contains(t, cmdtest.Flat(out), cmdtest.Flat("Ready: dockhand-base-sonoma, macOS 14 (Sonoma) with Command Line Tools 16.2.0.0.1.1733547573 and MacPorts "+tart.DefaultMacPorts+", checked in a disposable clone.\n"))

	images.result = tart.SetupResult{Image: "dockhand-xcode-tahoe", Release: release(t, "tahoe"), MacPorts: tart.DefaultMacPorts, CommandLineTools: "26.6", Xcode: "26.1"}
	out, _, err = cmdtest.Dockhand(t, "setup", "tart", "tahoe", "--xcode", "/Volumes/Xcodes")
	require.NoError(t, err)
	require.Equal(t, tart.SetupOptions{Release: "tahoe", Xcode: "/Volumes/Xcodes"}, images.asked[2])
	require.Contains(t, cmdtest.Flat(out), cmdtest.Flat("Made dockhand-xcode-tahoe: macOS 26 (Tahoe) with Xcode 26.1, Command Line Tools 26.6, and MacPorts "+tart.DefaultMacPorts+".\n"))

	images.err = errors.New("setup: image dockhand-base-sonoma failed validation")
	_, _, err = cmdtest.Dockhand(t, "setup", "tart", "--rebuild")
	require.ErrorContains(t, err, "failed validation")
	require.Equal(t, tart.SetupOptions{Rebuild: true}, images.asked[3])
}

func TestSetupShowsTheProviders(t *testing.T) {
	t.Parallel()
	cmdtest.NewWorld(t)
	images := &fakeImages{status: tart.Status{Host: release(t, "tahoe")}}
	useImages(t, images)
	out, _, err := cmdtest.Dockhand(t, "setup", "--yes")
	require.NoError(t, err)
	require.Contains(t, out, "  Providers    tart     ! not set up: dockhand setup tart   (macOS 26, up to 60 GB)\n"+
		"               github   ! needs a GitHub login, dockhand setup github, and your fork's Actions\n"+
		"               enabled\n")
	// The local provider's prefix, as this Mac has MacPorts there or not.
	require.Regexp(t, `\n               local    (✓ in /opt/local, among the ports active there: check --on local|· no MacPorts at /opt/local; \[providers.local\] prefix\n               names another)\n  Publishing   `, out)

	// With this Mac's image deleted and its golden copy kept, setup offers
	// the restore, not a 60 GB download (Codex's review, checked against
	// a268b6e5).
	images.status.Restorable = []macos.Release{release(t, "tahoe")}
	var told, asked bytes.Buffer
	err = cmdtest.RunFor(t, t.Context(), []string{"setup"}, term.Streams{In: strings.NewReader("n\nn\nn\nn\n"), Out: &told, Err: &asked}.AsTerminal())
	require.NoError(t, err)
	require.Contains(t, told.String()+asked.String(), "? Restore the Tart image for this Mac’s macOS version from its saved original? This takes about a minute and requires no download. [y/N] ")
	require.NotContains(t, told.String()+asked.String(), "It downloads macOS")
}

// The plan says before a check what an environment can't build, what it
// needs, and, once for the environment, what would give it that.
func TestThePlanSaysWhatWontBeBuilt(t *testing.T) {
	t.Parallel()
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
	view.WritePlan(&out, plan, nil, nil, func(model.Unmet) string { return "make an Xcode image" }, 8)
	require.Contains(t, out.String(), "Provider    tart macOS 26 (Tahoe) arm64 with the Command Line Tools · tests declared\n")
	require.Contains(t, out.String(), "Not built   libharbor on tart macOS 26 (Tahoe) arm64 with the Command Line Tools: needs Xcode\n"+
		"Not built   harbor-cli on tart macOS 26 (Tahoe) arm64 with the Command Line Tools: needs Xcode, through libharbor\n"+
		"            make an Xcode image\n")
	require.False(t, plan.Runnable())
	require.Equal(t, "none of the planned ports can build in the selected environments; see Not built", engine.Runnable(plan).Error())
}

// A missing Xcode is downloaded with xcodes when the person at the
// terminal agrees, and setup runs again; without a terminal, or without
// xcodes, setup says what to download and how xcodes would.
func TestSetupOffersToDownloadAMissingXcode(t *testing.T) {
	t.Parallel()
	cmdtest.NewWorld(t)
	missing := &tart.MissingXcode{Release: release(t, "sonoma"), Version: "15.4", Path: "/Volumes/Xcodes", Folder: "/Volumes/Xcodes"}
	images := &fakeImages{xcodes: true, errs: []error{missing}, result: tart.SetupResult{Image: "dockhand-xcode-sonoma", Release: release(t, "sonoma"),
		MacPorts: tart.DefaultMacPorts, CommandLineTools: "16.2", Xcode: "15.4"}}
	useImages(t, images)
	var out, errs bytes.Buffer
	err := cmdtest.RunFor(t, t.Context(), []string{"setup", "tart", "sonoma", "--xcode", "/Volumes/Xcodes"}, term.Streams{In: strings.NewReader("y\n"), Out: &out, Err: &errs}.AsTerminal())
	require.NoError(t, err, errs.String())
	require.Equal(t, []string{"15.4"}, images.downloaded)
	require.Len(t, images.asked, 2, "setup runs again once it's downloaded")
	require.Contains(t, out.String(), "Sonoma's Xcode is 15.4, and /Volumes/Xcodes has no archive of it.\n")
	require.Contains(t, errs.String(), "Download Xcode 15.4 with xcodes, signing in with your Apple ID? [y/N] ")
	require.Contains(t, out.String(), "Downloaded Xcode-15.4.0+15F31d.xip.\n")
	require.Contains(t, out.String(), "Made dockhand-xcode-sonoma: macOS 14 (Sonoma) with Xcode 15.4")

	images.errs, images.downloaded = []error{missing}, nil
	out.Reset()
	err = cmdtest.RunFor(t, t.Context(), []string{"setup", "tart", "sonoma", "--xcode", "/Volumes/Xcodes"}, term.Streams{In: strings.NewReader("n\n"), Out: &out, Err: &errs}.AsTerminal())
	require.ErrorContains(t, err, "Sonoma's Xcode is 15.4", "declined")
	require.Empty(t, images.downloaded)

	// Without a terminal, setup downloads with the sign-in xcodes keeps,
	// and shows what xcodes said when it can't.
	images.errs = []error{missing}
	out2, _, err := cmdtest.Dockhand(t, "setup", "tart", "sonoma", "--xcode", "/Volumes/Xcodes")
	require.NoError(t, err)
	require.Equal(t, []string{"15.4"}, images.downloaded)
	require.Contains(t, out2, "Downloading Xcode 15.4 with xcodes...\n")
	require.Contains(t, out2, "Downloaded Xcode-15.4.0+15F31d.xip.\n")

	images.errs, images.downloaded = []error{missing}, nil
	images.failure = "Apple ID: Missing username or a password. Please try again.\n"
	_, _, err = cmdtest.Dockhand(t, "setup", "tart", "sonoma", "--xcode", "/Volumes/Xcodes")
	require.ErrorIs(t, err, tart.ErrXcodes)
	require.ErrorContains(t, err, "xcodes said:\n  Apple ID: Missing username or a password. Please try again.\n")
	require.ErrorContains(t, err, "If xcodes asks you to sign in, run it once at a terminal: xcodes download 15.4 --directory /Volumes/Xcodes")

	images.errs, images.xcodes, images.failure = []error{missing}, false, ""
	_, _, err = cmdtest.Dockhand(t, "setup", "tart", "sonoma", "--xcode", "/Volumes/Xcodes")
	require.ErrorContains(t, err, "download Xcode 15.4 from https://developer.apple.com/download/all/; or install xcodes (sudo port install xcodes), and setup downloads it with your Apple ID")
	require.Len(t, images.downloaded, 1, "only what xcodes was asked for")
}

// providers is setup's Providers rows, as the configuration file has them,
// one a line.
func providers(t *testing.T) string {
	t.Helper()
	s := cmdtest.TestSettings(t)
	path, err := s.Places().ConfigPath(s.Getenv(config.PathVariable))
	require.NoError(t, err)
	file, err := s.Places().Load(path)
	require.NoError(t, err)
	return strings.Join(providerLines(t.Context(), cmdtest.TestSettings(t), file), "\n") + "\n"
}

// An image whose MacPorts is older than this Mac's is said, with how to
// make it again.
func TestProviderLinesSayAnImageBehindThisMacsMacPorts(t *testing.T) {
	t.Parallel()
	images := &fakeImages{}
	useImages(t, images)
	tahoe := release(t, "tahoe")
	sequoia := release(t, "sequoia")
	images.status = tart.Status{Host: tahoe, Base: []macos.Release{sequoia, tahoe}, Xcode: []macos.Release{tahoe},
		Behind: []tart.BehindImage{{Release: sequoia, MacPorts: "2.12.6", Host: "2.12.7"}, {Release: tahoe, Xcode: true, MacPorts: "2.12.6", Host: "2.12.7"}}}
	require.Contains(t, providers(t), "tart     ✓ ready for macOS 15 and 26\n"+
		"           macOS 15 Sequoia   base image    MacPorts 2.12.6, older than this Mac's 2.12.7\n"+
		"             dockhand setup tart sequoia --rebuild\n"+
		"           macOS 26 Tahoe     Xcode image   MacPorts 2.12.6, older than this Mac's 2.12.7\n"+
		"             dockhand setup tart tahoe --rebuild --xcode <folder>\n"+
		"             (Xcode "+tahoe.Xcode+"'s .xip, or a folder xcodes downloads it into)\n", "each image behind has its rebuild under it")
}

// Where a release's Xcode image, which its checks build in, is current,
// its base image behind it is said once, with no rebuild (the person,
// 2026-10-21 21:50Z: six rebuilds asked for that no check needed).
func TestProviderLinesSayOnlyTheImagesChecksBuildIn(t *testing.T) {
	t.Parallel()
	images := &fakeImages{}
	useImages(t, images)
	var releases []macos.Release
	for _, slug := range []string{"monterey", "ventura", "sonoma", "sequoia", "tahoe"} {
		releases = append(releases, release(t, slug))
	}
	status := tart.Status{Host: releases[4], Base: releases, Xcode: releases, MacPorts: map[string]string{}}
	for _, one := range releases {
		status.Behind = append(status.Behind, tart.BehindImage{Release: one, MacPorts: "2.12.6", Host: "2.12.7"})
		status.MacPorts["dockhand-base-"+one.Slug] = "2.12.6"
		status.MacPorts["dockhand-xcode-"+one.Slug] = "2.12.7"
	}
	images.status = status
	out := providers(t)
	require.Contains(t, out, "tart     ✓ ready for macOS 12, 13, 14, 15, and 26\n"+
		"           macOS 12 Monterey   Xcode image   MacPorts 2.12.7\n"+
		"           macOS 13 Ventura    Xcode image   MacPorts 2.12.7\n"+
		"           macOS 14 Sonoma     Xcode image   MacPorts 2.12.7\n"+
		"           macOS 15 Sequoia    Xcode image   MacPorts 2.12.7\n"+
		"           macOS 26 Tahoe      Xcode image   MacPorts 2.12.7\n"+
		"           Their base images have MacPorts 2.12.6, which no check builds with while the release has its Xcode image\n")
	require.NotContains(t, out, "--rebuild")
}

// An Xcode image made again says where its release's base image keeps an
// older MacPorts, which --xcode leaves as it is (field testing,
// 2026-10-16).
func TestAnXcodeRebuildSaysItsBaseKeepsAnOlderMacPorts(t *testing.T) {
	t.Parallel()
	cmdtest.NewWorld(t)
	images := &fakeImages{}
	useImages(t, images)
	tahoe := release(t, "tahoe")
	images.result = tart.SetupResult{Image: "dockhand-xcode-tahoe", Release: tahoe, MacPorts: "2.12.7", CommandLineTools: "26.0", Xcode: "26.0"}
	images.status = tart.Status{Host: tahoe, Base: []macos.Release{tahoe}, Xcode: []macos.Release{tahoe},
		Behind: []tart.BehindImage{{Release: tahoe, MacPorts: "2.12.6", Host: "2.12.7"}}}
	out, _, err := cmdtest.Dockhand(t, "setup", "tart", "tahoe", "--rebuild", "--xcode", t.TempDir())
	require.NoError(t, err)
	require.Contains(t, cmdtest.Flat(out), cmdtest.Flat("Its base image has MacPorts 2.12.6, older than this Mac's 2.12.7: dockhand setup tart tahoe --rebuild, without --xcode, makes it again.\n"), "the one sentence (the architecture review of 5897a82, R3-7)")
}
