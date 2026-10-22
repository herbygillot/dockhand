package command

import (
	"errors"

	"github.com/spf13/cobra"

	"github.com/herbygillot/dockhand/internal/engine"
	"github.com/herbygillot/dockhand/internal/term"
)

func shellCommand(s *settings, streams term.Streams) *cobra.Command {
	var on, port string
	cmd := &cobra.Command{
		Use:   "shell <check> [-- <command>...]",
		Short: "Run a command, or open a shell, in a clone a check kept",
		Long: `Starts a clone check --keep kept, runs the command in the port's work
directory, its staged directory where MacPorts' work is gone, streams what
it says, exits with its code, and stops the clone again; the clone stays
kept. With no command, on a terminal, it opens a shell there.

A check that kept a clone on several releases needs --on to choose one, and
a check of several ports builds in the first port it changed's directory
unless --port names another. The command form is the one a script or an
agent can use:

  dockhand shell check-12 -- otool -L /opt/local/bin/harbor`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if at := cmd.ArgsLenAtDash(); at > 1 || (at < 0 && len(args) > 1) {
				return errors.New("name one check, and put the command after --")
			}
			command := args[1:]
			if len(command) == 0 && !streams.Terminal() {
				return errors.New("without a terminal, name the command to run after --, as dockhand shell check-12 -- ls")
			}
			ctx := cmd.Context()
			e, err := s.open(ctx)
			if err != nil {
				return err
			}
			defer e.Close()
			run, err := e.RunNamed(ctx, args[0])
			if err != nil {
				return err
			}
			code, err := e.Shell(ctx, engine.ShellRequest{Run: run, Release: on, Port: port, Command: command, Input: streams.In, Output: streams.Out, Terminal: len(command) == 0})
			if err != nil {
				return err
			}
			if code != 0 {
				return &term.ExitError{Code: code}
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&on, "on", "", "the release whose clone to start, as tahoe or 26, where the check kept several")
	cmd.Flags().StringVar(&port, "port", "", "the port whose work directory the command runs in, the first the check changed where it's empty")
	return cmd
}
