package cli

import (
	"encoding/json"
	"fmt"

	"github.com/herbygillot/dockhand/internal/app"
	"github.com/herbygillot/dockhand/internal/assess"
	"github.com/herbygillot/dockhand/internal/macports/portedit"
	"github.com/spf13/cobra"
)

func (r *runtime) assessCommand() *cobra.Command {
	var request assess.Request
	cmd := &cobra.Command{
		Use: "assess [port...]", Short: "Assess whether Dockhand can prepare a port update",
		Long:        "Assess committed ports from local HEAD; working-tree edits are excluded. Select explicit ports, exact maintainer/category filters, or --all. The default checks local declarations and probes version inputs without querying upstream. With --version, resolve a specific upstream tag and check the proposed edit. No source archives are downloaded, helpers executed, builds run, branches changed, or jobs created. Native Portfile evaluation still executes Tcl. An input-found or candidate-checked result is preparation evidence, not a build guarantee. Blocked, unsupported, or unknown results exit with status 1 after reporting all selected ports.",
		Example:     "  dockhand assess terraform\n  dockhand assess rust-analyzer --version 2026-09-14\n  dockhand assess --maintainer herbygillot@github\n  dockhand assess --all --json",
		Annotations: map[string]string{stateIndependentHelp: "true"},
		Args: func(cmd *cobra.Command, args []string) error {
			request.Selection.Ports = args
			if cmd.Flags().Changed("version") && request.Version == "" {
				return fmt.Errorf("assess: --version must not be empty")
			}
			return request.Validate()
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
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
				err = json.NewEncoder(cmd.OutOrStdout()).Encode(result)
			} else {
				fmt.Fprintf(cmd.ErrOrStderr(), "Assessing committed source %s; working-tree edits are excluded.\n", result.Source.Commit)
				if len(result.Ports) == 0 {
					fmt.Fprintln(cmd.OutOrStdout(), "No ports matched the selectors.")
				}
				for _, port := range result.Ports {
					fmt.Fprintf(cmd.OutOrStdout(), "%s: %s", plain(port.Selector), port.Outcome)
					if port.CurrentVersion != "" {
						fmt.Fprintf(cmd.OutOrStdout(), "; current %s", plain(port.CurrentVersion))
					}
					fmt.Fprintln(cmd.OutOrStdout())
					for _, input := range port.Inputs {
						fmt.Fprintf(cmd.OutOrStdout(), "  input: %s:%d:%d (%s)\n", plain(port.Portfile), input.Line, input.Column, plain(input.Value))
					}
					for _, finding := range port.Findings {
						fmt.Fprintf(cmd.OutOrStdout(), "  %s: %s; %s\n", finding.Check, finding.Status, plain(finding.Detail))
					}
				}
				fmt.Fprintf(cmd.OutOrStdout(), "Assessed %d: %d input-found, %d candidate-checked, %d blocked, %d unsupported, %d unknown.\n", len(result.Ports), counts[portedit.InputFound], counts[portedit.CandidateChecked], counts[portedit.Blocked], counts[portedit.Unsupported], counts[portedit.Unknown])
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
	cmd.Flags().StringVar(&request.Version, "version", "", "Check one explicit upstream version/tag (one port only; queries upstream)")
	cmd.Flags().StringArrayVar(&request.Selection.Maintainers, "maintainer", nil, "Exact maintainer: @handle, handle@github, or email (repeatable)")
	cmd.Flags().StringArrayVar(&request.Selection.Categories, "category", nil, "Exact MacPorts category (repeatable; intersects maintainer selection)")
	cmd.Flags().BoolVar(&request.Selection.All, "all", false, "Assess the entire committed ports tree")
	return cmd
}
