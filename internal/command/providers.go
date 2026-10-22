package command

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/herbygillot/dockhand/internal/buildenv"
	"github.com/herbygillot/dockhand/internal/buildenv/tart"
	"github.com/herbygillot/dockhand/internal/command/host"
	"github.com/herbygillot/dockhand/internal/config"
	"github.com/herbygillot/dockhand/internal/engine"
	"github.com/herbygillot/dockhand/internal/github"
	"github.com/herbygillot/dockhand/internal/macos"
	"github.com/herbygillot/dockhand/internal/prose"
	"github.com/herbygillot/dockhand/internal/term"
)

// images is dockhand's Tart images, or nil when Tart isn't installed.
func imagesOf(s *settings) host.TartImages {
	if s.host.TartImages != nil {
		return s.host.TartImages
	}
	if _, err := s.host.LookTart("tart"); err != nil {
		return nil
	}
	return s.composeOptions().TartProvider()
}

// installTart installs Tart from MacPorts.
const installTart = "sudo port install tart"

func setupTartCommand(s *settings, streams term.Streams) *cobra.Command {
	var options tart.SetupOptions
	cmd := &cobra.Command{
		Use:   "tart [release]",
		Short: "Make the Tart image a release's checks build in",
		Long: `Makes dockhand-base-<release>, the image check --on tart clones for each
build, in dockhand's Tart home, ~/.dockhand/tart ($DOCKHAND_TART_HOME). The
release is this Mac's macOS unless named, such as tahoe or 15.

The image starts from Cirrus Labs' vanilla macOS image and holds the Command
Line Tools of the release's pinned generation and MacPorts. Making one
downloads the vanilla image the first time and takes up to ` + tart.SetupDisk + ` of disk.
A golden copy is kept beside it, and a lost image is restored from it.

Xcode is an add-on. --xcode, given an Xcode .xip from Apple or a folder of
them, makes dockhand-xcode-<release> beside the release's base image, which
it needs first, the same with Xcode too, in up to ` + tart.XcodeDisk + ` more. Its Xcode is the one MacPorts' arm64
buildbot for the release runs, so ports are built as MacPorts builds its
packages; providers.tart.xcode in the configuration names another for a
release. The archive of that version is required, never a newer one in its
place, nor a beta. A check builds a port that needs Xcode only there;
without it, the port isn't built, and the check says so.

When the image exists, setup checks it in a disposable clone and leaves it
as it is. --rebuild makes a replacement, and keeps the old one until the
new one has passed.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !s.host.TartSupported() {
				return errors.New("Tart's macOS images need Apple silicon, and this Mac isn't; checks here build on GitHub, with --on github")
			}
			if len(args) == 1 {
				options.Release = args[0]
			}
			_, file, _, err := s.options(cmd.Context())
			if err != nil {
				return err
			}
			options.Xcodes = file.Providers.Tart.Xcode
			images := imagesOf(s)
			if images == nil {
				return fmt.Errorf("checks build in Tart VMs, and Tart isn't installed: %s", installTart)
			}
			result, err := images.Setup(cmd.Context(), options, streams.Out)
			if missing := (*tart.MissingXcode)(nil); errors.As(err, &missing) {
				if err = offerXcode(cmd.Context(), images, streams, missing); err == nil {
					result, err = images.Setup(cmd.Context(), options, streams.Out)
				}
			}
			if err != nil {
				return err
			}
			release := fmt.Sprintf("macOS %s (%s)", result.Release.Product, result.Release.Name)
			made := fmt.Sprintf("%s with Command Line Tools %s and MacPorts %s", release, result.CommandLineTools, result.MacPorts)
			if result.Xcode != "" {
				made = fmt.Sprintf("%s with Xcode %s, Command Line Tools %s, and MacPorts %s", release, result.Xcode, result.CommandLineTools, result.MacPorts)
			}
			if result.Reused {
				fmt.Fprintf(streams.Out, "Ready: %s, %s, checked in a disposable clone.\n", result.Image, made)
			} else {
				fmt.Fprintf(streams.Out, "Made %s: %s.\n", result.Image, made)
			}
			if mac := result.HostMacPorts; mac != "" {
				fmt.Fprintf(streams.Out, "This Mac has MacPorts %s. Checks read ports with it and build them with the image's %s; --rebuild --macports-version %s makes them the same.\n",
					mac, result.MacPorts, mac)
			}
			// An Xcode image is an add-on beside the release's base image,
			// which keeps its own MacPorts: one made again with --xcode
			// left the base, and its golden copy, on 2.12.6, unsaid (field
			// testing, 2026-10-16).
			if result.Xcode != "" {
				if status, err := images.Status(cmd.Context()); err == nil {
					for _, behind := range status.Behind {
						if !behind.Xcode && behind.Release.Slug == result.Release.Slug {
							base := engine.MacPortsBehind{Name: "Its base image", Image: behind.MacPorts, Host: behind.Host, Remedy: engine.RemakeWords(behind.Release, false) + ", without --xcode, makes it again"}
							fmt.Fprintln(streams.Out, engine.OlderMacPorts([]engine.MacPortsBehind{base}, false)[0]+".")
						}
					}
				}
			}
			// Not a Next: line, which names only what the branch as it
			// stands would accept, and setup has no branch.
			fmt.Fprintf(streams.Out, "Checks build on it with --on tart:%s.\n", result.Release.Slug)
			return nil
		},
	}
	cmd.Flags().BoolVar(&options.Check, "check", false, "check the image in a disposable clone, making nothing")
	cmd.Flags().BoolVar(&options.Rebuild, "rebuild", false, "make a replacement even when the image exists")
	cmd.Flags().StringVar(&options.MacPortsVersion, "macports-version", "", "the MacPorts the image installs (default this Mac's, at least "+tart.DefaultMacPorts+")")
	cmd.Flags().StringVar(&options.Xcode, "xcode", "", "make the release's Xcode image, from an Xcode .xip or a folder of them")
	return cmd
}

// providerLines say whether each provider is ready, one line each, as
// setup shows them.
func providerLines(ctx context.Context, s *settings, file config.File) []string {
	tartLines := tartReadiness(ctx, s)
	lines := []string{fmt.Sprintf("%-8s %s", buildenv.Tart, tartLines[0])}
	for _, line := range tartLines[1:] {
		lines = append(lines, fmt.Sprintf("%-8s %s", "", line))
	}
	lines = append(lines, fmt.Sprintf("%-8s %s", buildenv.GitHub, githubReadiness(ctx, s)))
	if command := file.Providers.Command; command != nil {
		name := command.Name
		if name == "" {
			name = "your script"
		}
		lines = append(lines, fmt.Sprintf("%-8s ✓ %s: %s", buildenv.Command, name, command.Run))
	}
	return lines
}

// noTart says why Tart isn't for this Mac, and where its checks build.
const noTart = "· needs Apple silicon; checks here build on GitHub, with --on github"

// tartReadiness is setup's Tart line, and the lines under it: a release's
// each, where images are ready.
func tartReadiness(ctx context.Context, s *settings) []string {
	if !s.host.TartSupported() {
		return []string{noTart}
	}
	images := imagesOf(s)
	if images == nil {
		return []string{"· needs Tart: " + installTart + ", then dockhand setup tart"}
	}
	status, err := images.Status(ctx)
	if err != nil {
		return []string{"! " + err.Error()}
	}
	var mac string
	if status.Host.Product != "" {
		mac = fmt.Sprintf("macOS %s", status.Host.Product)
	}
	cost := "up to " + tart.SetupDisk
	if mac != "" {
		cost = mac + ", " + cost
	}
	products := func(releases []macos.Release) string {
		var names []string
		for _, release := range releases {
			names = append(names, release.Product)
		}
		return strings.Join(names, ", ")
	}
	// A base image deleted with its golden copy kept is restored, in
	// about a minute, never made again from a download (D-R3's, at setup).
	restore := ""
	if len(status.Restorable) > 0 {
		var commands []string
		for _, release := range status.Restorable {
			commands = append(commands, "dockhand setup tart "+release.Slug)
		}
		restore = fmt.Sprintf("macOS %s's image was deleted and its golden copy kept: %s restores it, in about a minute", products(status.Restorable), commands[0])
		if len(commands) > 1 {
			restore = fmt.Sprintf("macOS %s's images were deleted and their golden copies kept: %s restore them, in about a minute each", products(status.Restorable), prose.And(commands))
		}
	}
	switch status.Readiness() {
	case tart.XcodeOnly:
		if restore != "" {
			return []string{fmt.Sprintf("· Xcode image for macOS %s ready; no plain image, which a check needs beside it: %s", products(status.Xcode), restore)}
		}
		return []string{fmt.Sprintf("· Xcode image for macOS %s ready; no plain image, which a check needs beside it: dockhand setup tart   (%s)", products(status.Xcode), cost)}
	case tart.NoImage:
		if restore != "" {
			return []string{"· no image: " + restore}
		}
		return []string{fmt.Sprintf("· not set up: dockhand setup tart   (%s)", cost)}
	}
	// A heading, then a line for each release, aligned: the image its
	// checks build in, which is its Xcode image wherever it has one, and
	// that image's MacPorts, with its rebuild under it where it's behind
	// this Mac's (the person, 2026-10-21 21:50Z, on the one run-on line).
	var ready []string
	for _, release := range status.Base {
		ready = append(ready, release.Product)
	}
	lines := []string{"✓ ready for macOS " + prose.And(ready)}
	type row struct{ name, image, macports, rebuild string }
	var rows []row
	var baseBehind []string
	nameWidth, imageWidth := 0, 0
	for _, release := range macos.Known() {
		xcode, ok := status.CheckImage(release)
		if !ok {
			continue
		}
		one := row{name: "macOS " + release.Product + " " + release.Name, image: "base image"}
		if xcode {
			one.image = "Xcode image"
			if !slices.ContainsFunc(status.Base, func(base macos.Release) bool { return base.Darwin == release.Darwin }) {
				one.image = "Xcode image, no base image beside it: dockhand setup tart " + release.Slug
			}
			if behind, ok := status.BehindOf(release, false); ok && !slices.Contains(baseBehind, behind.MacPorts) {
				baseBehind = append(baseBehind, behind.MacPorts)
			}
		}
		if version := status.ImageMacPorts(release, xcode); version != "" {
			one.macports = "MacPorts " + version
		}
		if behind, ok := status.BehindOf(release, xcode); ok {
			one.macports = fmt.Sprintf("MacPorts %s, older than this Mac's %s", behind.MacPorts, behind.Host)
			one.rebuild = engine.RemakeWords(release, xcode)
		}
		nameWidth, imageWidth = max(nameWidth, len(one.name)), max(imageWidth, len(one.image))
		rows = append(rows, one)
	}
	for _, one := range rows {
		line := fmt.Sprintf("  %-*s   %-*s   %s", nameWidth, one.name, imageWidth, one.image, one.macports)
		lines = append(lines, strings.TrimRight(line, " "))
		if one.rebuild != "" {
			// The Xcode image's note goes on a line of its own, so no line
			// runs past the terminal.
			command, note, _ := strings.Cut(one.rebuild, " (")
			lines = append(lines, "    "+command)
			if note != "" {
				lines = append(lines, "    ("+note)
			}
		}
	}
	if len(baseBehind) > 0 {
		lines = append(lines, fmt.Sprintf("  Their base images have MacPorts %s, which no check builds with while the release has its Xcode image", strings.Join(baseBehind, " or ")))
	}
	if status.Readiness() == tart.NotForHost && !status.HostRestorable() {
		lines = append(lines, "  none for this Mac's "+mac+": dockhand setup tart")
	}
	if restore != "" {
		lines = append(lines, "  "+restore)
	}
	return lines
}

func githubReadiness(ctx context.Context, s *settings) string {
	switch github.LoginSource(ctx, s.signin()) {
	case "":
		return "· needs a GitHub login, dockhand setup github, and your fork's Actions enabled"
	case github.SourceKeychain:
		return "✓ with your GitHub login; your fork's Actions must be enabled"
	}
	return "✓ with your GitHub token; your fork's Actions must be enabled"
}

// missingSteps are the setup commands still to run, in order, for setup's
// Next: line, which jumped to update with Tart and the login undone (the
// rc1 full stage's A2): a Tart image where Tart can build, and a login
// where no token stands in for one.
func missingSteps(ctx context.Context, s *settings) []string {
	var steps []string
	if s.host.TartSupported() {
		if images := imagesOf(s); images != nil {
			if status, err := images.Status(ctx); err == nil && (status.Readiness() == tart.NoImage || status.Readiness() == tart.XcodeOnly) {
				steps = append(steps, "dockhand setup tart")
			}
		}
	}
	if github.LoginSource(ctx, s.signin()) == "" {
		steps = append(steps, "dockhand setup github")
	}
	return steps
}

// offerXcode downloads the Xcode setup is missing with xcodes. At a
// terminal it asks first, and xcodes asks there for the person's Apple ID
// when it needs it; dockhand never sees it. Without one it downloads, as
// setup downloads a vanilla image, with the sign-in xcodes keeps, and
// shows what xcodes said when it fails.
func offerXcode(ctx context.Context, images host.TartImages, streams term.Streams, missing *tart.MissingXcode) error {
	_, installed := images.Xcodes()
	switch {
	case missing.Folder == "":
		return missing
	case !installed:
		return fmt.Errorf("%w; or install xcodes (sudo port install xcodes), and setup downloads it with your Apple ID", missing)
	}
	fmt.Fprintf(streams.Out, "%s's Xcode is %s, and %s has no archive of it.\n", missing.Release.Name, missing.Version, missing.Folder)
	var path string
	var err error
	if streams.Terminal() {
		download, err := streams.AskYes(fmt.Sprintf("Download Xcode %s with xcodes, signing in with your Apple ID? [y/N] ", missing.Version), false)
		if err != nil {
			return err
		}
		if !download {
			return missing
		}
		path, err = images.DownloadXcode(ctx, missing, streams.In, streams.Out, streams.Err)
		if err != nil {
			return err
		}
	} else {
		fmt.Fprintf(streams.Out, "Downloading Xcode %s with xcodes...\n", missing.Version)
		var said bytes.Buffer
		path, err = images.DownloadXcode(ctx, missing, nil, &said, &said)
		if errors.Is(err, tart.ErrXcodes) {
			return fmt.Errorf("%w; xcodes said:\n%s\nIf it needs you to sign in, run it once at a terminal: xcodes download %s --directory %s",
				err, "  "+strings.Join(prose.LastLines(said.String(), 10), "\n  "), missing.Version, missing.Folder)
		}
		if err != nil {
			return err
		}
	}
	fmt.Fprintf(streams.Out, "Downloaded %s.\n", filepath.Base(path))
	return nil
}
