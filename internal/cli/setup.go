package cli

import (
	"fmt"
	"github.com/herbygillot/dockhand/internal/macos"
	"github.com/herbygillot/dockhand/internal/macports"

	"github.com/herbygillot/dockhand/internal/app"
	"github.com/spf13/cobra"
)

func (r *runtime) setupCommand() *cobra.Command {
	options := app.SetupOptions{MacPortsVersion: macports.DefaultBaseVersion}
	command := &cobra.Command{
		Use:         "setup",
		Short:       "Prepare a local Tart verification image",
		Long:        "Check the native MacPorts platform and prepare a clean local Tart image with the guest agent, command line tools, and MacPorts. Pass --os to choose another macOS release. Pass --xcode to prepare a separate full-Xcode profile from one .xip archive or a directory of release archives. An existing image is validated in a disposable clone. Missing images are provisioned from the matching vanilla macOS image.",
		Args:        cobra.NoArgs,
		Annotations: map[string]string{stateIndependentHelp: "true"},
		RunE: func(cmd *cobra.Command, _ []string) error {
			result, err := app.Setup(cmd.Context(), r.config, options, cmd.ErrOrStderr())
			if err != nil {
				return err
			}
			if r.json {
				return r.emit(result)
			}
			review := "no source-review record"
			if result.HostMacPorts.SourceReviewed {
				review = "source-reviewed"
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Host MacPorts Base %s (Tcl %s): evaluator startup checks passed; %s.\n", plain(result.HostMacPorts.BaseVersion), plain(result.HostMacPorts.TclVersion), review)
			for _, tool := range result.OptionalTools {
				status := "not found (needed only for matching dependency blocks)"
				if tool.Available {
					status = plain(tool.Path)
				}
				if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Optional %s: %s\n", tool.Name, status); err != nil {
					return err
				}
			}
			verb := "Provisioned"
			if result.Reused {
				verb = "Ready"
			}
			xcode := ""
			if result.XcodeVersion != "" {
				xcode = ", Xcode " + plain(result.XcodeVersion)
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "%s verification image %s (%s, MacPorts %s, guest agent %s%s).\n", verb, plain(result.Image), plain(macos.Describe(result.Platform)), plain(result.MacPortsVersion), plain(result.GuestAgentVersion), xcode)
			return err
		},
	}
	command.Flags().StringVar(&options.OS, "os", "", "macOS release name or major version (defaults to the host release)")
	command.Flags().BoolVar(&options.Check, "check", false, "Validate an existing image without provisioning one")
	command.Flags().BoolVar(&options.Rebuild, "rebuild", false, "Provision and validate a replacement even when the image exists")
	command.Flags().StringVar(&options.Image, "image", r.config.Tart.Image, "Local Tart image name (defaults from the native macOS release)")
	command.Flags().StringVar(&options.Source, "source", "", "Source Tart OCI image (defaults to the matching vanilla macOS image)")
	command.Flags().StringVar(&options.MacPortsVersion, "macports-version", macports.DefaultBaseVersion, "MacPorts version to install and require")
	command.Flags().StringVar(&options.Xcode, "xcode", "", "Xcode .xip archive or directory of compatible release archives")
	_ = command.MarkFlagFilename("xcode", "xip")
	command.MarkFlagsMutuallyExclusive("check", "rebuild")
	return command
}
