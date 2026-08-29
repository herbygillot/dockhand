package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/herbygillot/dockhand/internal/doctor"
)

// Doctor builds the doctor subcommand: report which tools are present
// and which capabilities they enable.
func Doctor() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Report which tools are present and which capabilities they enable",
		Args:  noArgs,
		Run: func(*cobra.Command, []string) {
			fmt.Print(doctor.Probe())
		},
	}
}
