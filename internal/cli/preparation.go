package cli

import (
	"crypto/rand"
	"encoding/json"
	"fmt"

	"github.com/herbygillot/dockhand/internal/app"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/publish"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/upstream"
	"github.com/herbygillot/dockhand/internal/workflow"
	"github.com/spf13/cobra"
)

func (r *runtime) changeCommands() []*cobra.Command {
	var commands []*cobra.Command
	const shared = "New preparations use freshly fetched master from macports/macports-ports; local commits and working-tree edits are excluded. Retries continue their recorded contribution and frozen source. --diff previews the edit without touching the checkout or opening the state database; --no-verify stops at branch creation. --publish continues to a confirmed PR after passing verification; --wait or --trace stays through that destination. Publication requires verification."
	for _, spec := range []struct {
		action  record.Action
		short   string
		long    string
		example string
	}{
		{record.Bump, "Prepare a port version update",
			shared + " Version updates support GitHub/GitLab tags and explicit archive versions, scoped release subports, conditional archive checksums, and supported Go/Cargo dependency declarations. Independent pinned releases are preserved. Omitting the version selects the newest eligible stable numeric version using the port's source convention and livecheck filter; ports whose source selects published releases ignore tags without a release. Already-current ports complete without branch creation or verification. An explicit version may include its upstream tag prefix.",
			"  dockhand bump jq\n  dockhand bump jq 1.8.1 --diff\n  dockhand bump rust-analyzer 2026-09-14 --publish --wait"},
		{record.BumpRevision, "Prepare a port revision bump",
			shared + " The literal revision increments by one; revision expressions and ambiguous or dynamically named scopes are refused. --reason becomes the commit body and pull-request description. The version and checksums are preserved.",
			"  dockhand bump-revision jq --reason \"rebuild against oniguruma 6.9.10\"\n  dockhand bump-revision jq --diff --reason rebuild"},
		{record.RefreshChecksums, "Refresh a port's distfile checksums",
			shared + " The port's declared archives are downloaded from their direct master sites and every declared digest is recomputed from the real contents, including named and conditional checksums. The version and revision are preserved; a changed archive is reported rather than silently accepted when the port pins its size.",
			"  dockhand refresh-checksums jq --diff\n  dockhand refresh-checksums jq --reason \"upstream re-rolled the tarball\""},
	} {
		options := &Options{}
		var build buildOptions
		var publication publish.Options
		var reason, change string
		var sharedRelease bool
		var variants []string
		use, maximum := string(spec.action)+" <port>", 1
		if spec.action == record.Bump {
			use += " [version]"
			maximum = 2
		}
		command := &cobra.Command{
			Use: use, Short: spec.short,
			Long: spec.short + ".\n\n" + spec.long, Example: spec.example,
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
				if build.dependents && (options.NoVerify || options.Diff) {
					return fmt.Errorf("--dependents requires verification; omit --no-verify or --diff")
				}
				choices, err := parseVariants(variants)
				if err != nil {
					return err
				}
				var version string
				if spec.action == record.Bump && len(args) == 2 {
					version = args[1]
				}
				var destination *publish.Options
				if options.Publish {
					destination = &publication
				} else if (cmd.Flags().Changed("remote") || cmd.Flags().Changed("upstream") || cmd.Flags().Changed("base")) && build.provider != "github" && build.provider != "auto" {
					return fmt.Errorf("publication destination flags require --publish")
				}
				if !options.Diff {
					config, err := build.config(cmd, r.config)
					if err != nil {
						return err
					}
					services, err := r.build(cmd.Context(), config)
					if err != nil {
						return err
					}
					defer services.Close()
					fmt.Fprintln(cmd.ErrOrStderr(), "Binding contribution source; local commits and working-tree edits are excluded.")
					bound, err := services.BindPreparation(cmd.Context(), app.Preparation{SharedRelease: sharedRelease, KeepFailed: build.keepFailed,
						ChangeID: record.ChangeID(change), IncludeDependents: build.dependents, Action: spec.action, Version: version, ID: record.RequestID("request_" + rand.Text()),
						Selection: macports.Selection{Selector: args[0], Variants: choices},
						Reason:    reason, Publish: destination, NoVerify: options.NoVerify, Tests: record.TestPolicy(build.tests), FromSource: build.fromSource,
					})
					if err != nil {
						return err
					}
					receipt, err := services.Workflow.Submit(cmd.Context(), bound.Request)
					if err != nil {
						return fmt.Errorf("accepting request %s: %w", bound.Request.ID, err)
					}
					fmt.Fprintf(cmd.ErrOrStderr(), "Accepted job %s; source commit %s.\n", receipt.JobID, receipt.Source.Commit)
					milestone := workflow.Admission
					if options.Wait || options.Trace {
						milestone = workflow.Completion
					}
					return r.attach(cmd, services, receipt.JobID, milestone, options.Trace, false, &receipt)
				}
				request := app.PreviewRequest{SharedRelease: sharedRelease, Action: spec.action, Selection: macports.Selection{Selector: args[0], Variants: choices}, Reason: reason}
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
					} else if release.Archive {
						if release.Listing != nil {
							fmt.Fprintf(cmd.ErrOrStderr(), "Release: archive version %s; discovered at %s\n", release.Version, release.Listing.URL)
						} else {
							fmt.Fprintf(cmd.ErrOrStderr(), "Release: explicit archive version %s\n", release.Version)
						}
					} else {
						fmt.Fprintf(cmd.ErrOrStderr(), "Release: %s (%s); upstream commit: %s\n", release.Tag, release.Version, release.Commit)
					}
					if release.LeavesStable {
						fmt.Fprintf(cmd.ErrOrStderr(), "Warning: this takes %s out of stable; %s is a prerelease. The explicit version is honored.\n", args[0], release.Version)
					}
				}
				for _, patch := range preview.Preparation.Patches {
					fmt.Fprintf(cmd.ErrOrStderr(), "Patch %s: %s\n", plain(patch.Name), plain(patch.Detail))
				}
				if scope := preview.Preparation.Scope; scope != nil {
					for _, member := range scope.Affected {
						fmt.Fprintf(cmd.ErrOrStderr(), "Affected: %s %s -> %s (metadata only: %t)\n", plain(member.Target.Name), plain(member.Before.Version), plain(member.After.Version), member.MetadataOnly)
					}
				}
				_, err = fmt.Fprint(cmd.OutOrStdout(), preview.Diff)
				return err
			},
		}
		changeFlags(command, options)
		if spec.action == record.Bump {
			command.Flags().BoolVar(&sharedRelease, "shared-release", false, "Authorize updating all subports that share this release source")
		}
		command.Flags().StringVar(&change, "change", "", "Continue one contribution when the target is ambiguous")
		command.MarkFlagsMutuallyExclusive("change", "diff")
		command.Flags().StringArrayVar(&variants, "variant", nil, "Select a variant, e.g. +debug or --variant=-debug")
		if spec.action == record.BumpRevision || spec.action == record.Bump || spec.action == record.RefreshChecksums {
			build.flags(command, r.config)
			publicationFlags(command, &publication)
			command.Flags().StringVar(&reason, "reason", "", "Reason for the change")
		}
		commands = append(commands, command)
	}
	return commands
}
