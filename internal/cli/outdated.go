package cli

import (
	"fmt"
	"github.com/herbygillot/dockhand/internal/progress"

	"github.com/herbygillot/dockhand/internal/app"
	"github.com/herbygillot/dockhand/internal/outdated"
	"github.com/spf13/cobra"
)

func (r *runtime) outdatedCommand() *cobra.Command {
	var selection outdated.Selection
	cmd := &cobra.Command{
		Use: "outdated [port...]", Short: "Check committed ports for upstream updates",
		Long:        "Check explicit ports, or select by --maintainer and --category, from local HEAD using their GitHub or GitLab catalogs, or the livecheck MacPorts itself resolves for the port, including pypi, sourceforge, and other checker types, as long as it comes down to a regex without custom hooks. Working-tree edits are excluded. Reports current, update-available, and unknown assessments; unsupported or failed observations remain visible. Does not fetch MacPorts master, open the state database, create jobs, or authorize publication.",
		Annotations: map[string]string{stateIndependentHelp: "true"},
		Args:        func(_ *cobra.Command, args []string) error { selection.Ports = args; return selection.Validate() },
		RunE: func(cmd *cobra.Command, args []string) error {
			result, err := app.Outdated(cmd.Context(), r.config, selection)
			if err != nil {
				return err
			}
			if r.json {
				err = r.emit(result)
			} else {
				progress.VerboseReport(cmd.Context(), "Inspecting committed source %s; working-tree edits are excluded", result.Source.Commit)
				if len(result.Ports) == 0 {
					fmt.Fprintln(cmd.OutOrStdout(), "No ports matched the selectors.")
				}
				for _, port := range result.Ports {
					fmt.Fprintf(cmd.OutOrStdout(), "%s: %s", plain(port.Selector), port.Headline())
					if port.CurrentVersion != "" {
						fmt.Fprintf(cmd.OutOrStdout(), "; current %s", plain(port.CurrentVersion))
					}
					if port.CandidateVersion != "" {
						fmt.Fprintf(cmd.OutOrStdout(), "; upstream %s", plain(port.CandidateVersion))
					}
					if port.Detail != "" {
						fmt.Fprintf(cmd.OutOrStdout(), "; %s", plain(port.Detail))
					}
					fmt.Fprintln(cmd.OutOrStdout())
				}
			}
			if err != nil {
				return err
			}
			if result.Incomplete() {
				return fmt.Errorf("some upstream observations are unknown; see the per-port results")
			}
			return nil
		},
	}
	cmd.Flags().StringArrayVar(&selection.Maintainers, "maintainer", nil, "Exact maintainer: @handle, handle@github, or email; or a class, openmaintainer or nomaintainer (repeatable)")
	cmd.Flags().StringArrayVar(&selection.NotMaintainers, "not-maintainer", nil, "Leave out ports with this maintainer, spelled as for --maintainer (repeatable; goes with --all too)")
	cmd.Flags().StringArrayVar(&selection.Categories, "category", nil, "Exact MacPorts category (repeatable; intersects maintainer selection)")
	return cmd
}
