// Package cli is dockhand's cobra adapter and its COMPOSITION ROOT, and
// it is those two things and nothing else. It parses a command line into
// a request, acquires exactly the services that request declares, builds
// ONE app operation, runs it, renders the typed result through report,
// and closes what it opened in reverse order whether the operation
// succeeded or not.
//
// IT SEQUENCES NOTHING. That is the whole of what this package lost when
// internal/cmd became internal/cli: the shipped adapter ran multi-step
// verbs out of its own Actions — mint then publish, resolve then cancel
// then close — and every one of those sequences is now an app operation
// with a name. There is exactly ONE line of sequencing left in this
// package and it is written out at its call site: app.Promote after
// app.Change under --to-pr on a verifier-less host, which is a
// DELEGATION between two operations rather than a road assembled from
// stages. If a second one ever appears here, the operation it belongs to
// is missing.
//
// WHAT IT OWNS THAT NOTHING ELSE MAY. The flags, the exit-code table,
// the two lockfiles, the dispatch loop and its signals, the clock, the
// streams, and who this process is: record.OwnerID.Root is canonical at
// exactly one point and that point is Services.Me. Operations open no
// file, take no lock, read no environment variable and name no verb —
// every one of those is a dependency this package resolves and hands in
// as a value.
package cli

import (
	"context"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/herbygillot/dockhand/internal/exitcode"
	"github.com/herbygillot/dockhand/internal/gh"
	"github.com/herbygillot/dockhand/internal/tool"
)

// logo opens the help message. The trailing spaces are the art's own.
const logo = `     _            _    _                     _
  __| | ___   ___| | _| |__   __ _ _ __   __| |
 / _` + "`" + ` |/ _ \ / __| |/ / '_ \ / _` + "`" + ` | '_ \ / _` + "`" + ` |
| (_| | (_) | (__|   <| | | | (_| | | | | (_| |
 \__,_|\___/ \___|_|\_\_| |_|\__,_|_| |_|\__,_|
`

// agentEnv names which AI agent was driving. It is recorded beside the
// invoker and READ BY NO GATE: setting it can neither grant nor withhold
// anything, which is why it survived the retirement of --auto untouched
// — it says who was driving, never who is entitled.
//
// It is read ONCE, at the composition root, and this is the constant
// that names it. A resident dispatcher makes it a half-truth worth
// stating: on a dispatched checkout the process that reads this is the
// daemon, so every change the dispatcher touches carries the agent named
// in a launchd plist rather than the one in the shell where the work was
// queued. Nothing breaks loudly, because nothing reads it; the
// provenance simply stops being true, and a per-record agent is what
// would fix it.
const agentEnv = "AI_AGENT"

// Root builds the dockhand command tree.
func Root(version string) *cobra.Command {
	root, _ := newRoot(version)
	return root
}

// newRoot builds the tree along with the Services it belongs to.
// Execute needs both — the services have to be closed however the
// command ends.
//
// THE COMPOSITION ROOT IS HERE AND THE ACQUISITION IS NOT. What this
// function wires are the SEAMS — the one tool finder over the real PATH
// search, the two provider resolvers built over it, the forge runner,
// the clock, the build's publication grant — and what it does not do is
// open anything. Nothing is resolved until a verb's own PreRun states
// its app.Needs, which is what makes "no dependency is resolved that the
// invocation will not use" a property of the code rather than a habit.
func newRoot(version string) (*cobra.Command, *Services) {
	tools := tool.NewFinder(nil)
	s := &Services{
		Tools:    tools,
		Version:  version,
		Verifier: realVerifier(tools),
		Lister:   realLister(tools),
		Now:      time.Now,
		// THE PROCESS'S BIRTH, read once, here, as close to the actual
		// start as this package gets. lease.sameProcess compares it
		// against the kernel's fork time with a one-minute window sized on
		// Go's own startup, so it must be read at startup and never
		// recomputed — see Services.Me for what restamping it cost.
		born: time.Now().UTC(),
		// The agent marker is process state, so it is read here and
		// nowhere below: a service that read its own environment would be
		// deciding provenance rather than being told it.
		Agent: os.Getenv(agentEnv),
		// The build's answer about unattended publication, spent here and
		// only here. grant.go says what it is and why it is a constant.
		Grant: machineGrant,
	}
	s.Forge = gh.RealGhOut(tools)

	root := &cobra.Command{
		Use:          "dockhand",
		Short:        "A port maintenance utility for MacPorts",
		Long:         logo + "\nA port maintenance utility for MacPorts.\nFrom upstream release to submitted port.",
		Version:      version,
		SilenceUsage: true,
		PersistentPreRunE: func(c *cobra.Command, _ []string) error {
			treeRoot, err := c.Flags().GetString("tree")
			if err != nil {
				return err
			}
			prefixPath, err := c.Flags().GetString("prefix")
			if err != nil {
				return err
			}
			debug, err := c.Flags().GetBool("debug")
			if err != nil {
				return err
			}
			// The logger is configured before anything else, because the
			// tree search below speaks through it: --debug has to be able to
			// say which tree was found. It belongs to this layer because
			// --debug is a flag, and Services holds facilities rather than
			// process-wide settings.
			level := slog.LevelWarn
			if debug {
				level = slog.LevelDebug
			}
			slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level})))
			s.TreeRoot, s.PrefixPath, s.Debug = discoverTree(treeRoot), prefixPath, debug
			s.Out, s.Err = c.OutOrStdout(), c.ErrOrStderr()
			return nil
		},
	}
	root.SetErrPrefix("dockhand:")
	root.SetVersionTemplate("dockhand {{.Version}}\n")
	// Pre-defining the version flag gives it the -V shorthand; cobra only
	// adds its own (shorthand-less) flag when none exists.
	root.Flags().BoolP("version", "V", false, "print the version")

	// THE PERSISTENT FLAGS ARE EXACTLY THREE. --auto and DOCKHAND_AUTO
	// are gone with the dispatch ruling: the invoker stops being
	// something an invocation asserts and becomes a property of WHICH
	// PROCESS acted — `dispatch` is the machine, every typed verb is a
	// person — so nothing replaces it as a flag. Nothing was promoted to
	// join them either: --dry-run belongs to the pass verbs alone.
	//
	// The two environment variables that remain are flag DEFAULTS, so the
	// flag always wins and a Changed() test on them is meaningless.
	root.PersistentFlags().StringP("prefix", "p", os.Getenv("DOCKHAND_PREFIX"),
		"MacPorts installation prefix (default $DOCKHAND_PREFIX, else discovered)")
	root.PersistentFlags().StringP("tree", "t", os.Getenv("DOCKHAND_TREE"),
		"ports tree root (default $DOCKHAND_TREE, else the tree the working directory is in)")
	root.PersistentFlags().Bool("debug", false,
		"print debug output to stderr")

	// Flag-parse failures are usage errors; cobra's own are untyped.
	root.SetFlagErrorFunc(func(_ *cobra.Command, err error) error {
		return &UsageError{Err: err}
	})

	// Registration order is display order — the help reads as the
	// workflow, so the workflow decides the order, not the alphabet.
	cobra.EnableCommandSorting = false
	add := func(group, title string, cmds ...*cobra.Command) {
		root.AddGroup(&cobra.Group{ID: group, Title: title})
		for _, c := range cmds {
			c.GroupID = group
			root.AddCommand(c)
		}
	}
	// THE GROUPS ARE QUESTIONS A PERSON ARRIVES WITH, in the order the
	// work happens. Registration order is display order, so the help
	// reads as the workflow rather than as the alphabet.
	//
	// WHAT THIS REPLACED, and why, because three of the old groups were
	// answering no question at all:
	//
	// "Test the port" held status, cancel and dismiss beside verify. Only
	// verify tests anything. status is the most-run verb in the tool and
	// was buried under a heading that does not describe it, and dismiss
	// answers a FINDING — a person looking for "how do I answer this
	// proposal" would not have looked there.
	//
	// "Housekeeping" held three unrelated kinds under one word: a brake
	// (hold, unhold), a demolition (discard, purge) and the whole
	// unattended workflow (dispatch, cycle). dispatch is not housekeeping;
	// it is how the tool is meant to run.
	//
	// And exec was in no group at all, so a real verb sat among cobra's
	// own builtins under "Additional Commands".
	//
	// The rule now: every group is 2-3 verbs, every title is a verb
	// phrase, and a verb is filed by the QUESTION it answers rather than
	// by the machinery it happens to touch.
	add("survey", "Survey the tree:", outdatedCmd(s), classifyCmd(s))
	add("intent", "Change a port:", intentCommands(s)...)
	// The two forward moves, together: everything else in this tool
	// either watches one of these or undoes it.
	add("advance", "Verify and submit:", verifyCmd(s), promoteCmd(s))
	// Watching, and looking inside what is being watched. status leads
	// because it is the verb a person runs most and the one that names
	// every road out of what it shows.
	add("follow", "Follow a change:", statusCmd(s), logCmd(s), shellCmd(s))
	// Telling dockhand what YOU decided: a proposal answered, a change
	// held, a hold released. dismiss belongs here and not beside verify —
	// it is a person's answer, not a test.
	add("steer", "Steer a change:", dismissCmd(s), holdCmd(s), unholdCmd(s))
	// The escalation, in order of how much it takes: a run, a change, the
	// checkout. Filed together because they answer one question — "make
	// it stop" — and a person asking it should see all three costs at
	// once rather than reaching for the largest.
	add("undo", "Stop or undo:", cancelCmd(s), discardCmd(s), purgeCmd(s))
	// The pass verbs are their own group because they are not
	// housekeeping: they are every group above, run together. The order
	// between them is the dispatch ruling — cycle is the person's
	// one-shot, dispatch the machine's resident loop.
	add("pass", "Run the whole workflow:", cycleCmd(s), dispatchCmd(s))
	// The machine rather than the ports, which is why doctor is here and
	// not under Survey, and it is last because it is done once.
	add("setup", "Set up the machine:", provisionCmd(s), execCmd(s), doctorCmd(s))
	root.AddCommand(versionCmd())
	return root, s
}

// discoverTree is the one place --tree's absence is answered. With no
// tree named, the one the user is standing in is the one they mean.
//
// Best-effort on purpose: a verb that needs no tree must not fail
// because the working directory is not in one, so a fruitless search
// leaves the root empty and the verbs that DO need a tree report it
// themselves through Services.Acquire.
func discoverTree(named string) string {
	if named != "" {
		return named
	}
	wd, err := os.Getwd()
	if err != nil {
		return ""
	}
	root, err := findTree(wd)
	if err != nil {
		return ""
	}
	slog.Debug("ports tree discovered from the working directory", "tree", root)
	return root
}

// Execute runs the dockhand command tree against os.Args and returns the
// process exit code.
func Execute(version string) int {
	ctx, stop := interruptContext(context.Background())
	defer stop()
	return execute(ctx, version, os.Args[1:], os.Stdout, os.Stderr)
}

// interruptContext is REAL SIGNAL HANDLING, and it exists because the
// shipped one was not.
//
// internal/cmd/root.go promised "A second signal still kills outright"
// over os/signal.NotifyContext, and NotifyContext does not do that. It
// registers a BUFFERED CHANNEL OF ONE and its goroutine RETURNS after
// the first delivery, while signal.Notify stays registered — so the
// default SIGINT disposition remains disabled for the life of the
// process and signal number two is buffered and then dropped. On a
// one-shot verb that is a latent bug. On `dockhand dispatch`, which a
// person will Ctrl-C twice because a drain is taking minutes, it is the
// first thing they hit, and the promise has to be kept before a resident
// process inherits it.
//
// So: a channel of two, a goroutine that STAYS, and on the second signal
// signal.Stop — which restores the default disposition — followed by
// re-raising the same signal at this process, which is what "kills
// outright" means and what a wrapper reading $? expects to see (128+n
// rather than a code dockhand chose).
//
// The FIRST signal cancels the context rather than killing, so deferred
// cleanup actually runs: without it a Ctrl-C mid-fetch left the run's
// temporary files behind with nothing to attribute them to. What changed
// under the dispatch ruling is not the mechanism but the STAKE — the
// gated create whose interrupt had to mean "pause, not destroy" no
// longer exists, and on every road the build is detached and owned by
// the record, so an interrupt now stops a person watching and never the
// work.
func interruptContext(parent context.Context) (context.Context, func()) {
	ctx, cancel := context.WithCancel(parent)
	ch := make(chan os.Signal, 2)
	signal.Notify(ch, os.Interrupt, syscall.SIGTERM)
	done := make(chan struct{})
	go func() {
		defer close(done)
		first := true
		for {
			select {
			case sig, ok := <-ch:
				if !ok {
					return
				}
				if first {
					first = false
					cancel()
					continue
				}
				// Restore the default disposition and let the signal do what
				// it would have done if dockhand had never registered.
				signal.Stop(ch)
				if s, ok := sig.(syscall.Signal); ok {
					_ = syscall.Kill(os.Getpid(), s)
				}
				return
			case <-ctx.Done():
				return
			}
		}
	}()
	return ctx, func() {
		signal.Stop(ch)
		cancel()
		<-done
	}
}

func execute(ctx context.Context, version string, args []string, out, errOut io.Writer) int {
	root, s := newRoot(version)
	// However the command ends — success, failure, interrupt — the
	// services give back what they took, in reverse order of acquisition.
	defer s.Close()
	root.SetOut(out)
	root.SetErr(errOut)
	root.SetArgs(args)
	// Unknown commands are usage errors, but cobra detects them inside
	// Execute with an untyped error; pre-flighting Find keeps the
	// classification identity-based. The default subcommands must exist
	// before Find, or `dockhand help` and `dockhand completion` would
	// themselves look unknown.
	root.InitDefaultHelpCmd()
	root.InitDefaultCompletionCmd()
	if _, _, err := root.Find(args); err != nil {
		root.PrintErrln(root.ErrPrefix(), err.Error())
		root.PrintErrf("Run '%v --help' for usage.\n", root.CommandPath())
		return exitcode.Usage
	}
	return ExitCode(root.ExecuteContext(ctx))
}

// exactArgs is cobra.ExactArgs classified as a usage error.
func exactArgs(n int) cobra.PositionalArgs {
	return func(c *cobra.Command, args []string) error {
		if err := cobra.ExactArgs(n)(c, args); err != nil {
			return &UsageError{Err: err}
		}
		return nil
	}
}

// noArgs is cobra.NoArgs classified as a usage error.
func noArgs(c *cobra.Command, args []string) error {
	if err := cobra.NoArgs(c, args); err != nil {
		return &UsageError{Err: err}
	}
	return nil
}

// versionCmd keeps "dockhand version" working alongside -V/--version.
func versionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the version",
		Args:  noArgs,
		Run: func(cmd *cobra.Command, _ []string) {
			cmd.Printf("dockhand %s\n", cmd.Root().Version)
		},
	}
}
