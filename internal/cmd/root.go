// Package cmd holds the dockhand command tree: one public constructor
// per subcommand, assembled under Root, with cmd/dockhand as a thin
// entry point. Cobra parses through pflag, so the full POSIX/GNU flag
// surface (clustered shorts with glued values, interspersed flags)
// comes with it. The package also owns the exit-code table (exit.go):
// failures are classified by error identity, never by message text.
package cmd

import (
	"os"

	"github.com/spf13/cobra"
)

// Root builds the dockhand command tree.
func Root(version string) *cobra.Command {
	root := &cobra.Command{
		Use:          "dockhand",
		Short:        "A port maintenance utility for MacPorts",
		Version:      version,
		SilenceUsage: true,
	}
	root.SetErrPrefix("dockhand:")
	root.SetVersionTemplate("dockhand {{.Version}}\n")
	// Pre-defining the version flag gives it the -V shorthand; cobra
	// only adds its own (shorthand-less) flag when none exists.
	root.Flags().BoolP("version", "V", false, "print the version")
	// Globals: every command that talks to a MacPorts installation or a
	// ports tree resolves them through these flags.
	root.PersistentFlags().StringP("prefix", "p", os.Getenv("DOCKHAND_PREFIX"),
		"MacPorts installation prefix (default $DOCKHAND_PREFIX, else discovered)")
	root.PersistentFlags().StringP("tree", "t", os.Getenv("DOCKHAND_TREE"),
		"ports tree root (default $DOCKHAND_TREE)")
	// Flag-parse failures are usage errors; cobra's own are untyped.
	root.SetFlagErrorFunc(func(_ *cobra.Command, err error) error {
		return &UsageError{Err: err}
	})
	root.AddCommand(Apply(), Bump(), Classify(), Doctor(), versionCmd())
	return root
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
func Execute(version string) int {
	return execute(Root(version), os.Args[1:])
}

func execute(root *cobra.Command, args []string) int {
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
	return ExitCode(root.Execute())
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
