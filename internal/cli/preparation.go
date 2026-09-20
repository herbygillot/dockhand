package cli

import (
	"crypto/rand"
	"errors"
	"fmt"
	"github.com/herbygillot/dockhand/internal/macports/version"
	"github.com/herbygillot/dockhand/internal/progress"

	"github.com/herbygillot/dockhand/internal/app"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/publish"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/workflow"
	"github.com/herbygillot/dockhand/internal/workflow/view"
	"github.com/spf13/cobra"
)

func (r *runtime) changeCommands() []*cobra.Command {
	var commands []*cobra.Command
	const shared = "New preparations use freshly fetched master from macports/macports-ports; local commits and working-tree edits are excluded. The command continues the port's open contribution and its frozen source; a preparation that stopped before any branch has retired, and the next one starts from fresh master. By default the command stays in the foreground through verification and publication of a PR on macports/macports-ports from your fork. --no-publish (-P) stops after verification, --skip-verify (-V) publishes the prepared branch without building it and says so in the PR, both together stop at the prepared branch, and --detach returns once the work is accepted and admitted, leaving wait or start to finish it. Ctrl-C detaches without canceling accepted work. --diff previews the edit without touching the checkout or opening the state database."
	for _, spec := range []struct {
		action  record.Action
		short   string
		long    string
		example string
	}{
		{record.Bump, "Prepare a port version update",
			shared + " The version is found in a literal version, a github.setup, gitlab.setup, or go.setup argument, or a perl5.setup, R.setup, or ruby.setup argument, and is edited in the spelling the source uses. Version updates support GitHub/GitLab tags, explicit archive versions, ports fetched with git, scoped release subports, conditional archive checksums, archives on HTTP or anonymous FTP master sites, legacy checksum blocks, and supported Go/Cargo dependency declarations. A python, perl, or ruby stub bumps through its newest versioned subport as one shared release. Independent pinned releases are preserved. Omitting the version selects the newest eligible version using the port's source convention and livecheck filter, stable releases only unless the port already rides a prerelease; ports whose source selects published releases ignore tags without a release. Already-current ports complete without branch creation or verification. An explicit version may include its upstream tag prefix.",
			"  dockhand bump jq\n  dockhand bump jq 1.8.1 --diff\n  dockhand bump rust-analyzer 2026-09-14 --no-publish\n  dockhand bump jq --skip-verify\n  dockhand bump py-idna --detach"},
		{record.BumpRevision, "Prepare a port revision bump",
			shared + " The literal revision increments by one; revision expressions and ambiguous or dynamically named scopes are refused. --reason becomes the commit body and pull-request description. The version and checksums are preserved.",
			"  dockhand bump-revision jq --reason \"rebuild against oniguruma 6.9.10\"\n  dockhand bump-revision jq --diff --reason rebuild"},
		{record.RefreshChecksums, "Refresh a port's distfile checksums",
			shared + " The port's declared archives are downloaded from their direct HTTP or FTP master sites and every declared digest is recomputed from the real contents, including named and conditional checksums; a legacy block of md5 or sha1 digests is rewritten as rmd160, sha256, and size unless --keep-old-checksums keeps it as written. The version and revision are preserved; a changed archive is reported rather than silently accepted when the port pins its size.",
			"  dockhand refresh-checksums jq --diff\n  dockhand refresh-checksums jq --reason \"upstream re-rolled the tarball\""},
	} {
		options := &Options{}
		var build buildOptions
		var publication publish.Options
		var reason, change string
		var sharedRelease, keepOldChecksums bool
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
					return version.Validate(args[1])
				}
				return nil
			},
			RunE: func(cmd *cobra.Command, args []string) error {
				if build.dependents && (options.SkipVerify || options.Diff) {
					return fmt.Errorf("--dependents requires verification; omit --skip-verify or --diff")
				}
				choices, err := parseVariants(variants)
				if err != nil {
					return err
				}
				selector, err := portName(args[0])
				if err != nil {
					return err
				}
				var version string
				if spec.action == record.Bump && len(args) == 2 {
					version = args[1]
				}
				var destination *publish.Options
				if !options.NoPublish && !options.Diff {
					destination = &publication
				} else if (cmd.Flags().Changed("remote") || cmd.Flags().Changed("upstream") || cmd.Flags().Changed("base") || cmd.Flags().Changed("refresh-body")) && build.provider != "github" && build.provider != "auto" {
					return fmt.Errorf("publication destination flags need publication; drop --no-publish")
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
					progress.VerboseReport(cmd.Context(), "Binding contribution source; local commits and working-tree edits are excluded")
					bound, err := services.BindPreparation(cmd.Context(), app.Preparation{EditIntent: record.EditIntent{SharedRelease: sharedRelease, KeepOldChecksums: keepOldChecksums}, AllSubports: options.AllSubports, KeepFailed: build.keepFailed,
						ChangeID: record.ChangeID(change), IncludeDependents: build.dependents, Action: spec.action, Version: version, ID: record.RequestID("request_" + rand.Text()),
						Selection: macports.Selection{Selector: selector, Variants: choices},
						Reason:    reason, Publish: destination, SkipVerify: options.SkipVerify, Tests: record.TestPolicy(build.tests), FromSource: build.fromSource,
					})
					if err != nil {
						return publicationIntakeHint(err, destination != nil)
					}
					receipt, err := services.Workflow.Submit(cmd.Context(), bound.Request)
					if err != nil {
						return fmt.Errorf("accepting request %s: %w", bound.Request.ID, err)
					}
					progress.VerboseReport(cmd.Context(), "Accepted job %s; source commit %s", receipt.JobID, receipt.Source.Commit)
					milestone := workflow.Completion
					if options.Detach {
						milestone = workflow.Admission
					}
					return r.attach(cmd, services, receipt.JobID, milestone, options.Trace, false, &receipt)
				}
				request := app.PreviewRequest{EditIntent: record.EditIntent{SharedRelease: sharedRelease, KeepOldChecksums: keepOldChecksums}, Action: spec.action, Selection: macports.Selection{Selector: selector, Variants: choices}, Reason: reason}
				if len(args) == 2 {
					request.Version = args[1]
				}
				if !r.json {
					progress.VerboseReport(cmd.Context(), "Fetching MacPorts master for preview; local commits and working-tree edits are excluded")
				}
				preview, err := app.PreviewPreparation(cmd.Context(), r.config, request)
				if err != nil {
					return err
				}
				if r.json {
					return r.emit(preview)
				}
				progress.VerboseReport(cmd.Context(), "Repository %s; branch %s; commit %s; target %s", preview.Repository, preview.Branch, preview.Preparation.Base.Commit, preview.Preparation.Target.Name)
				if release := preview.Preparation.Release; release != nil {
					fmt.Fprintf(cmd.ErrOrStderr(), "%s: %s\n", plain(preview.Preparation.Target.Name), plain(view.VersionMove(release)))
					switch {
					case release.Archive && release.Listing != nil:
						progress.VerboseReport(cmd.Context(), "Archive version %s discovered at %s", release.Version, release.Listing.URL)
					case release.Archive:
						progress.VerboseReport(cmd.Context(), "Explicit archive version %s", release.Version)
					default:
						progress.VerboseReport(cmd.Context(), "Release %s (%s); upstream commit %s", release.Tag, release.Version, release.Commit)
					}
					if release.LeavesStable {
						fmt.Fprintf(cmd.ErrOrStderr(), "Warning: this takes %s out of stable; %s is a prerelease. The explicit version is honored.\n", plain(args[0]), plain(release.Version))
					}
				} else {
					what := "revision bump"
					if spec.action == record.RefreshChecksums {
						what = "checksum refresh"
					}
					fmt.Fprintf(cmd.ErrOrStderr(), "%s: %s\n", plain(preview.Preparation.Target.Name), what)
				}
				for _, patch := range preview.Preparation.Patches {
					if patch.Checked && patch.Applies {
						progress.VerboseReport(cmd.Context(), "Patch %s: %s", patch.Name, patch.Detail)
					} else {
						progress.Report(cmd.Context(), "Patch %s: %s", patch.Name, patch.Detail)
					}
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
		if spec.action == record.Bump || spec.action == record.RefreshChecksums {
			command.Flags().BoolVar(&keepOldChecksums, "keep-old-checksums", false, "Keep a legacy checksum block's algorithms and layout, refreshing md5/sha1 values in place instead of rewriting the block as rmd160, sha256, and size")
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

// publicationIntakeHint names the way out when the default publication
// cannot be bound before any work starts: no GitHub login, no fork, or an
// ambiguous remote layout.
func publicationIntakeHint(err error, publishing bool) error {
	if publishing && errors.Is(err, workflow.ErrPublicationIntake) {
		return fmt.Errorf("%w; pass --no-publish to stop after verification", err)
	}
	return err
}
