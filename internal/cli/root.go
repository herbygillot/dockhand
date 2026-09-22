package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/herbygillot/dockhand/internal/app"
	"github.com/herbygillot/dockhand/internal/credential"
	"github.com/herbygillot/dockhand/internal/github"
	"github.com/spf13/cobra"
)

const stateIndependentHelp = "dockhand.state-independent"

type serviceBuilder func(context.Context, app.Config) (*app.Services, error)

type runtime struct {
	// outcome is the command's JSON result, written once inside the envelope after execution.
	outcome      any
	verbosity    int
	debug        bool
	timestamps   bool
	build        serviceBuilder
	statusGitHub func(context.Context, *github.Client) (app.GitHubAuthStatus, error)
	logoutGitHub func(context.Context, credential.Remover) (app.GitHubLogoutResult, error)
	config       app.Config
	json         bool
	loginGitHub  func(context.Context, app.GitHubLoginOptions) (app.GitHubLoginResult, error)
}

// logo opens the main help message, as it did in the first dockhand, with
// the build's version beneath it. The trailing spaces on its lines are the
// art's own.
const logo = `     _            _    _                     _
  __| | ___   ___| | _| |__   __ _ _ __   __| |
 / _` + "`" + ` |/ _ \ / __| |/ / '_ \ / _` + "`" + ` | '_ \ / _` + "`" + ` |
| (_| | (_) | (__|   <| | | | (_| | | | | (_| |
 \__,_|\___/ \___|_|\_\_| |_|\__,_|_| |_|\__,_|
`

func NewRoot(config app.Config) (*cobra.Command, error) {
	root, _, err := newRoot(config, app.Build)
	return root, err
}

func newRoot(config app.Config, build serviceBuilder) (*cobra.Command, *runtime, error) {
	if config.DependencyTools.Go2Port == "" {
		config.DependencyTools.Go2Port = os.Getenv("GO2PORT_BIN")
	}
	if config.DependencyTools.Cargo2Port == "" {
		config.DependencyTools.Cargo2Port = os.Getenv("CARGO2PORT_BIN")
	}
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
		config.DBPath = os.Getenv("DOCKHAND_DB")
	}
	if config.DBPath == "" {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return nil, nil, fmt.Errorf("cli: locating home directory for default state database: %w", err)
		}
		config.DBPath = filepath.Join(homeDir, ".dockhand", "state.db")
	}
	runtime := &runtime{build: build, config: config, loginGitHub: app.LoginGitHub, statusGitHub: app.StatusGitHub, logoutGitHub: app.LogoutGitHub}
	root := &cobra.Command{
		Use:           "dockhand",
		Version:       buildVersion(),
		Short:         "Maintain MacPorts ports",
		Long:          logo + "version " + buildVersion() + "\n\nDockhand prepares, verifies, and publishes MacPorts changes.\nPrepare version and revision bumps, verify committed ports, resume jobs, and run driver cycles. Automatic discovery supports GitHub/GitLab catalogs and supported MacPorts livechecks. Publish verified contribution branches to GitHub.",
		Args:          cobra.NoArgs,
		SilenceErrors: true,
		PersistentPreRun: func(cmd *cobra.Command, _ []string) {
			if runtime.timestamps && !runtime.json {
				// Every line the command writes to stderr, progress and
				// its own, carries the time it was printed; JSON reports
				// carry it as a field instead, so their lines stay pure.
				cmd.SetErr(&stampedWriter{w: cmd.ErrOrStderr()})
			}
			cmd.SetContext(progressContext(cmd.Context(), cmd.ErrOrStderr(), runtime.level(cmd), runtime.json, runtime.timestamps))
		},
		SilenceUsage: true,
		RunE:         func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
	}
	dbPath := dbPathValue{target: &runtime.config.DBPath}
	if err := dbPath.Set(runtime.config.DBPath); err != nil {
		return nil, nil, fmt.Errorf("cli: resolving state database: %w", err)
	}
	root.PersistentFlags().Var(dbPath, "db", "Path to the Dockhand state database (DOCKHAND_DB; otherwise ~/.dockhand/state.db)")
	for _, flag := range []struct {
		name, shorthand, usage string
		target                 *string
	}{
		{"tree", "t", "Ports tree directory (MACPORTS_TREE; defaults to the current directory)", &runtime.config.Repository},
		{"prefix", "p", "Local MacPorts installation prefix (MACPORTS_PREFIX; otherwise find port-tclsh on PATH)", &runtime.config.MacPortsPrefix},
	} {
		value := directoryPathValue{target: flag.target}
		if *flag.target != "" {
			if err := value.Set(*flag.target); err != nil {
				return nil, nil, fmt.Errorf("cli: resolving --%s: %w", flag.name, err)
			}
		}
		root.PersistentFlags().VarP(value, flag.name, flag.shorthand, flag.usage)
		if err := root.MarkPersistentFlagDirname(flag.name); err != nil {
			return nil, nil, err
		}
	}
	gitPath := executablePathValue{target: &runtime.config.GitExecutable}
	if runtime.config.GitExecutable != "" {
		if err := gitPath.Set(runtime.config.GitExecutable); err != nil {
			return nil, nil, fmt.Errorf("cli: resolving --git: %w", err)
		}
	}
	root.PersistentFlags().Var(gitPath, "git", "Git executable (GIT_BIN; otherwise find git on PATH)")
	tartPath := executablePathValue{target: &runtime.config.Tart.Executable}
	if runtime.config.Tart.Executable != "" {
		if err := tartPath.Set(runtime.config.Tart.Executable); err != nil {
			return nil, nil, fmt.Errorf("cli: resolving --tart: %w", err)
		}
	}
	root.PersistentFlags().Var(tartPath, "tart", "Tart executable (TART_BIN; otherwise find tart on PATH)")
	for _, tool := range []struct {
		name, env string
		target    *string
	}{{"go2port", "GO2PORT_BIN", &runtime.config.DependencyTools.Go2Port}, {"cargo2port", "CARGO2PORT_BIN", &runtime.config.DependencyTools.Cargo2Port}} {
		value := executablePathValue{target: tool.target}
		if *tool.target != "" {
			if err := value.Set(*tool.target); err != nil {
				return nil, nil, err
			}
		}
		root.PersistentFlags().Var(value, tool.name, "Optional dependency generator executable ("+tool.env+")")
		if err := root.MarkPersistentFlagFilename(tool.name); err != nil {
			return nil, nil, err
		}
	}
	root.PersistentFlags().BoolVar(&runtime.json, "json", false, "Output command results as JSON")
	root.PersistentFlags().CountVarP(&runtime.verbosity, "verbose", "v", "Show identifiers and the work behind the scenes; -vv shows every sub-operation")
	root.PersistentFlags().BoolVar(&runtime.debug, "debug", false, "Show every sub-operation (same as -vv)")
	root.PersistentFlags().BoolVar(&runtime.timestamps, "timestamps", false, "Prefix each progress line with the time it was printed; JSON reports carry it as time")
	if err := root.MarkPersistentFlagFilename("db"); err != nil {
		return nil, nil, err
	}
	if err := root.MarkPersistentFlagFilename("git"); err != nil {
		return nil, nil, err
	}
	if err := root.MarkPersistentFlagFilename("tart"); err != nil {
		return nil, nil, err
	}

	root.AddCommand(runtime.setupCommand(), runtime.databaseCommand(), runtime.gcCommand())
	root.AddCommand(runtime.authCommand(), runtime.outdatedCommand(), runtime.assessCommand())
	root.AddCommand(runtime.changeCommands()...)
	root.AddCommand(runtime.correctionCommands()...)
	root.AddCommand(runtime.contributionCommands()...)
	root.AddCommand(runtime.adoptCommand())
	root.AddCommand(runtime.verifyCommand(), runtime.publishCommand())
	root.AddCommand(runtime.statusCommand(), runtime.consoleCommand(), runtime.waitCommand(), runtime.cancelCommand(), runtime.serveCommand(), runtime.reviewCommand())
	groupCommands(root)

	root.InitDefaultHelpCmd()
	for _, command := range root.Commands() {
		if command.Name() == "help" {
			command.Aliases = append(command.Aliases, "usage")
		}
	}
	section(root.PersistentFlags(), sectionPaths, "tree", "prefix", "db", "git", "tart", "go2port", "cargo2port")
	section(root.PersistentFlags(), sectionOutput, "json", "verbose", "debug", "timestamps")
	registerTemplateFuncs()
	root.SetUsageTemplate(usageTemplate)
	help := root.HelpFunc()
	root.SetHelpFunc(func(cmd *cobra.Command, args []string) {
		help(cmd, args)
		if cmd.Annotations[stateIndependentHelp] != "true" {
			fmt.Fprintf(cmd.OutOrStdout(), "\nState database: %s\n", runtime.config.DBPath)
		}
	})
	return root, runtime, nil
}

// helpGroups orders the help screen as the life of a contribution: prepare the
// machine, look at ports, prepare and revise an update, prove and publish it,
// follow the accepted work, and clean up afterwards. Commands outside these
// groups, such as the unimplemented review command, stay under Cobra's
// "Additional Commands" heading.
var helpGroups = []struct {
	id, title string
	commands  []string
}{
	{"start", "Get started:", []string{"setup", "auth"}},
	{"investigate", "Investigate ports:", []string{"outdated", "assess"}},
	{"prepare", "Prepare an update:", []string{"bump", "bump-revision", "checksums", "adopt"}},
	{"revise", "Revise your update:", []string{"amend", "rebase", "reassociate"}},
	{"publish", "Verify and publish:", []string{"verify", "publish"}},
	{"jobs", "Watch and manage jobs:", []string{"status", "console", "wait", "cancel", "serve", "sync", "abandon"}},
	{"housekeeping", "Housekeeping:", []string{"gc", "db"}},
	{"planned", "Planned, not implemented yet:", []string{"review"}},
}

// Help lists commands in group order, never alphabetically. The setting is a
// cobra package global, so it is made once here rather than on every root,
// where concurrent constructions would race on it.
func init() { cobra.EnableCommandSorting = false }

func groupCommands(root *cobra.Command) {
	commands := root.Commands()
	byName := make(map[string]*cobra.Command, len(commands))
	for _, command := range commands {
		byName[command.Name()] = command
	}
	// Re-register commands in group order so help lists each group's commands
	// in the order a contributor uses them rather than alphabetically.
	root.RemoveCommand(commands...)
	grouped := make(map[string]bool, len(commands))
	for _, group := range helpGroups {
		root.AddGroup(&cobra.Group{ID: group.id, Title: group.title})
		for _, name := range group.commands {
			command, ok := byName[name]
			if !ok {
				continue
			}
			command.GroupID = group.id
			grouped[name] = true
			root.AddCommand(command)
		}
	}
	for _, command := range commands {
		if !grouped[command.Name()] {
			root.AddCommand(command)
		}
	}
}

// registerTemplateFuncs adds the usage template's functions to cobra's
// package-wide table once. The table is a plain map, and a root is built
// per command and per test, in parallel under the test runner, so adding
// on every construction raced and the runtime stopped the binary.
var templateFuncs sync.Once

func registerTemplateFuncs() {
	templateFuncs.Do(func() { cobra.AddTemplateFunc("flagSections", flagSections) })
}
