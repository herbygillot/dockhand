package command

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/herbygillot/dockhand/internal/buildenv"
	"github.com/herbygillot/dockhand/internal/buildenv/tart"
	"github.com/herbygillot/dockhand/internal/config"
	"github.com/herbygillot/dockhand/internal/github"
)

// tartImages is what the providers commands ask of dockhand's Tart images.
type tartImages interface {
	Status(ctx context.Context) (tart.Status, error)
	Setup(ctx context.Context, options tart.SetupOptions, progress io.Writer) (tart.SetupResult, error)
	Xcodes() (string, bool)
	DownloadXcode(ctx context.Context, missing *tart.MissingXcode, in io.Reader, out, errs io.Writer) (string, error)
}

// testTartImages, when set, stands in for dockhand's Tart images.
var testTartImages tartImages

// images is dockhand's Tart images, or nil when Tart isn't installed.
func images() tartImages {
	if testTartImages != nil {
		return testTartImages
	}
	if _, err := lookTart("tart"); err != nil {
		return nil
	}
	return &tart.Provider{}
}

// installTart installs Tart from MacPorts.
const installTart = "sudo port install tart"

func providersCommand(s *settings, streams Streams) *cobra.Command {
	cmd := &cobra.Command{
		Use:    "providers",
		Hidden: true,
		Short:  "Show where checks can build, and set up a place to build",
		Long: `Shows each provider a check can build with, and whether it's ready: tart,
in dockhand's own macOS VMs; github, in your fork's GitHub Actions; and
command, your own script, when the configuration file names one.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, file, _, err := s.options(cmd.Context())
			if err != nil {
				return err
			}
			for _, line := range providerLines(cmd.Context(), file) {
				fmt.Fprintln(streams.Out, line)
			}
			return nil
		},
	}
	setup := &cobra.Command{
		Use:   "setup",
		Short: "Set up a provider",
		Args:  cobra.NoArgs,
	}
	setup.AddCommand(setupTartCommand(s, streams))
	cmd.AddCommand(setup)
	return cmd
}

func setupTartCommand(s *settings, streams Streams) *cobra.Command {
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
them, makes dockhand-xcode-<release> instead, the same with Xcode too, in
up to ` + tart.XcodeDisk + ` more. Its Xcode is the one MacPorts' arm64
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
			if !tartSupported() {
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
			images := images()
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
			// Ports are evaluated with this Mac's MacPorts and built with
			// the image's, which can differ on what a Portfile means.
			if host := tart.HostMacPorts(cmd.Context(), portTclsh()); host != "" && host != result.MacPorts {
				fmt.Fprintf(streams.Out, "This Mac has MacPorts %s. Checks read ports with it and build them with the image's %s; --rebuild --macports-version %s makes them the same.\n",
					host, result.MacPorts, host)
			}
			// Not a Next: line, which names only what the branch as it
			// stands would accept, and setup has no branch.
			fmt.Fprintf(streams.Out, "Checks build on it with --on tart:%s.\n", result.Release.Slug)
			return nil
		},
	}
	cmd.Flags().BoolVar(&options.Check, "check", false, "check the image in a disposable clone, making nothing")
	cmd.Flags().BoolVar(&options.Rebuild, "rebuild", false, "make a replacement even when the image exists")
	cmd.Flags().StringVar(&options.MacPortsVersion, "macports-version", "", "the MacPorts the image installs (default "+tart.DefaultMacPorts+")")
	cmd.Flags().StringVar(&options.Xcode, "xcode", "", "make the release's Xcode image, from an Xcode .xip or a folder of them")
	return cmd
}

// providerLines say whether each provider is ready, one line each, as
// providers and init show them.
func providerLines(ctx context.Context, file config.File) []string {
	lines := []string{fmt.Sprintf("%-8s %s", buildenv.Tart, tartReadiness(ctx))}
	lines = append(lines, fmt.Sprintf("%-8s %s", buildenv.GitHub, githubReadiness(ctx)))
	if command := file.Providers.Command; command != nil {
		name := command.Name
		if name == "" {
			name = "your script"
		}
		lines = append(lines, fmt.Sprintf("%-8s ✓ %s: %s", buildenv.Command, name, command.Run))
	}
	return lines
}

// tartSupported is tart.Supported, which a test sets to see an Intel Mac.
var tartSupported = tart.Supported

// noTart says why Tart isn't for this Mac, and where its checks build.
const noTart = "· needs Apple silicon; checks here build on GitHub, with --on github"

func tartReadiness(ctx context.Context) string {
	if !tartSupported() {
		return noTart
	}
	images := images()
	if images == nil {
		return "· needs Tart: " + installTart + ", then dockhand setup tart"
	}
	status, err := images.Status(ctx)
	if err != nil {
		return "! " + err.Error()
	}
	var host string
	if status.Host.Product != "" {
		host = fmt.Sprintf("macOS %s", status.Host.Product)
	}
	if len(status.Base) == 0 {
		cost := "up to " + tart.SetupDisk
		if host != "" {
			cost = host + ", " + cost
		}
		// An Xcode image made without the plain one is said, not taken for
		// nothing set up (the rc1 full stage's A2).
		if len(status.Xcode) > 0 {
			var xcode []string
			for _, release := range status.Xcode {
				xcode = append(xcode, release.Product)
			}
			return fmt.Sprintf("· Xcode image for macOS %s ready; no plain image, which most ports build in: dockhand setup tart   (%s)", strings.Join(xcode, ", "), cost)
		}
		return fmt.Sprintf("· not set up: dockhand setup tart   (%s)", cost)
	}
	var releases []string
	hasHost := false
	for _, release := range status.Base {
		releases = append(releases, release.Product)
		hasHost = hasHost || release.Darwin == status.Host.Darwin
	}
	line := "✓ images for macOS " + strings.Join(releases, ", ")
	if len(status.Xcode) > 0 {
		var xcode []string
		for _, release := range status.Xcode {
			xcode = append(xcode, release.Product)
		}
		line += ", with Xcode for " + strings.Join(xcode, ", ")
	}
	if host != "" && !hasHost {
		line += "; none for this Mac's " + host + ": dockhand setup tart"
	}
	return line
}

func githubReadiness(ctx context.Context) string {
	if overridingToken() != "" {
		return "✓ with your GitHub token; your fork's Actions must be enabled"
	}
	if _, err := authStore.Get(ctx, github.CredentialKey); err == nil {
		return "✓ with your GitHub login; your fork's Actions must be enabled"
	}
	return "· needs a GitHub login, dockhand setup github, and your fork's Actions enabled"
}

// missingSteps are the setup commands still to run, in order, for setup's
// Next: line, which jumped to update with Tart and the login undone (the
// rc1 full stage's A2): a Tart image where Tart can build, and a login
// where no token stands in for one.
func missingSteps(ctx context.Context) []string {
	var steps []string
	if tartSupported() {
		if images := images(); images != nil {
			if status, err := images.Status(ctx); err == nil && len(status.Base) == 0 {
				steps = append(steps, "dockhand setup tart")
			}
		}
	}
	if overridingToken() == "" {
		if _, err := authStore.Get(ctx, github.CredentialKey); err != nil {
			steps = append(steps, "dockhand setup github")
		}
	}
	return steps
}

// offerXcode downloads the Xcode setup is missing with xcodes. At a
// terminal it asks first, and xcodes asks there for the person's Apple ID
// when it needs it; dockhand never sees it. Without one it downloads, as
// setup downloads a vanilla image, with the sign-in xcodes keeps, and
// shows what xcodes said when it fails.
func offerXcode(ctx context.Context, images tartImages, streams Streams, missing *tart.MissingXcode) error {
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
	if streams.terminal() {
		download, err := confirm(streams, fmt.Sprintf("Download Xcode %s with xcodes, signing in with your Apple ID? [y/N] ", missing.Version))
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
				err, lastLines(said.String(), 10), missing.Version, missing.Folder)
		}
		if err != nil {
			return err
		}
	}
	fmt.Fprintf(streams.Out, "Downloaded %s.\n", filepath.Base(path))
	return nil
}

// lastLines is the end of a program's output, its progress redrawn with
// carriage returns counted as lines.
func lastLines(output string, n int) string {
	var lines []string
	for line := range strings.SplitSeq(strings.ReplaceAll(output, "\r", "\n"), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			lines = append(lines, "  "+line)
		}
	}
	return strings.Join(lines[max(0, len(lines)-n):], "\n")
}
