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
	if config.DBPath == "" {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("cli: locating home directory for default state database: %w", err)
		}
		config.DBPath = filepath.Join(homeDir, ".dockhand", "state.db")
	}
	runtime := &runtime{config: config}
	root := &cobra.Command{
		Use:           "dockhand",
		Short:         "Maintain MacPorts ports",
		Long:          "Dockhand prepares, verifies, and publishes MacPorts changes.\nVerify committed ports, resume jobs, and run driver cycles. Preparation and publication are under construction.",
		Args:          cobra.NoArgs,
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE:          func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
	}
	dbPath := dbPathValue{target: &runtime.config.DBPath}
	if err := dbPath.Set(runtime.config.DBPath); err != nil {
		return nil, fmt.Errorf("cli: resolving state database: %w", err)
	}
	root.PersistentFlags().Var(dbPath, "db", "Path to the Dockhand state database")
	root.PersistentFlags().BoolVar(&runtime.json, "json", false, "Output command results as JSON")
	if err := root.MarkPersistentFlagFilename("db"); err != nil {
		return nil, err
	}

	root.AddCommand(runtime.setupCommand())
	root.AddCommand(runtime.changeCommands()...)
	root.AddCommand(runtime.verifyCommand(), runtime.publishCommand())
	root.AddCommand(runtime.statusCommand(), runtime.waitCommand(), runtime.cancelCommand(), runtime.startCommand(), runtime.reviewCommand())

	root.InitDefaultHelpCmd()
	for _, command := range root.Commands() {
		if command.Name() == "help" {
			command.Aliases = append(command.Aliases, "usage")
		}
	}
	help := root.HelpFunc()
	root.SetHelpFunc(func(cmd *cobra.Command, args []string) {
		help(cmd, args)
		fmt.Fprintf(cmd.OutOrStdout(), "\nState database: %s\n", runtime.config.DBPath)
	})
	return root, nil
}
