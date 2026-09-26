package command

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/herbygillot/dockhand/internal/config"
	"github.com/herbygillot/dockhand/internal/github"
	"github.com/herbygillot/dockhand/internal/provider/tart"
)

// tartImages is what the providers commands ask of dockhand's Tart images.
type tartImages interface {
	Status(ctx context.Context) (tart.Status, error)
	Setup(ctx context.Context, options tart.SetupOptions, progress io.Writer) (tart.SetupResult, error)
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
		Use:   "providers",
		Short: "Show where checks can build, and set up a place to build",
		Long: `Shows each provider a check can build with, and whether it's ready: tart,
in dockhand's own macOS VMs; github, in your fork's GitHub Actions; and
command, your own script, when the configuration file names one.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, file, _, err := s.options()
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
	setup.AddCommand(setupTartCommand(streams))
	cmd.AddCommand(setup)
	return cmd
}

func setupTartCommand(streams Streams) *cobra.Command {
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
them, makes dockhand-xcode-<release> instead, the same with the newest Xcode
the release runs, in up to ` + tart.XcodeDisk + ` more. A check builds a port that needs
Xcode only there; without it, the port isn't built, and the check says so.

When the image exists, setup checks it in a disposable clone and leaves it
as it is. --rebuild makes a replacement, and keeps the old one until the
new one has passed.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 1 {
				options.Release = args[0]
			}
			images := images()
			if images == nil {
				return fmt.Errorf("checks build in Tart VMs, and Tart isn't installed: %s", installTart)
			}
			result, err := images.Setup(cmd.Context(), options, streams.Out)
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
			fmt.Fprintf(streams.Out, "Next: dockhand check --on tart:%s\n", result.Release.Slug)
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
	lines := []string{fmt.Sprintf("%-8s %s", "tart", tartReadiness(ctx))}
	lines = append(lines, fmt.Sprintf("%-8s %s", "github", githubReadiness(ctx)))
	if command := file.Providers.Command; command != nil {
		name := command.Name
		if name == "" {
			name = "your script"
		}
		lines = append(lines, fmt.Sprintf("%-8s ✓ %s: %s", "command", name, command.Run))
	}
	return lines
}

func tartReadiness(ctx context.Context) string {
	images := images()
	if images == nil {
		return "· needs Tart: " + installTart + ", then dockhand providers setup tart"
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
		return fmt.Sprintf("· not set up: dockhand providers setup tart   (%s)", cost)
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
		line += "; none for this Mac's " + host + ": dockhand providers setup tart"
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
	return "· needs a GitHub login and your fork's Actions enabled"
}
