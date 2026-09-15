package cli

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/herbygillot/dockhand/internal/app"
	"github.com/herbygillot/dockhand/internal/upstream"
	"github.com/spf13/cobra"
)

func (r *runtime) outdatedCommand() *cobra.Command {
	return &cobra.Command{
		Use: "outdated <port> [port...]", Short: "Check committed ports for upstream updates",
		Long:        "Check explicitly selected ports from local HEAD using their GitHub or GitLab source conventions. Working-tree edits are excluded. Reports current, update-available, and unknown assessments; unsupported or failed observations remain visible. Does not fetch MacPorts master, open the state database, create jobs, or authorize publication.",
		Annotations: map[string]string{stateIndependentHelp: "true"},
		Args:        cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			for _, arg := range args {
				if strings.TrimSpace(arg) == "" {
					return fmt.Errorf("port must not be empty")
				}
			}
			result, err := app.Outdated(cmd.Context(), r.config, args)
			if err != nil {
				return err
			}
			unknown := false
			for _, port := range result.Ports {
				unknown = unknown || port.Assessment == upstream.Unknown
			}
			if r.json {
				err = json.NewEncoder(cmd.OutOrStdout()).Encode(result)
			} else {
				fmt.Fprintf(cmd.ErrOrStderr(), "Inspecting committed source %s; working-tree edits are excluded.\n", result.Source.Commit)
				for _, port := range result.Ports {
					fmt.Fprintf(cmd.OutOrStdout(), "%s: %s", plain(port.Selector), port.Assessment)
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
			if unknown {
				return fmt.Errorf("some upstream observations are unknown; see the per-port results")
			}
			return nil
		},
	}
}
