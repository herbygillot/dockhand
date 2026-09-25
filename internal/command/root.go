package command

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/herbygillot/dockhand/internal/version"
)

// Streams are the standard streams a command reads and writes.
type Streams struct {
	In  io.Reader
	Out io.Writer
	Err io.Writer
	// interactive stands in for a terminal in tests.
	interactive bool
	// lines reads answers from In; one reader for the whole command, so
	// input it buffered for one question is there for the next.
	lines *bufio.Reader
	// mode is whether this command line reports as JSON, and its result.
	mode *outputMode
}

// terminal reports whether the input is an interactive terminal, the only
// place a command may ask a question. /dev/null is a character device too,
// so this asks the terminal driver rather than the file's mode.
func (s Streams) terminal() bool {
	// A --json command line is a script's: it never asks.
	if s.json() {
		return false
	}
	if s.interactive {
		return true
	}
	file, ok := s.In.(*os.File)
	return ok && isTerminal(file.Fd())
}

const rebuilding = `dockhand is being rebuilt as v3 (docs/design-v3.md). The whole loop is here,
but check builds only on your own script (docs/command-provider.md) until the
Tart provider lands, and the real MacPorts paths are still to be proven on a
Mac. Until then, the working tool is v2, tagged v2-final:

  git worktree add ../dockhand-v2 v2-final
  make -C ../dockhand-v2 build BINARY="$HOME/.local/bin/dockhand-v2"`

// Run executes the command line in args.
func Run(ctx context.Context, args []string, streams Streams) error {
	var settings settings
	if streams.lines == nil && streams.In != nil {
		streams.lines = bufio.NewReader(streams.In)
	}
	stdout := streams.Out
	out := &switchWriter{w: stdout}
	mode := &outputMode{json: asksForJSON(args)}
	streams.Out, streams.mode = out, mode
	var asJSON bool
	root := &cobra.Command{
		Use:           "dockhand",
		Short:         "Author, check, and submit changes to MacPorts ports",
		Long:          "Author, check, and submit changes to MacPorts ports.\n\n" + rebuilding,
		Version:       version.Current().String(),
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.NoArgs,
		// With no command, a ports checkout shows its status (Design v3
		// §10); anywhere else, the help.
		RunE: func(cmd *cobra.Command, _ []string) error {
			e, err := settings.open(cmd.Context())
			if err != nil {
				return cmd.Help()
			}
			defer e.Close()
			return showStatus(cmd.Context(), e, streams, nil, false, false, "")
		},
	}
	root.SetVersionTemplate("dockhand {{.Version}}\n")
	settings.flags(root)
	root.PersistentFlags().BoolVar(&asJSON, "json", false, "write the result as one JSON envelope on standard output")
	// A command that can't report as JSON is refused before it does
	// anything, so --json never does work it can't report.
	root.PersistentPreRunE = func(cmd *cobra.Command, _ []string) error {
		mode.command = strings.TrimPrefix(cmd.CommandPath(), "dockhand ")
		if cmd == root {
			mode.command = "status"
		}
		if !asJSON {
			mode.json = false
			return nil
		}
		mode.json = true
		if cmd.Annotations[jsonAnnotation] == "" {
			return fmt.Errorf("--json isn't available for dockhand %s yet; its output is text only", mode.command)
		}
		out.silent = true
		return nil
	}
	supportsJSON(root)
	root.AddGroup(&cobra.Group{ID: "work", Title: "Start or enter work:"})
	for _, command := range []*cobra.Command{
		initCommand(&settings, streams),
		startCommand(&settings, streams),
		adoptCommand(&settings, streams),
		pathCommand(&settings, streams),
	} {
		command.GroupID = "work"
		root.AddCommand(command)
	}
	root.AddGroup(&cobra.Group{ID: "author", Title: "Author:"})
	for _, command := range []*cobra.Command{
		updateCommand(&settings, streams),
		checksumsCommand(&settings, streams),
		revbumpCommand(&settings, streams),
		editCommand(&settings, streams),
	} {
		command.GroupID = "author"
		root.AddCommand(command)
	}
	root.AddGroup(&cobra.Group{ID: "understand", Title: "Understand:"})
	for _, command := range []*cobra.Command{
		statusCommand(&settings, streams),
		diffCommand(&settings, streams),
		impactCommand(&settings, streams),
		watchCommand(&settings, streams),
	} {
		command.GroupID = "understand"
		root.AddCommand(command)
	}
	root.AddGroup(&cobra.Group{ID: "check", Title: "Check:"})
	for _, command := range []*cobra.Command{
		checkCommand(&settings, streams),
		logsCommand(&settings, streams),
		retryCommand(&settings, streams),
	} {
		command.GroupID = "check"
		root.AddCommand(command)
	}
	root.AddGroup(&cobra.Group{ID: "review", Title: "Prepare for review:"})
	for _, command := range []*cobra.Command{
		tidyCommand(&settings, streams),
		submitCommand(&settings, streams),
		rebaseCommand(&settings, streams),
		reviewCommand(&settings, streams),
	} {
		command.GroupID = "review"
		root.AddCommand(command)
	}
	root.AddGroup(&cobra.Group{ID: "queue", Title: "Keep work moving:"})
	for _, command := range []*cobra.Command{
		queueCommand(&settings, streams),
		waitCommand(&settings, streams),
		cancelCommand(&settings, streams),
		serveCommand(&settings, streams),
	} {
		command.GroupID = "queue"
		root.AddCommand(command)
	}
	root.AddGroup(&cobra.Group{ID: "occasional", Title: "Occasional:"})
	for _, command := range []*cobra.Command{
		authCommand(streams),
		restoreCommand(&settings, streams),
		archiveCommand(&settings, streams),
		cleanCommand(&settings, streams),
		explainCommand(streams),
	} {
		command.GroupID = "occasional"
		root.AddCommand(command)
	}
	// The commands whose results --json reports; any other refuses it.
	for _, command := range root.Commands() {
		switch command.Name() {
		case "status", "path", "diff", "impact", "check", "retry", "wait", "queue":
			supportsJSON(command)
		}
	}
	root.SetArgs(args)
	root.SetIn(streams.In)
	root.SetOut(streams.Out)
	root.SetErr(streams.Err)
	err := root.ExecuteContext(ctx)
	if !mode.json {
		return err
	}
	out.silent = false
	if mode.command == "" {
		mode.command = strings.Join(args, " ")
	}
	if writeErr := writeEnvelope(stdout, mode, err); writeErr != nil {
		return writeErr
	}
	// The envelope carries the error; the exit code is still the outcome's.
	if err != nil {
		return &ExitError{Code: ExitCode(err)}
	}
	return nil
}
