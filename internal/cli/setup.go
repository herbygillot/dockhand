package cli

import (
	"encoding/json"
	"fmt"

	"github.com/herbygillot/dockhand/v2/internal/app"
	"github.com/herbygillot/dockhand/v2/internal/tart/provision"
	"github.com/spf13/cobra"
)

func (r *runtime) setupCommand() *cobra.Command {
	options := app.SetupOptions{MacPortsVersion: provision.DefaultMacPortsVersion}
	command := &cobra.Command{
		Use:         "setup",
		Short:       "Prepare a local Tart verification image",
		Long:        "Check the native MacPorts platform and prepare a clean local Tart image with the guest agent, command line tools, and MacPorts. Pass --xcode to prepare a separate full-Xcode profile from one .xip archive or a directory of release archives. An existing image is validated in a disposable clone. Missing images are provisioned from the matching vanilla macOS image.",
		Args:        cobra.NoArgs,
		Annotations: map[string]string{stateIndependentHelp: "true"},
		RunE: func(cmd *cobra.Command, _ []string) error {
			result, err := app.Setup(cmd.Context(), r.config, options, cmd.ErrOrStderr())
			if err != nil {
				return err
			}
			if r.json {
				return json.NewEncoder(cmd.OutOrStdout()).Encode(result)
			}
			verb := "Provisioned"
			if result.Reused {
				verb = "Ready"
			}
			xcode := ""
			if result.XcodeVersion != "" {
				xcode = ", Xcode " + plain(result.XcodeVersion)
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "%s verification image %s (%s %s %s, MacPorts %s, guest agent %s%s).\n", verb, plain(result.Image), plain(result.Platform.OS), plain(result.Platform.Version), plain(result.Platform.Architecture), plain(result.MacPortsVersion), plain(result.GuestAgentVersion), xcode)
			return err
		},
	}
	command.Flags().BoolVar(&options.Check, "check", false, "Validate an existing image without provisioning one")
	command.Flags().BoolVar(&options.Rebuild, "rebuild", false, "Provision and validate a replacement even when the image exists")
	command.Flags().StringVar(&options.Image, "image", r.config.Tart.Image, "Local Tart image name (defaults from the native macOS release)")
	command.Flags().StringVar(&options.Source, "source", "", "Source Tart OCI image (defaults to the matching vanilla macOS image)")
	command.Flags().StringVar(&options.MacPortsVersion, "macports-version", provision.DefaultMacPortsVersion, "MacPorts version to install and require")
	command.Flags().StringVar(&options.Xcode, "xcode", "", "Xcode .xip archive or directory of compatible release archives")
	_ = command.MarkFlagFilename("xcode", "xip")
	command.MarkFlagsMutuallyExclusive("check", "rebuild")
	return command
}
