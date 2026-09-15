package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/herbygillot/dockhand/internal/app"
	"github.com/herbygillot/dockhand/internal/credential"
	"github.com/herbygillot/dockhand/internal/forge/github"
	"github.com/spf13/cobra"
)

const stateIndependentHelp = "dockhand.state-independent"

type runtime struct {
	statusGitHub func(context.Context, *github.Client) (app.GitHubAuthStatus, error)
	logoutGitHub func(context.Context, credential.Remover) (app.GitHubLogoutResult, error)
	config       app.Config
	json         bool
	loginGitHub  func(context.Context, app.GitHubLoginOptions) (app.GitHubLoginResult, error)
}

func NewRoot(config app.Config) (*cobra.Command, error) {
	if config.Repository == "" {
		config.Repository = os.Getenv("MACPORTS_TREE")
		if config.Repository == "" {
			config.Repository = "."
		}
	}
	if config.MacPortsPrefix == "" {
		config.MacPortsPrefix = os.Getenv("MACPORTS_PREFIX")
	}
	if config.GitExecutable == "" {
		config.GitExecutable = os.Getenv("GIT_BIN")
	}
	if config.Tart.Executable == "" {
		config.Tart.Executable = os.Getenv("TART_BIN")
	}
	if config.DBPath == "" {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("cli: locating home directory for default state database: %w", err)
		}
		config.DBPath = filepath.Join(homeDir, ".dockhand", "state.db")
	}
	runtime := &runtime{config: config, loginGitHub: app.LoginGitHub, statusGitHub: app.StatusGitHub, logoutGitHub: app.LogoutGitHub}
	root := &cobra.Command{
		Use:           "dockhand",
		Short:         "Maintain MacPorts ports",
		Long:          "Dockhand prepares, verifies, and publishes MacPorts changes.\nPrepare version and revision bumps, verify committed ports, resume jobs, and run driver cycles. Automatic selection supports GitHub sources with stable numeric versions and a tags livecheck. Publish verified contribution branches to GitHub.",
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
	for _, flag := range []struct {
		name, shorthand, usage string
		target                 *string
	}{
		{"tree", "T", "Ports tree directory (MACPORTS_TREE; defaults to the current directory)", &runtime.config.Repository},
		{"prefix", "P", "Local MacPorts installation prefix (MACPORTS_PREFIX; otherwise find port-tclsh on PATH)", &runtime.config.MacPortsPrefix},
	} {
		value := directoryPathValue{target: flag.target}
		if *flag.target != "" {
			if err := value.Set(*flag.target); err != nil {
				return nil, fmt.Errorf("cli: resolving --%s: %w", flag.name, err)
			}
		}
		root.PersistentFlags().VarP(value, flag.name, flag.shorthand, flag.usage)
		if err := root.MarkPersistentFlagDirname(flag.name); err != nil {
			return nil, err
		}
	}
	gitPath := executablePathValue{target: &runtime.config.GitExecutable}
	if runtime.config.GitExecutable != "" {
		if err := gitPath.Set(runtime.config.GitExecutable); err != nil {
			return nil, fmt.Errorf("cli: resolving --git: %w", err)
		}
	}
	root.PersistentFlags().Var(gitPath, "git", "Git executable (GIT_BIN; otherwise find git on PATH)")
	tartPath := executablePathValue{target: &runtime.config.Tart.Executable}
	if runtime.config.Tart.Executable != "" {
		if err := tartPath.Set(runtime.config.Tart.Executable); err != nil {
			return nil, fmt.Errorf("cli: resolving --tart: %w", err)
		}
	}
	root.PersistentFlags().Var(tartPath, "tart", "Tart executable (TART_BIN; otherwise find tart on PATH)")
	root.PersistentFlags().BoolVar(&runtime.json, "json", false, "Output command results as JSON")
	if err := root.MarkPersistentFlagFilename("db"); err != nil {
		return nil, err
	}
	if err := root.MarkPersistentFlagFilename("git"); err != nil {
		return nil, err
	}
	if err := root.MarkPersistentFlagFilename("tart"); err != nil {
		return nil, err
	}

	root.AddCommand(runtime.setupCommand(), runtime.databaseCommand(), runtime.gcCommand())
	root.AddCommand(runtime.authCommand())
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
		if cmd.Annotations[stateIndependentHelp] != "true" {
			fmt.Fprintf(cmd.OutOrStdout(), "\nState database: %s\n", runtime.config.DBPath)
		}
	})
	return root, nil
}
