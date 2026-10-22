package command

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/herbygillot/dockhand/internal/command/settings"
	"github.com/herbygillot/dockhand/internal/engine"
	"github.com/herbygillot/dockhand/internal/term"
)

func shellCommand(s *settings.Settings, streams term.Streams) *cobra.Command {
	var on, port string
	var test bool
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

  dockhand shell check-12 -- otool -L /opt/local/bin/harbor

--test copies the port's directory as it is in the branch now to where the
check staged it, and runs the test phase alone again with port -o, which
keeps the build though the Portfile changed. An edit outside the test
phase doesn't apply, and the run is never a result or evidence.`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if at := cmd.ArgsLenAtDash(); at > 1 || (at < 0 && len(args) > 1) {
				return errors.New("name one check, and put the command after --")
			}
			command := args[1:]
			if test && len(command) > 0 {
				return errors.New("--test runs the port's test phase; name no command with it")
			}
			if len(command) == 0 && !test && !streams.Terminal() {
				return errors.New("without a terminal, name the command to run after --, as dockhand shell check-12 -- ls")
			}
			ctx := cmd.Context()
			e, err := s.Open(ctx)
			if err != nil {
				return err
			}
			defer e.Close()
			run, err := e.RunNamed(ctx, args[0])
			if err != nil {
				return err
			}
			if test {
				fmt.Fprintln(streams.Err, "Running the test phase again with the port as it is in the branch now, on the build as it stands: an edit outside the test phase doesn't apply, and the run is no check's result.")
			}
			code, err := e.Shell(ctx, engine.ShellRequest{Run: run, Release: on, Port: port, Command: command, Test: test, Input: streams.In, Output: streams.Out, Terminal: len(command) == 0 && !test})
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
	cmd.Flags().BoolVar(&test, "test", false, "copy the port's directory, as it is in the branch now, into the clone, and run its test phase again on the build")
	cmd.Flags().StringVar(&port, "port", "", "the port whose work directory the command runs in, the first the check changed where it's empty")
	return cmd
}
