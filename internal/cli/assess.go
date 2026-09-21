package cli

import (
	"errors"
	"fmt"
	"github.com/herbygillot/dockhand/internal/progress"

	"github.com/herbygillot/dockhand/internal/app"
	"github.com/herbygillot/dockhand/internal/assess"
	"github.com/herbygillot/dockhand/internal/macports/portedit"
	"github.com/spf13/cobra"
)

func (r *runtime) assessCommand() *cobra.Command {
	var request assess.Request
	var journalPath string
	cmd := &cobra.Command{
		Use: "assess [port...]", Short: "Assess whether Dockhand can prepare a port update",
		Long:        "Assess committed ports from local HEAD; working-tree edits are excluded. Select explicit ports, exact maintainer/category filters, or --all. The default checks local declarations and probes version inputs without querying upstream. With --at, resolve a specific upstream tag or explicit archive version and check the proposed edit to it. No source archives are downloaded, helpers executed, builds run, branches changed, or jobs created. Native Portfile evaluation still executes Tcl. A ready or candidate-ready result is preparation evidence, not a build guarantee. Blocked, unsupported, or unknown results exit with status 1 after reporting all selected ports.",
		Example:     "  dockhand assess terraform-1.16\n  dockhand assess rust-analyzer --at 2026-09-14\n  dockhand assess --maintainer herbygillot@github\n  dockhand assess --all --journal survey.jsonl",
		Annotations: map[string]string{stateIndependentHelp: "true"},
		Args: func(cmd *cobra.Command, args []string) error {
			ports, err := portNames(args)
			if err != nil {
				return err
			}
			request.Selection.Ports = ports
			if cmd.Flags().Changed("at") && request.Version == "" {
				return fmt.Errorf("assess: --at must not be empty")
			}
			return request.Validate()
		},
		RunE: func(cmd *cobra.Command, _ []string) (err error) {
			if journalPath != "" {
				journal, err := assess.OpenJournal(journalPath)
				if err != nil {
					return err
				}
				defer func() { err = errors.Join(err, journal.Close()) }()
				request.Journal = journal
			}
			result, err := app.Assess(cmd.Context(), r.config, request)
			if err != nil {
				return err
			}
			incomplete := false
			counts := map[string]int{}
			for _, port := range result.Ports {
				counts[port.Outcome]++
				incomplete = incomplete || port.Outcome == portedit.Blocked || port.Outcome == portedit.Unsupported || port.Outcome == portedit.Unknown
			}
			if r.json {
				err = r.emit(result)
			} else {
				progress.VerboseReport(cmd.Context(), "Assessing committed source %s; working-tree edits are excluded", result.Source.Commit)
				if len(result.Ports) == 0 {
					fmt.Fprintln(cmd.OutOrStdout(), "No ports matched the selectors.")
				}
				for _, port := range result.Ports {
					fmt.Fprintf(cmd.OutOrStdout(), "%s: %s", plain(port.Selector), assessmentWord(port.Outcome))
					if port.CurrentVersion != "" {
						fmt.Fprintf(cmd.OutOrStdout(), "; current %s", plain(port.CurrentVersion))
					}
					if release := port.Release; release != nil {
						fmt.Fprintf(cmd.OutOrStdout(), "; candidate %s", plain(release.Version))
						if release.Tag != "" {
							fmt.Fprintf(cmd.OutOrStdout(), " (tag %s)", plain(release.Tag))
						}
						if release.LeavesStable {
							fmt.Fprint(cmd.OutOrStdout(), "; prerelease: leaves stable")
						}
					}
					fmt.Fprintln(cmd.OutOrStdout())
					for _, input := range port.Inputs {
						fmt.Fprintf(cmd.OutOrStdout(), "  input: %s:%d:%d (%s)\n", plain(port.Portfile), input.Line, input.Column, plain(input.Value))
					}
					if scope := port.Scope; scope != nil {
						for _, member := range scope.Affected {
							fmt.Fprintf(cmd.OutOrStdout(), "  affected: %s %s -> %s (metadata only: %t)\n", plain(member.Target.Name), plain(member.Before.Version), plain(member.After.Version), member.MetadataOnly)
						}
					}
					for _, finding := range port.Findings {
						fmt.Fprintf(cmd.OutOrStdout(), "  %s: %s; %s\n", finding.Check, finding.Status, plain(finding.Detail))
					}
				}
				fmt.Fprintf(cmd.OutOrStdout(), "Assessed %d: %d ready, %d candidate ready, %d blocked, %d unsupported, %d unknown.", len(result.Ports), counts[portedit.InputFound], counts[portedit.CandidateChecked], counts[portedit.Blocked], counts[portedit.Unsupported], counts[portedit.Unknown])
				if result.Skipped > 0 {
					fmt.Fprintf(cmd.OutOrStdout(), " %d already in the journal.", result.Skipped)
				}
				fmt.Fprintln(cmd.OutOrStdout())
			}
			if err != nil {
				return err
			}
			if incomplete {
				return fmt.Errorf("some preparation assessments are blocked, unsupported, or unknown; see the per-port results")
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&request.SharedRelease, "shared-release", false, "Assess the full shared release across sibling subports")
	cmd.Flags().StringVar(&request.Version, "at", "", "Check the update to one explicit upstream version or tag (one port only; forge sources query upstream)")
	cmd.Flags().StringArrayVar(&request.Selection.Maintainers, "maintainer", nil, "Exact maintainer: @handle, handle@github, or email; or a class, openmaintainer or nomaintainer (repeatable)")
	cmd.Flags().StringArrayVar(&request.Selection.NotMaintainers, "not-maintainer", nil, "Leave out ports with this maintainer, spelled as for --maintainer (repeatable; goes with --all too)")
	cmd.Flags().StringArrayVar(&request.Selection.Categories, "category", nil, "Exact MacPorts category (repeatable; intersects maintainer selection)")
	cmd.Flags().BoolVar(&request.Selection.All, "all", false, "Assess the entire committed ports tree")
	cmd.Flags().StringVar(&journalPath, "journal", "", "Append one JSON line per port to this file as each finishes; a rerun with the same file skips the ports it holds, so an interrupted run continues")
	_ = cmd.MarkFlagFilename("journal", "jsonl")
	return cmd
}

// assessmentWord is the plain headline for an assessment outcome; the code
// itself stays in JSON.
func assessmentWord(outcome string) string {
	switch outcome {
	case portedit.InputFound:
		return "ready"
	case portedit.CandidateChecked:
		return "candidate ready"
	}
	return outcome
}
