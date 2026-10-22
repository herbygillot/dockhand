package command

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/herbygillot/dockhand/internal/buildinfo"
	"github.com/herbygillot/dockhand/internal/command/author"
	"github.com/herbygillot/dockhand/internal/command/branchflags"
	"github.com/herbygillot/dockhand/internal/command/bump"
	"github.com/herbygillot/dockhand/internal/command/check"
	"github.com/herbygillot/dockhand/internal/command/clean"
	"github.com/herbygillot/dockhand/internal/command/config"
	"github.com/herbygillot/dockhand/internal/command/create"
	"github.com/herbygillot/dockhand/internal/command/explain"
	"github.com/herbygillot/dockhand/internal/command/host"
	"github.com/herbygillot/dockhand/internal/command/outdated"
	"github.com/herbygillot/dockhand/internal/command/review"
	"github.com/herbygillot/dockhand/internal/command/runcmd"
	"github.com/herbygillot/dockhand/internal/command/serve"
	"github.com/herbygillot/dockhand/internal/command/settings"
	"github.com/herbygillot/dockhand/internal/command/setup"
	"github.com/herbygillot/dockhand/internal/command/shell"
	"github.com/herbygillot/dockhand/internal/command/ship"
	"github.com/herbygillot/dockhand/internal/command/status"
	"github.com/herbygillot/dockhand/internal/command/submit"
	"github.com/herbygillot/dockhand/internal/command/tidy"
	"github.com/herbygillot/dockhand/internal/command/understand"
	"github.com/herbygillot/dockhand/internal/command/view"
	"github.com/herbygillot/dockhand/internal/compose"
	"github.com/herbygillot/dockhand/internal/github"
	"github.com/herbygillot/dockhand/internal/progress"
	"github.com/herbygillot/dockhand/internal/prose"
	"github.com/herbygillot/dockhand/internal/term"
)

// logo opens the main help, as it did in dockhand's earlier generations,
// with the build's version on the line under it, indented a space to sit
// under the art. The trailing spaces are the art's own.
const logo = `     _            _    _                     _
  __| | ___   ___| | _| |__   __ _ _ __   __| |
 / _` + "`" + ` |/ _ \ / __| |/ / '_ \ / _` + "`" + ` | '_ \ / _` + "`" + ` |
| (_| | (_) | (__|   <| | | | (_| | | | | (_| |
 \__,_|\___/ \___|_|\_\_| |_|\__,_|_| |_|\__,_|
`

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
func Run(ctx context.Context, args []string, streams term.Streams) error {
	dir, err := os.Getwd()
	if err != nil {
		return err
	}
	return RunWith(ctx, settings.Invocation{Args: args, Env: os.Environ(), Dir: dir}, streams, compose.Dependencies{}, host.MachineHost())
}

// run executes the command line on the host, with the stand-ins given,
// compose's real ones where they're empty, which a test gives its own.
func RunWith(ctx context.Context, line settings.Invocation, streams term.Streams, dependencies compose.Dependencies, mac *host.Host) error {
	args := line.Args
	var shared settings.Settings
	shared.Deps, shared.Host, shared.Env, shared.Dir, shared.Command = dependencies, mac, line.Env, line.Dir, Run
	streams = term.Begin(streams.WithDisplay(display(line.Env)), args)
	shared.Streams = streams
	root := newRoot(&shared, streams)
	// Cobra reads os.Args for nil arguments; Run's are only those given,
	// so a test binary's own flags never reach a command.
	root.SetArgs(append([]string{}, args...))
	root.SetIn(streams.In)
	root.SetOut(streams.Out)
	root.SetErr(streams.Err)
	spend := github.StartSpend()
	err := view.SayNext(root.ExecuteContext(ctx))
	clean.CleanupAfter(&shared, streams, streams.Command())
	// What the command spent of GitHub's hourly allowance, said with -v,
	// in the envelope, and in the spend log.
	requests := spend.Requests()
	if verbosity, _ := root.PersistentFlags().GetCount("verbose"); verbosity > 0 && requests > 0 {
		streams.Say(fmt.Sprintf("GitHub's API: %s", prose.Plural(int(requests), "request")))
	}
	spend.Log(shared.ComposeOptions().SpendLog(), args)
	return streams.Finish(args, requests, err)
}

// newRoot is dockhand's command tree, which Run executes and the
// help-reference test reads (the architecture review of 5897a82, R3-22:
// a package-level seam in Run stood for it). streams are term.Begin's.
func newRoot(shared *settings.Settings, streams term.Streams) *cobra.Command {
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
			e, err := shared.Open(cmd.Context())
			if err != nil {
				return cmd.Help()
			}
			defer e.Close()
			return status.ShowStatus(cmd.Context(), e, streams, nil, false, false, branchflags.BranchFlags{}, shared.Places())
		},
	}
	root.SetVersionTemplate("dockhand {{.Version}}\n")
	shared.Flags(root)
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
				shared.NamesBranch = true
			}
		}
		if len(args) > 0 && slices.Contains([]string{"path", "status", "archive"}, cmd.Name()) {
			shared.NamesBranch = true
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
				streams.Say("  " + update.Message)
				return
			}
			streams.Say(update.Message)
		}))
		command := strings.TrimPrefix(cmd.CommandPath(), "dockhand ")
		if cmd == root {
			command = "status"
		}
		streams.Reports(command, asJSON)
		if asJSON && cmd.Annotations[settings.JsonAnnotation] == "" {
			return fmt.Errorf("--json isn't available for dockhand %s yet; its output is text only", command)
		}
		return nil
	}
	settings.SupportsJSON(root)
	root.AddGroup(&cobra.Group{ID: "work", Title: "Start or enter work:"})
	for _, command := range []*cobra.Command{
		setup.Command(shared, streams),
		setup.StartCommand(shared, streams),
		setup.AdoptCommand(shared, streams),
		setup.PathCommand(shared, streams),
	} {
		command.GroupID = "work"
		root.AddCommand(command)
	}
	root.AddGroup(&cobra.Group{ID: "author", Title: "Author:"})
	for _, command := range []*cobra.Command{
		ship.Command(shared, streams),
		bump.Command(shared, streams),
		author.UpdateCommand(shared, streams),
		author.ChecksumsCommand(shared, streams),
		author.RevbumpCommand(shared, streams),
		create.Command(shared, streams),
		author.EditCommand(shared, streams),
	} {
		command.GroupID = "author"
		root.AddCommand(command)
	}
	root.AddGroup(&cobra.Group{ID: "understand", Title: "Understand:"})
	for _, command := range []*cobra.Command{
		status.Command(shared, streams),
		understand.DiffCommand(shared, streams),
		understand.OpenCommand(shared, streams),
		understand.ImpactCommand(shared, streams),
		outdated.Command(shared, streams),
	} {
		command.GroupID = "understand"
		root.AddCommand(command)
	}
	root.AddGroup(&cobra.Group{ID: "check", Title: "Check:"})
	for _, command := range []*cobra.Command{
		check.Command(shared, streams),
		runcmd.LogsCommand(shared, streams),
		shell.Command(shared, streams),
		runcmd.RetryCommand(shared, streams),
	} {
		command.GroupID = "check"
		root.AddCommand(command)
	}
	root.AddGroup(&cobra.Group{ID: "review", Title: "Prepare for review:"})
	for _, command := range []*cobra.Command{
		tidy.Command(shared, streams),
		submit.Command(shared, streams),
		tidy.RebaseCommand(shared, streams),
		review.Command(shared, streams),
	} {
		command.GroupID = "review"
		root.AddCommand(command)
	}
	root.AddGroup(&cobra.Group{ID: "queue", Title: "Keep work moving:"})
	for _, command := range []*cobra.Command{
		runcmd.WaitCommand(shared, streams),
		runcmd.CancelCommand(shared, streams),
		serve.Command(shared, streams),
	} {
		command.GroupID = "queue"
		root.AddCommand(command)
	}
	root.AddGroup(&cobra.Group{ID: "occasional", Title: "Occasional:"})
	for _, command := range []*cobra.Command{
		tidy.UndoCommand(shared, streams),
		clean.ArchiveCommand(shared, streams),
		clean.Command(shared, streams),
		explain.Command(streams),
		config.Command(shared, streams),
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

// display is how the command line's environment asks its text be shown:
// NO_COLOR, set and not empty, leaves the marks uncoloured, and COLUMNS is
// the width text wraps at off a terminal (term.Display).
func display(env []string) term.Display {
	var d term.Display
	for _, entry := range env {
		name, value, _ := strings.Cut(entry, "=")
		switch name {
		case "NO_COLOR":
			d.NoColour = value != ""
		case "COLUMNS":
			if columns, err := strconv.Atoi(value); err == nil && columns > 0 {
				d.Columns = columns
			}
		}
	}
	return d
}
