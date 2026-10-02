package command

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/herbygillot/dockhand/internal/buildinfo"
	"github.com/herbygillot/dockhand/internal/progress"
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
	// status is standard error's one redrawn line, which progress reports
	// print around.
	status *statusLine
}

// logo opens the main help, as it did in dockhand's earlier generations,
// with the build's version on the line under it. The trailing spaces are
// the art's own.
const logo = `     _            _    _                     _
  __| | ___   ___| | _| |__   __ _ _ __   __| |
 / _` + "`" + ` |/ _ \ / __| |/ / '_ \ / _` + "`" + ` | '_ \ / _` + "`" + ` |
| (_| | (_) | (__|   <| | | | (_| | | | | (_| |
 \__,_|\___/ \___|_|\_\_| |_|\__,_|_| |_|\__,_|
`

// stderrLine is the command's redrawn line on standard error.
func (s Streams) stderrLine() *statusLine {
	if s.status != nil {
		return s.status
	}
	return &statusLine{w: s.Err}
}

// errTerminal reports whether errors and progress go to a terminal, where
// a line can be redrawn; a --json command line's never do.
func (s Streams) errTerminal() bool {
	if s.json() {
		return false
	}
	if s.interactive {
		return true
	}
	file, ok := s.Err.(*os.File)
	return ok && isTerminal(file.Fd())
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

// unattended are the streams of a command that asks nothing, whatever its
// input, as bump does: everything that would ask goes the way it goes
// without a terminal.
func (s Streams) unattended() Streams {
	s.In, s.interactive, s.lines = strings.NewReader(""), false, nil
	return s
}

// gettingStarted is the main help's introduction: how to begin, and where
// the guide is.
const gettingStarted = `In your ports checkout, dockhand init sets up, and dockhand providers setup
tart makes a clean macOS image for checks to build in. Then:

  dockhand update <port> --new    a branch with the port at its newest release
  dockhand check                  build what the branch changes
  dockhand tidy                   shape its commits for review
  dockhand submit                 open the pull request from your fork

dockhand bump <port> does all four asking nothing, and stops where you
should look. docs/usage.md is the guide, and dockhand status shows where
things are.`

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
	if streams.status == nil {
		streams.status = &statusLine{w: streams.Err}
	}
	var asJSON bool
	var verbosity int
	root := &cobra.Command{
		Use:           "dockhand",
		Short:         "Author, check, and submit changes to MacPorts ports",
		Long:          logo + buildinfo.Current().String() + "\n\nAuthor, check, and submit changes to MacPorts ports.\n\n" + gettingStarted,
		Version:       buildinfo.Current().String(),
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
	root.PersistentFlags().CountVarP(&verbosity, "verbose", "v", "say more of what dockhand does as it works on standard error: -v the work behind the scenes, -vv every step")
	// A command that can't report as JSON is refused before it does
	// anything, so --json never does work it can't report.
	root.PersistentPreRunE = func(cmd *cobra.Command, _ []string) error {
		// What the work reports as it goes goes to standard error (Design
		// v3 §12): what a person needs to follow it, and with -v and -vv,
		// the work behind the scenes and every step.
		threshold := progress.Level(min(verbosity, int(progress.Debug)))
		// A report about a part of the work, as one environment of a check,
		// is indented under the command's own, as the run's lines are: the
		// index's lines began at the margin among the provider's (the hugo
		// exercise's check-65).
		cmd.SetContext(progress.WithReporter(cmd.Context(), func(update progress.Update) {
			if update.Level > threshold {
				return
			}
			if update.About != "" {
				streams.status.say("  " + update.Message)
				return
			}
			streams.status.say(update.Message)
		}))
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
		bumpCommand(&settings, streams),
		updateCommand(&settings, streams),
		checksumsCommand(&settings, streams),
		revbumpCommand(&settings, streams),
		createCommand(&settings, streams),
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
		outdatedCommand(&settings, streams),
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
		providersCommand(&settings, streams),
		authCommand(streams),
		restoreCommand(&settings, streams),
		archiveCommand(&settings, streams),
		cleanCommand(&settings, streams),
		explainCommand(streams),
		configCommand(&settings, streams),
	} {
		command.GroupID = "occasional"
		root.AddCommand(command)
	}
	// The commands whose results --json reports; any other refuses it.
	for _, command := range root.Commands() {
		switch command.Name() {
		case "status", "path", "diff", "impact", "check", "retry", "wait", "queue", "config",
			"start", "adopt", "bump", "update", "checksums", "revbump", "create", "edit", "tidy", "submit", "rebase", "review",
			"cancel", "logs", "restore", "archive", "clean", "explain", "outdated":
			supportsJSON(command)
		}
	}
	root.SetArgs(args)
	root.SetIn(streams.In)
	root.SetOut(streams.Out)
	root.SetErr(streams.Err)
	err := root.ExecuteContext(ctx)
	settings.cleanupAfter(streams, mode.command)
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
