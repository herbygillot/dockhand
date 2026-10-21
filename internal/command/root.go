package command

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/herbygillot/dockhand/internal/buildinfo"
	"github.com/herbygillot/dockhand/internal/compose"
	"github.com/herbygillot/dockhand/internal/github"
	"github.com/herbygillot/dockhand/internal/progress"
	"github.com/herbygillot/dockhand/internal/prose"
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
// with the build's version on the line under it, indented a space to sit
// under the art. The trailing spaces are the art's own.
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
// a line can be redrawn.
func (s Streams) errTerminal() bool { return s.onTerminal(s.Err) }

// terminal reports whether the input is an interactive terminal, the only
// place a command may ask a question.
func (s Streams) terminal() bool { return s.onTerminal(s.In) }

// outTerminal reports whether the output is a terminal that can be
// redrawn.
func (s Streams) outTerminal() bool { return s.onTerminal(s.Out) }

// onTerminal reports whether one of the streams is a terminal. A --json
// command line's never is: it's a script's, which never asks and reads
// no redrawn line. /dev/null is a character device too, so this asks the
// terminal driver rather than the file's mode, of the file under the
// writer Run puts before the output, which watch's live view never saw
// past (the command package review's three predicates, folded).
func (s Streams) onTerminal(stream any) bool {
	if s.json() {
		return false
	}
	if s.interactive {
		return true
	}
	if w, ok := stream.(*switchWriter); ok {
		stream = w.w
	}
	file, ok := stream.(*os.File)
	return ok && isTerminal(file.Fd())
}

// width is the output terminal's columns, for text to wrap at, else 80.
func (s Streams) width() int {
	stream := s.Out
	if w, ok := stream.(*switchWriter); ok {
		stream = w.w
	}
	if file, ok := stream.(*os.File); ok {
		if columns := terminalWidth(file.Fd()); columns > 0 {
			return columns
		}
	}
	return 80
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
const gettingStarted = `In your ports checkout, dockhand setup sets up, offering the GitHub login,
your maintainers line, and a clean macOS image for checks to build in. Then:

  dockhand update <port> --new    a branch with the port at its newest release
  dockhand check                  build what the branch changes
  dockhand tidy                   shape its commits for review
  dockhand submit                 open the pull request from your fork

dockhand bump <port> does all four asking nothing, and stops where you
should look. docs/usage.md is the guide, and dockhand status shows where
things are.`

// Run executes the command line in args, in the process's environment and
// working directory.
func Run(ctx context.Context, args []string, streams Streams) error {
	dir, err := os.Getwd()
	if err != nil {
		return err
	}
	return run(ctx, invocation{args: args, env: os.Environ(), dir: dir}, streams, compose.Dependencies{}, machineHost())
}

// invocation is a command line: its arguments, and the environment and
// working directory it reads, as os.Environ and os.Getwd give them, in
// place of the process's.
type invocation struct {
	args, env []string
	dir       string
}

// run executes the command line on the host, with the stand-ins given,
// compose's real ones where they're empty, which a test gives its own.
func run(ctx context.Context, line invocation, streams Streams, dependencies compose.Dependencies, host *host) error {
	args := line.args
	var settings settings
	settings.deps, settings.host, settings.env, settings.dir = dependencies, host, line.env, line.dir
	stdout := streams.Out
	streams = runStreams(streams, args)
	settings.streams = streams
	out, mode := streams.Out.(*switchWriter), streams.mode
	root := newRoot(&settings, streams)
	// Cobra reads os.Args for nil arguments; Run's are only those given,
	// so a test binary's own flags never reach a command.
	root.SetArgs(append([]string{}, args...))
	root.SetIn(streams.In)
	root.SetOut(streams.Out)
	root.SetErr(streams.Err)
	spend := github.StartSpend()
	err := root.ExecuteContext(ctx)
	settings.cleanupAfter(streams, mode.command)
	// What the command spent of GitHub's hourly allowance, said with -v,
	// in the envelope, and in the spend log.
	mode.githubRequests = spend.Requests()
	if verbosity, _ := root.PersistentFlags().GetCount("verbose"); verbosity > 0 && mode.githubRequests > 0 {
		streams.status.say(fmt.Sprintf("GitHub's API: %s", prose.Plural(int(mode.githubRequests), "request")))
	}
	spend.Log(settings.composeOptions().SpendLog(), args)
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

// runStreams are the streams a command line runs with: its input read by
// line, its output switched off while --json gathers the envelope, its
// output mode, and its status line.
func runStreams(streams Streams, args []string) Streams {
	if streams.lines == nil && streams.In != nil {
		streams.lines = bufio.NewReader(streams.In)
	}
	streams.Out, streams.mode = &switchWriter{w: streams.Out}, &outputMode{json: asksForJSON(args)}
	if streams.status == nil {
		streams.status = &statusLine{w: streams.Err}
	}
	return streams
}

// newRoot is dockhand's command tree, which Run executes and the
// help-reference test reads (the architecture review of 5897a82, R3-22:
// a package-level seam in Run stood for it). streams are runStreams'.
func newRoot(settings *settings, streams Streams) *cobra.Command {
	out, mode := streams.Out.(*switchWriter), streams.mode
	var asJSON bool
	var verbosity int
	root := &cobra.Command{
		Use:           "dockhand",
		Short:         "Author, check, and submit changes to MacPorts ports",
		Long:          logo + " " + buildinfo.Current().String() + "\n\nAuthor, check, and submit changes to MacPorts ports.\n\n" + gettingStarted,
		Version:       buildinfo.Current().String(),
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          unknownCommand,
		// With no command, a ports checkout shows its status (Design v3
		// §10); anywhere else, the help.
		RunE: func(cmd *cobra.Command, _ []string) error {
			e, err := settings.open(cmd.Context())
			if err != nil {
				return cmd.Help()
			}
			defer e.Close()
			return showStatus(cmd.Context(), e, streams, nil, false, false, branchFlags{}, settings.places())
		},
	}
	root.SetVersionTemplate("dockhand {{.Version}}\n")
	settings.flags(root)
	root.PersistentFlags().BoolVar(&asJSON, "json", false, "write the result as one JSON envelope on standard output")
	root.PersistentFlags().CountVarP(&verbosity, "verbose", "v", "say more of what dockhand does as it works on standard error: -v the work behind the scenes, -vv every step")
	// A command that can't report as JSON is refused before it does
	// anything, so --json never does work it can't report.
	root.PersistentPreRunE = func(cmd *cobra.Command, args []string) error {
		// A command naming a branch finds its checkout from anywhere
		// (settings.registeredCheckout): by -b, -p, or --pr, or path,
		// status, and archive by its name.
		for _, flag := range []string{"branch", "port", "pr"} {
			if f := cmd.Flags().Lookup(flag); f != nil && f.Changed {
				settings.namesBranch = true
			}
		}
		if len(args) > 0 && slices.Contains([]string{"path", "status", "archive"}, cmd.Name()) {
			settings.namesBranch = true
		}
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
		setupCommand(settings, streams),
		startCommand(settings, streams),
		adoptCommand(settings, streams),
		pathCommand(settings, streams),
	} {
		command.GroupID = "work"
		root.AddCommand(command)
	}
	root.AddGroup(&cobra.Group{ID: "author", Title: "Author:"})
	for _, command := range []*cobra.Command{
		shipCommand(settings, streams),
		bumpCommand(settings, streams),
		updateCommand(settings, streams),
		checksumsCommand(settings, streams),
		revbumpCommand(settings, streams),
		createCommand(settings, streams),
		editCommand(settings, streams),
	} {
		command.GroupID = "author"
		root.AddCommand(command)
	}
	root.AddGroup(&cobra.Group{ID: "understand", Title: "Understand:"})
	for _, command := range []*cobra.Command{
		statusCommand(settings, streams),
		diffCommand(settings, streams),
		openCommand(settings, streams),
		impactCommand(settings, streams),
		outdatedCommand(settings, streams),
	} {
		command.GroupID = "understand"
		root.AddCommand(command)
	}
	root.AddGroup(&cobra.Group{ID: "check", Title: "Check:"})
	for _, command := range []*cobra.Command{
		checkCommand(settings, streams),
		logsCommand(settings, streams),
		retryCommand(settings, streams),
	} {
		command.GroupID = "check"
		root.AddCommand(command)
	}
	root.AddGroup(&cobra.Group{ID: "review", Title: "Prepare for review:"})
	for _, command := range []*cobra.Command{
		tidyCommand(settings, streams),
		submitCommand(settings, streams),
		rebaseCommand(settings, streams),
		reviewCommand(settings, streams),
	} {
		command.GroupID = "review"
		root.AddCommand(command)
	}
	root.AddGroup(&cobra.Group{ID: "queue", Title: "Keep work moving:"})
	for _, command := range []*cobra.Command{
		waitCommand(settings, streams),
		cancelCommand(settings, streams),
		serveCommand(settings, streams),
	} {
		command.GroupID = "queue"
		root.AddCommand(command)
	}
	root.AddGroup(&cobra.Group{ID: "occasional", Title: "Occasional:"})
	for _, command := range []*cobra.Command{
		undoCommand(settings, streams),
		archiveCommand(settings, streams),
		cleanCommand(settings, streams),
		explainCommand(streams),
		configCommand(settings, streams),
	} {
		command.GroupID = "occasional"
		root.AddCommand(command)
	}
	return root
}

// unknownCommand refuses a word that names no command, as cobra.NoArgs
// does, with cobra's own suggestions, which NoArgs leaves out: v0.3.0's
// init, auth, providers, and restore point at what replaced them.
// foldedCommands are the commands folded into another, and what does
// their work now.
var foldedCommands = map[string]string{
	"queue": "dockhand status lists the checks queued and running",
	"watch": "dockhand status --watch keeps it current",
}

func unknownCommand(cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		return nil
	}
	// A command folded into another says where it went (the second
	// look's §6).
	if moved, ok := foldedCommands[args[0]]; ok && !cmd.HasParent() {
		return fmt.Errorf("%s is part of status now: %s", args[0], moved)
	}
	message := fmt.Sprintf("unknown command %q for %q", args[0], cmd.CommandPath())
	if suggestions := cmd.SuggestionsFor(args[0]); len(suggestions) > 0 {
		message += "\n\nDid you mean this?\n"
		for _, suggestion := range suggestions {
			message += "\t" + suggestion + "\n"
		}
	}
	return errors.New(message)
}
