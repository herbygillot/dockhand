package cli

import (
	"github.com/herbygillot/dockhand/v2/internal/app"
	"github.com/spf13/cobra"
)

func (r *runtime) workflowCommand(use, short string, args cobra.PositionalArgs) (*cobra.Command, *Options) {
	options := &Options{}
	command := &cobra.Command{
		Use:   use,
		Short: short,
		Long:  short + ".\n\nThis command's workflow is not implemented yet.",
		Args:  args,
		RunE: func(cmd *cobra.Command, args []string) error {
			if options.Diff {
				return execute(cmd.Context(), cmd.CommandPath(), args, *options, Streams{In: cmd.InOrStdin(), Out: cmd.OutOrStdout(), Err: cmd.ErrOrStderr()}, nil)
			}
			services, err := app.Build(cmd.Context(), r.config)
			if err != nil {
				return err
			}
			defer services.Close()
			effective := *options
			effective.JSON = r.json
			effective.Wait = effective.Wait || effective.Trace
			streams := Streams{In: cmd.InOrStdin(), Out: cmd.OutOrStdout(), Err: cmd.ErrOrStderr()}
			return execute(cmd.Context(), cmd.CommandPath(), args, effective, streams, services)
		},
	}
	return command, options
}

func (r *runtime) setupCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "setup",
		Short: "Check tools and prepare verification environments",
		Long:  "Check tools and prepare verification environments.\n\nSetup is not implemented yet.",
		Args:  cobra.NoArgs,
		RunE:  func(cmd *cobra.Command, _ []string) error { return app.Setup(cmd.Context(), r.config) },
	}
}

func (r *runtime) changeCommands() []*cobra.Command {
	var commands []*cobra.Command
	for _, spec := range []struct{ name, short string }{
		{"bump", "Prepare a port version update"},
		{"bump-revision", "Prepare a port revision bump"},
		{"refresh-checksums", "Refresh a port's distfile checksums"},
	} {
		command, options := r.workflowCommand(spec.name+" <selector>", spec.short, cobra.ExactArgs(1))
		changeFlags(command, options)
		commands = append(commands, command)
	}
	return commands
}

func (r *runtime) publishCommand() *cobra.Command {
	command, options := r.workflowCommand("publish <target>", "Publish an existing change", cobra.ExactArgs(1))
	command.Flags().BoolVar(&options.Wait, "wait", false, "Stay until the requested work completes")
	return command
}

func (r *runtime) reviewCommand() *cobra.Command {
	review := &cobra.Command{
		Use:   "review",
		Short: "Record a review decision",
		Args:  cobra.NoArgs,
		RunE:  func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
	}
	accept, _ := r.workflowCommand("accept <change>", "Accept a proposed change", cobra.ExactArgs(1))
	dismiss, _ := r.workflowCommand("dismiss <change>", "Dismiss a proposed change", cobra.ExactArgs(1))
	review.AddCommand(accept, dismiss)
	return review
}
