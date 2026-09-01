package cmd

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/herbygillot/dockhand/internal/doctor"
	"github.com/herbygillot/dockhand/internal/runstate"
)

// doctorAction reports which tools are present and which capabilities
// they enable.
type doctorAction struct{}

var _ Action = doctorAction{}

func (doctorAction) Execute(_ context.Context, rs *runstate.Context) error {
	_, err := fmt.Fprint(rs.Out, doctor.Probe())
	return err
}

// Doctor builds the doctor subcommand.
func Doctor() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Report which tools are present and which capabilities they enable",
		Args:  noArgs,
		// RunE rather than lifecycle.Run: the report is what this command
		// produces, and a truncated one written to a file must not
		// exit 0.
		RunE: runE(func(*cobra.Command, []string) (Action, error) {
			return doctorAction{}, nil
		}),
	}
}
