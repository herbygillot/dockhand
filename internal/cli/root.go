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
	if config.LockDir == "" {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("cli: locating home directory for default lock directory: %w", err)
		}
		config.LockDir = filepath.Join(homeDir, ".dockhand", "lock")
	}
	runtime := &runtime{config: config}
	root := &cobra.Command{
		Use:           "dockhand",
		Short:         "Maintain MacPorts ports",
		Long:          "Dockhand prepares, verifies, and publishes MacPorts changes.\nStatus reporting is available. Action commands are under construction.",
		Args:          cobra.NoArgs,
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE:          func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
	}
	lockDir := lockDirValue{target: &runtime.config.LockDir}
	if err := lockDir.Set(runtime.config.LockDir); err != nil {
		return nil, fmt.Errorf("cli: resolving lock directory: %w", err)
	}
	root.PersistentFlags().VarP(lockDir, "lock-dir", "L", "Directory for Dockhand resource locks")
	root.PersistentFlags().BoolVar(&runtime.json, "json", false, "Output command results as JSON")
	if err := root.MarkPersistentFlagDirname("lock-dir"); err != nil {
		return nil, err
	}

	root.AddCommand(runtime.setupCommand())
	root.AddCommand(runtime.changeCommands()...)
	root.AddCommand(runtime.verifyCommand(), runtime.publishCommand())
	wait, _ := runtime.workflowCommand("wait <target>", "Wait for existing work to finish", cobra.ExactArgs(1))
	cancel, _ := runtime.workflowCommand("cancel <target>", "Request cancellation of outstanding work", cobra.ExactArgs(1))
	start, _ := runtime.workflowCommand("start", "Run the persistent driver in this process", cobra.NoArgs)
	root.AddCommand(runtime.statusCommand(), wait, cancel, start, runtime.reviewCommand())

	root.InitDefaultHelpCmd()
	for _, command := range root.Commands() {
		if command.Name() == "help" {
			command.Aliases = append(command.Aliases, "usage")
		}
	}
	help := root.HelpFunc()
	root.SetHelpFunc(func(cmd *cobra.Command, args []string) {
		help(cmd, args)
		fmt.Fprintf(cmd.OutOrStdout(), "\nLock directory: %s\n", runtime.config.LockDir)
	})
	return root, nil
}
