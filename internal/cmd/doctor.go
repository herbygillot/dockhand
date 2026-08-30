package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/herbygillot/dockhand/internal/doctor"
)

// Doctor builds the doctor subcommand: report which tools are present
// and which capabilities they enable.
func Doctor(rc *RunContext) *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Report which tools are present and which capabilities they enable",
		Args:  noArgs,
		// RunE rather than Run: the report is what this command
		// produces, and a truncated one written to a file must not
		// exit 0.
		RunE: func(*cobra.Command, []string) error {
			_, err := fmt.Fprint(rc.Out, doctor.Probe())
			return err
		},
	}
}
