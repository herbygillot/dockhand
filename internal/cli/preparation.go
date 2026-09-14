package cli

import (
	"crypto/rand"
	"encoding/json"
	"fmt"

	"github.com/herbygillot/dockhand/internal/app"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/prepare"
	"github.com/herbygillot/dockhand/internal/publish"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/upstream"
	"github.com/herbygillot/dockhand/internal/workflow"
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
		var build buildOptions
		var publication publish.Options
		var subport, reason string
		var variants []string
		use, maximum := string(spec.action)+" <port>", 1
		if spec.action == record.Bump {
			use += " [version]"
			maximum = 2
		}
		command := &cobra.Command{
			Use: use, Short: spec.short,
			Long: spec.short + ".\n\nVersion and revision bumps use freshly fetched master from macports/macports-ports, then create a new local contribution branch. --diff previews the edit; --no-verify stops at branch creation. --publish continues to a confirmed PR after passing verification; --wait or --trace stays through that destination. Publication requires verification. Version updates are supported for a bounded set of GitHub PortGroup sources with one distfile and literal checksums. Omitting the version selects the newest eligible stable numeric version using the port's GitHub tags livecheck filter. Already-current ports complete without branch creation or verification. An explicit version may include its upstream tag prefix. Standalone checksum refresh is not implemented yet.",
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
				choices, err := parseVariants(variants)
				if err != nil {
					return err
				}
				if spec.action == record.RefreshChecksums {
					return fmt.Errorf("%w: %w: checksum refresh", ErrNotImplemented, prepare.ErrNotImplemented)
				}
				var version string
				if spec.action == record.Bump && len(args) == 2 {
					version = args[1]
				}
				var destination *publish.Options
				if options.Publish {
					destination = &publication
				} else if cmd.Flags().Changed("remote") || cmd.Flags().Changed("upstream") || cmd.Flags().Changed("base") {
					return fmt.Errorf("publication destination flags require --publish")
				}
				if !options.Diff {
					config, err := build.config(cmd, r.config)
					if err != nil {
						return err
					}
					services, err := app.Build(cmd.Context(), config)
					if err != nil {
						return err
					}
					defer services.Close()
					fmt.Fprintln(cmd.ErrOrStderr(), "Fetching MacPorts master; local commits and working-tree edits are excluded.")
					bound, err := services.BindPreparation(cmd.Context(), app.Preparation{
						Action: spec.action, Version: version, ID: record.RequestID("request_" + rand.Text()),
						Selection: macports.Selection{Selector: args[0], Subport: subport, Variants: choices},
						Reason:    reason, Publish: destination, NoVerify: options.NoVerify, Tests: record.TestPolicy(build.tests), FromSource: build.fromSource,
					})
					if err != nil {
						return err
					}
					receipt, err := services.Workflow.Submit(cmd.Context(), bound.Request)
					if err != nil {
						return fmt.Errorf("accepting request %s: %w", bound.Request.ID, err)
					}
					fmt.Fprintf(cmd.ErrOrStderr(), "Accepted job %s; source commit %s.\n", receipt.JobID, bound.Request.Spec.Source.Commit)
					milestone := workflow.Admission
					if options.Wait || options.Trace {
						milestone = workflow.Completion
					}
					return r.attach(cmd, services, receipt.JobID, milestone, options.Trace, false, &receipt)
				}
				request := app.PreviewRequest{Action: spec.action, Selection: macports.Selection{Selector: args[0], Subport: subport, Variants: choices}, Reason: reason}
				if len(args) == 2 {
					request.Version = args[1]
				}
				if !r.json {
					fmt.Fprintln(cmd.ErrOrStderr(), "Fetching MacPorts master for preview; local commits and working-tree edits are excluded.")
				}
				preview, err := app.PreviewPreparation(cmd.Context(), r.config, request)
				if err != nil {
					return err
				}
				if r.json {
					return json.NewEncoder(cmd.OutOrStdout()).Encode(preview)
				}
				fmt.Fprintf(cmd.ErrOrStderr(), "Repository: %s\nBranch: %s\nCommit: %s\nTarget: %s\n", preview.Repository, preview.Branch, preview.Preparation.Base.Commit, preview.Preparation.Target.Name)
				if release := preview.Preparation.Release; release != nil {
					if release.NoUpdate {
						fmt.Fprintf(cmd.ErrOrStderr(), "Already current at %s; latest eligible version is %s.\n", release.CurrentVersion, release.Version)
					} else {
						fmt.Fprintf(cmd.ErrOrStderr(), "Release: %s (%s); upstream commit: %s\n", release.Tag, release.Version, release.Commit)
					}
				}
				_, err = fmt.Fprint(cmd.OutOrStdout(), preview.Diff)
				return err
			},
		}
		changeFlags(command, options)
		command.Flags().StringVar(&subport, "subport", "", "Select a subport within the Portfile")
		command.Flags().StringArrayVar(&variants, "variant", nil, "Select a variant, e.g. +debug or --variant=-debug")
		if spec.action == record.BumpRevision || spec.action == record.Bump {
			build.flags(command, r.config)
			publicationFlags(command, &publication)
			command.Flags().StringVar(&reason, "reason", "", "Reason for the change")
		}
		commands = append(commands, command)
	}
	return commands
}
