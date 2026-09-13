package cli

import (
	"encoding/json"
	"fmt"

	"github.com/herbygillot/dockhand/v2/internal/app"
	"github.com/herbygillot/dockhand/v2/internal/git"
	"github.com/herbygillot/dockhand/v2/internal/macports"
	"github.com/herbygillot/dockhand/v2/internal/prepare"
	"github.com/herbygillot/dockhand/v2/internal/record"
	"github.com/herbygillot/dockhand/v2/internal/upstream"
	"github.com/spf13/cobra"
)

func (r *runtime) changeCommands() []*cobra.Command {
	var commands []*cobra.Command
	for _, spec := range []struct {
		action record.Action
		short  string
	}{
		{record.Bump, "Prepare a port version update"},
		{record.BumpRevision, "Prepare a port revision bump"},
		{record.RefreshChecksums, "Refresh a port's distfile checksums"},
	} {
		options := &Options{}
		var branch, subport, reason string
		var variants []string
		use, maximum := string(spec.action)+" <port>", 1
		if spec.action == record.Bump {
			use += " [version]"
			maximum = 2
		}
		command := &cobra.Command{
			Use: use, Short: spec.short,
			Long: spec.short + ".\n\nRevision-bump previews (--diff) use committed source from the current branch or --branch. Branch creation, version preparation, and checksum refresh are not implemented yet. An optional bump version may include its upstream tag prefix.",
			Args: func(cmd *cobra.Command, args []string) error {
				if err := cobra.RangeArgs(1, maximum)(cmd, args); err != nil {
					return err
				}
				if args[0] == "" {
					return fmt.Errorf("port must not be empty")
				}
				if len(args) == 2 {
					return upstream.ValidateVersion(args[1])
				}
				return nil
			},
			RunE: func(cmd *cobra.Command, args []string) error {
				if cmd.Flags().Changed("branch") && !git.ValidBranchName(branch) {
					return fmt.Errorf("branch must name a literal local branch")
				}
				choices, err := parseVariants(variants)
				if err != nil {
					return err
				}
				if !options.Diff {
					return fmt.Errorf("%w: preparation job execution is not connected; bump-revision --diff can preview a revision edit", ErrNotImplemented)
				}
				if spec.action != record.BumpRevision {
					return fmt.Errorf("%w: %s still needs release/download/checksum preparation", prepare.ErrNotImplemented, spec.action)
				}
				request := app.PreviewRequest{Action: spec.action, Branch: branch, Selection: macports.Selection{Selector: args[0], Subport: subport, Variants: choices}, Reason: reason}
				if len(args) == 2 {
					request.Version = args[1]
				}
				if !r.json {
					fmt.Fprintln(cmd.ErrOrStderr(), "Preparing preview from committed source; working-tree edits are excluded.")
				}
				preview, err := app.PreviewPreparation(cmd.Context(), r.config, request)
				if err != nil {
					return err
				}
				if r.json {
					return json.NewEncoder(cmd.OutOrStdout()).Encode(preview)
				}
				fmt.Fprintf(cmd.ErrOrStderr(), "Branch: %s\nCommit: %s\nTarget: %s\n", preview.Branch, preview.Preparation.Base.Commit, preview.Preparation.Target.Name)
				_, err = fmt.Fprint(cmd.OutOrStdout(), preview.Diff)
				return err
			},
		}
		changeFlags(command, options)
		command.Flags().StringVar(&branch, "branch", "", "Select committed source from a literal local branch")
		command.Flags().StringVar(&subport, "subport", "", "Select a subport within the Portfile")
		command.Flags().StringArrayVar(&variants, "variant", nil, "Select a variant, e.g. +debug or --variant=-debug")
		if spec.action == record.BumpRevision {
			command.Flags().StringVar(&reason, "reason", "", "Reason for the revision bump")
		}
		commands = append(commands, command)
	}
	return commands
}
