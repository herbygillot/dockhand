package shell

import (
	"context"
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/herbygillot/dockhand/internal/command/settings"
	"github.com/herbygillot/dockhand/internal/engine"
	"github.com/herbygillot/dockhand/internal/term"
)

func Command(s *settings.Settings, streams term.Streams) *cobra.Command {
	var on, port string
	var test bool
	cmd := &cobra.Command{
		Use:   "shell <check> [-- <command>...]",
		Short: "Run a command or open a shell in a retained check VM",
		Long: `Starts a VM retained after a failed check or by check --keep, and runs
the command in the port's work directory. If MacPorts' work directory is
gone, it uses the staged port directory. Command output is streamed, and
Dockhand returns the command's exit code. The VM is stopped afterward and
retained for later use. With no command, opens an interactive shell when
running in a terminal.

While a Tart check is running, enters its VM without stopping the check.
Results completed before the shell opened remain valid; later builds do
not count as evidence, because the shell may have changed their inputs.

If the check retained VMs for several releases, use --on to choose one.
For a check of several ports, the command runs in the first changed
port's directory unless --port names another. Scripts and agents can use
the command form:

  dockhand shell check-12 -- otool -L /opt/local/bin/harbor

--test copies the port's current branch files to where the check staged
them and reruns only the test phase with port -o, which keeps the
existing build even though the Portfile changed. Changes outside the test
phase are not applied, and the run never counts as a check result or as
evidence.`,
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
			// shell opens a check's kept clone, chosen by --on; one attempt,
			// or a provider's run, is refused by name, with its check
			// (docs/designs/check-lifecycle.md, "Selection").
			if !engine.IsCheckName(args[0]) {
				if selection, err := e.SelectBuild(ctx, args[0]); err == nil && selection.Kind != engine.SelectedCheck {
					return fmt.Errorf("shell opens a check's kept clone; %s is one %s of %s. Run dockhand shell %s, with --on for the release", args[0], selection.Kind, selection.Run.Name(), selection.Run.Name())
				}
			}
			run, err := e.RunNamed(ctx, args[0])
			if err != nil {
				return keptHere(ctx, e, err)
			}
			if test {
				fmt.Fprintln(streams.Err, "Rerunning the test phase using the current port files and the existing build. Changes outside the test phase are not applied. This run does not count as a check result.")
			}
			code, err := e.Shell(ctx, engine.ShellRequest{Run: run, Release: on, Port: port, Command: command, Test: test, Input: streams.In, Output: streams.Prose(), Terminal: len(command) == 0 && !test,
				Notice: func(words string) { fmt.Fprintln(streams.Err, words) }})
			if err != nil {
				return err
			}
			if code != 0 {
				return &term.ExitError{Code: code}
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&on, "on", "", "select a release when the check retained more than one clone, for example tahoe or 26")
	cmd.Flags().BoolVar(&test, "test", false, "copy the port's current branch files into the clone and rerun the test phase against the existing build")
	cmd.Flags().StringVar(&port, "port", "", "port whose work directory to use (default: the first changed port in the check)")
	return cmd
}

// keptHere is a name that isn't a run's refused with the latest check of
// the branch checked out here that kept a clone, where one did: "shell
// sh" met "sh is not a run's name" alone, the check's name erased from
// the terminal (field testing's curl +sectrust notes, note 5).
func keptHere(ctx context.Context, e *engine.Engine, err error) error {
	if !errors.As(err, new(engine.NotARunName)) {
		return err
	}
	branch, found := e.Current(ctx)
	if found != nil {
		return err
	}
	kept, found := e.KeptClones(ctx)
	if found != nil {
		return err
	}
	for _, leftover := range kept {
		if leftover.Run.Branch == branch.ID {
			return fmt.Errorf("%w; the latest check here that kept a clone is %s", err, leftover.Run.Name())
		}
	}
	return err
}
