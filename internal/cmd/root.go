// Package cmd holds the dockhand command tree: one public constructor
// per subcommand, assembled under Root, with cmd/dockhand as a thin
// entry point. Cobra parses through pflag, so the full POSIX/GNU flag
// surface (clustered shorts with glued values, interspersed flags)
// comes with it. The package also owns the exit-code table (exit.go):
// failures are classified by error identity, never by message text.
package cmd

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/herbygillot/dockhand/internal/exitcode"
	"github.com/herbygillot/dockhand/internal/gh"
	"github.com/herbygillot/dockhand/internal/runstate"
	"github.com/herbygillot/dockhand/internal/tool"
	"github.com/herbygillot/dockhand/internal/verify"
	"github.com/herbygillot/dockhand/internal/verify/tart"
	"github.com/herbygillot/dockhand/internal/verify/tart/provision"
)

// realVMProvider builds the resolver of the machine's verify provider
// — the tart provider assembled from the base images actually present,
// found through the run's finder. The two ways of having no
// environment are told apart, because their remedies are: no tart at
// all is ErrNoProvider, and the verbs narrow their contract around it
// (a bump warns and proceeds, a promote warns and allows); tart with
// no base images is ErrNoEnvironment, and the remedy is provisioning.
// Both exit in the machine band.
//
// It lives at the composition root rather than in the engine because
// it names the provider: the engine may speak verify's vocabulary and
// never the tart that implements it.
func realVMProvider(tools *tool.Finder) func(ctx context.Context) (verify.Verifier, error) {
	return func(ctx context.Context) (verify.Verifier, error) {
		if _, err := tools.Find(tool.Tart); err != nil {
			// The sentinel is new and the sentence is not: verify.NoProvider
			// carries ErrNoProvider under the words this refusal has always
			// used. What a caller can ask changed here; what a user reads
			// did not.
			return nil, verify.NoProvider(
				"tart is not installed (`port install tart`); --no-verify skips verification")
		}
		releases, err := (provision.Tart{Tools: tools}).Provisioned(ctx)
		if err != nil {
			return nil, err
		}
		if len(releases) == 0 {
			return nil, fmt.Errorf(
				"%w: no base images; run `dockhand provision tart --macos <release>` first",
				verify.ErrNoEnvironment)
		}
		// Newest first: the provider's default is its first base, and
		// the default a quick bump wants is the current OS — the
		// mundane-build check — not the oldest. Platform-floor
		// archaeology asks for old releases by name.
		bases := make([]tart.Base, 0, len(releases))
		for i := len(releases) - 1; i >= 0; i-- {
			bases = append(bases, tart.Base{VM: tart.BaseName(releases[i]), Release: releases[i]})
		}
		return tart.Provider{Bases: bases, Tools: tools}, nil
	}
}

// realWorkerLister builds the resolver of the backend the worker audit
// asks. tart on PATH is the whole gate — the same question the audit
// asked before it went through a capability — because listing guests
// needs no base image. That is the point of it being a separate
// resolver: realVMProvider refuses on a machine whose bases are gone,
// and a machine whose bases are gone is exactly where a cloned worker
// outlives them and pins one of two slots.
//
// The refusal's words never reach a user: the audit is best-effort and
// renders every refusal as silence.
func realWorkerLister(tools *tool.Finder) func(ctx context.Context) (verify.Verifier, error) {
	return func(context.Context) (verify.Verifier, error) {
		if _, err := tools.Find(tool.Tart); err != nil {
			return nil, verify.NoProvider("tart is not installed")
		}
		return tart.Provider{Tools: tools}, nil
	}
}

// Root builds the dockhand command tree. The run it will execute is
// created here and populated once the global flags are parsed, so every
// command draws its prefix, tree, evaluators and temporary space from one
// place instead of deriving them itself.
func Root(version string) *cobra.Command {
	root, _ := newRoot(version)
	return root
}

// logo opens the help message. The trailing spaces are the art's own.
const logo = `     _            _    _                     _
  __| | ___   ___| | _| |__   __ _ _ __   __| |
 / _` + "`" + ` |/ _ \ / __| |/ / '_ \ / _` + "`" + ` | '_ \ / _` + "`" + ` |
| (_| | (_) | (__|   <| | | | (_| | | | | (_| |
 \__,_|\___/ \___|_|\_\_| |_|\__,_|_| |_|\__,_|
`

// newRoot builds the tree along with the run it belongs to. Execute
// needs both — the run has to be closed however the command ends.
func newRoot(version string) (*cobra.Command, *runstate.Context) {
	// The composition root: the one tool finder — over the real PATH
	// search — and the two external-service seams built over it are
	// wired here and only here. Tests build a Context with fakes
	// instead of mutating package state.
	tools := tool.NewFinder(nil)
	rc := &runstate.Context{
		Tools:    tools,
		Version:  version,
		Verifier: realVMProvider(tools),
		Lister:   realWorkerLister(tools),
		// The agent marker is process state, so it is read here and
		// nowhere below: an engine that read its own environment would be
		// deciding provenance rather than being told it.
		Agent: os.Getenv(agentEnv),
		// The build's answer about unattended publication, spent here and
		// only here. machinepublish.go says what it is and why it is a
		// constant; from here it reaches the engine through Deps, and it
		// reaches the forge through the guarded runner below.
		MachinePublish: machinePublishEnabled,
	}
	// The gh seam, wrapped so that a machine's WRITE to the forge is
	// refused underneath every path that could assemble one. The engine
	// gates its own two funnels as well; this is the layer that holds when
	// a new one is written by somebody who never read them.
	rc.Gh = guardForgeWrites(rc, gh.RealGhOut(tools))
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
			// Who is running this, resolved once and before any Action —
			// which is what makes it one invocation's answer rather than
			// each verb's. auto.go says how it is declared and why nothing
			// here detects it.
			invoker, err := resolveInvoker(c)
			if err != nil {
				return err
			}
			// The logger is configured here, before the run is built:
			// Init's tree search speaks through it, so --debug can say
			// which tree was found. It belongs to the command layer
			// because --debug is a flag, and the run holds facilities,
			// not process-wide settings.
			level := slog.LevelWarn
			if debug {
				level = slog.LevelDebug
			}
			slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level})))
			rc.Invoker = invoker
			rc.Init(treeRoot, prefixPath, debug, c.OutOrStdout(), c.ErrOrStderr())
			c.SetContext(runstate.Into(c.Context(), rc))
			return nil
		},
	}
	root.SetErrPrefix("dockhand:")
	root.SetVersionTemplate("dockhand {{.Version}}\n")
	// Pre-defining the version flag gives it the -V shorthand; cobra
	// only adds its own (shorthand-less) flag when none exists.
	root.Flags().BoolP("version", "V", false, "print the version")

	// Global flags. Every command that talks to a MacPorts installation
	// or a ports tree resolves them through these, so a command needing
	// either inherits it rather than declaring its own.

	root.PersistentFlags().StringP("prefix", "p", os.Getenv("DOCKHAND_PREFIX"),
		"MacPorts installation prefix (default $DOCKHAND_PREFIX, else discovered)")

	root.PersistentFlags().StringP("tree", "t", os.Getenv("DOCKHAND_TREE"),
		"ports tree root (default $DOCKHAND_TREE, else the tree the working directory is in)")

	root.PersistentFlags().Bool("debug", false,
		"print debug output to stderr")

	// Auto mode is declared here rather than detected anywhere: the flag
	// is persistent because who is running an invocation is a fact about
	// the invocation and not about the verb. Its default is false and the
	// environment is consulted only where the command line said nothing,
	// so --auto=false withdraws a standing DOCKHAND_AUTO for one run.
	root.PersistentFlags().Bool(autoFlag, false,
		"run as the machine rather than as a person, for cron and launchd (default $"+autoEnv+")")

	// Flag-parse failures are usage errors; cobra's own are untyped.
	root.SetFlagErrorFunc(func(_ *cobra.Command, err error) error {
		return &UsageError{Err: err}
	})
	// The verbs are grouped by family, and the families are the
	// architecture's own: intents change a port, the lifecycle verbs
	// work the branches an intent minted, the environment verbs build
	// and reach into the VMs verification runs in, and the reports read
	// without writing. Registration order is display order — the help
	// reads as the workflow, so the workflow decides the order, not the
	// alphabet.
	cobra.EnableCommandSorting = false
	add := func(group string, title string, cmds ...*cobra.Command) {
		root.AddGroup(&cobra.Group{ID: group, Title: title})
		for _, c := range cmds {
			c.GroupID = group
			root.AddCommand(c)
		}
	}
	add("intent", "Change a port:", intentCommands()...)
	// dismiss sits with the verbs that read a verification's answer,
	// because that is what it answers: a proposal is something a
	// settlement found, and saying no to one is the other half of the
	// cohort verb that says yes.
	add("test", "Test the port:", Verify(), Status(), Cancel(), Dismiss())
	add("submit", "Submit the port:", Promote())
	add("env", "Troubleshoot the port:", Log(), Shell())
	// auto sits with the housekeeping verbs and after clean, because it
	// is the pass those two verbs are halves of, run by nobody.
	//
	// hold and unhold sit at the front of the same group, ahead of the
	// verbs that remove things. They are the brake on every road in the
	// tool — publication, verification, retirement — and a reader
	// scanning this group for "how do I stop it" should meet them before
	// they meet `discard`, which is how the question gets answered by
	// deleting the work instead.
	add("branch", "Housekeeping:", Hold(), Unhold(), Discard(), Clean(), Auto(), Provision())
	add("report", "Reports:", Outdated(), Classify(), Doctor())
	root.AddCommand(Exec(), versionCmd())
	return root, rc
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

// Execute runs the dockhand command tree against os.Args and returns
// the process exit code.
//
// An interrupt cancels the run's context rather than killing the
// process, so the deferred cleanup actually runs: without it a Ctrl-C
// mid-fetch left the run's temporary files — shadowed portdirs, downloaded
// distfiles — behind with nothing to attribute it to. A second signal
// still kills outright, which is what a user pressing it twice means.
func Execute(version string) int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return execute(ctx, version, os.Args[1:], os.Stdout, os.Stderr)
}

func execute(ctx context.Context, version string, args []string, out, errOut io.Writer) int {
	root, rc := newRoot(version)
	// However the command ends — success, failure, interrupt — the run
	// gives back what it took.
	defer rc.Close()
	root.SetOut(out)
	root.SetErr(errOut)
	root.SetArgs(args)
	// Unknown commands are usage errors, but cobra detects them inside
	// Execute with an untyped error; pre-flighting Find keeps the
	// classification identity-based. The default subcommands must exist
	// before Find or "dockhand help"/"dockhand completion" would
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
