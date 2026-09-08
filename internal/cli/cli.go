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
	add("intent", "Change a port:", intentCommands(s)...)
	// dismiss sits with the verbs that read a verification's answer,
	// because that is what it answers: a proposal is something a
	// settlement found, and saying no to one is the other half of the
	// cohort verb that says yes.
	add("test", "Test the port:", verifyCmd(s), statusCmd(s), cancelCmd(s), dismissCmd(s))
	add("submit", "Submit the port:", promoteCmd(s))
	add("env", "Troubleshoot the port:", logCmd(s), shellCmd(s))
	// hold and unhold sit at the FRONT of this group, ahead of the verbs
	// that remove things: they are the brake on every road in the tool —
	// publication, verification, retirement — and a reader scanning this
	// group for "how do I stop it" should meet them before they meet
	// discard, which is how the question gets answered by deleting the
	// work instead. Behind them sit the two pass verbs, and the order
	// between THEM is the dispatch ruling: `dispatch` is the machine's
	// resident loop and `cycle` is the person's one-shot, which is why
	// they are filed together here rather than one of them being shelved
	// under Setup as a daemon.
	add("branch", "Housekeeping:", holdCmd(s), unholdCmd(s), discardCmd(s), purgeCmd(s), dispatchCmd(s), cycleCmd(s))
	add("report", "Reports:", outdatedCmd(s), classifyCmd(s))
	// Setup is the verbs that run BEFORE there is anything to maintain,
	// and mostly outside a checkout. Filing doctor here rather than under
	// Reports says plainly that it reports on the MACHINE and not on the
	// ports.
	add("setup", "Setup:", provisionCmd(s), doctorCmd(s))
	root.AddCommand(execCmd(s), versionCmd())
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
