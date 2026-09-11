package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/herbygillot/dockhand/v2/internal/app"
	"github.com/spf13/cobra"
)

type runtime struct {
	config app.Config
	json   bool
}

func NewRoot(config app.Config) (*cobra.Command, error) {
	if config.Lockfile == "" {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("cli: locating home directory for default lockfile: %w", err)
		}
		config.Lockfile = filepath.Join(homeDir, ".dockhand", "ledger.lock")
	}
	runtime := &runtime{config: config}
	root := &cobra.Command{
		Use:           "dockhand",
		Short:         "Maintain MacPorts ports",
		Long:          "Dockhand prepares, verifies, and publishes MacPorts changes.\nWorkflow commands are under construction and currently return not-implemented errors.",
		Args:          cobra.NoArgs,
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE:          func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
	}
	lockfile := lockfileValue{target: &runtime.config.Lockfile}
	if err := lockfile.Set(runtime.config.Lockfile); err != nil {
		return nil, fmt.Errorf("cli: resolving ledger lockfile: %w", err)
	}
	root.PersistentFlags().VarP(lockfile, "lockfile", "L", "Ledger writer lockfile")
	root.PersistentFlags().BoolVar(&runtime.json, "json", false, "Output command results as JSON")
	if err := root.MarkPersistentFlagFilename("lockfile"); err != nil {
		return nil, err
	}

	root.AddCommand(runtime.setupCommand())
	root.AddCommand(runtime.changeCommands()...)
	root.AddCommand(runtime.verifyCommand(), runtime.publishCommand())
	status, _ := runtime.workflowCommand("status", "Show recorded workflow status", cobra.NoArgs)
	wait, _ := runtime.workflowCommand("wait <target>", "Wait for existing work to finish", cobra.ExactArgs(1))
	cancel, _ := runtime.workflowCommand("cancel <target>", "Request cancellation of outstanding work", cobra.ExactArgs(1))
	start, _ := runtime.workflowCommand("start", "Run the persistent driver in this process", cobra.NoArgs)
	root.AddCommand(status, wait, cancel, start, runtime.reviewCommand())

	root.InitDefaultHelpCmd()
	for _, command := range root.Commands() {
		if command.Name() == "help" {
			command.Aliases = append(command.Aliases, "usage")
		}
	}
	help := root.HelpFunc()
	root.SetHelpFunc(func(cmd *cobra.Command, args []string) {
		help(cmd, args)
		fmt.Fprintf(cmd.OutOrStdout(), "\nLedger lockfile: %s\n", runtime.config.Lockfile)
	})
	return root, nil
}
