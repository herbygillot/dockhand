// Package cmd holds the dockhand command tree: one public constructor
// per subcommand, assembled under Root, with cmd/dockhand as a thin
// entry point. Cobra parses through pflag, so the full POSIX/GNU flag
// surface (clustered shorts with glued values, interspersed flags)
// comes with it. The package also owns the exit-code table (exit.go):
// failures are classified by error identity, never by message text.
package cmd

import (
	"context"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/herbygillot/dockhand/internal/runstate"
)

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
	rc := &runstate.Context{}
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
			rc.Init(treeRoot, prefixPath, debug, c.InOrStdin(), c.OutOrStdout(), c.ErrOrStderr())
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

	// Flag-parse failures are usage errors; cobra's own are untyped.
	root.SetFlagErrorFunc(func(_ *cobra.Command, err error) error {
		return &UsageError{Err: err}
	})
	root.AddCommand(Bump(), BumpRevisionCmd(), Classify(), Clean(), Discard(), Doctor(), Log(), Promote(), Provision(), RefreshChecksums(), Shell(), Status(), Verify(), versionCmd())
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
		return ExitUsage
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
