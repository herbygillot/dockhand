package command

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/herbygillot/dockhand/internal/config"
	"github.com/herbygillot/dockhand/internal/engine"
	"github.com/herbygillot/dockhand/internal/provider"
	"github.com/herbygillot/dockhand/internal/provider/actions"
	"github.com/herbygillot/dockhand/internal/provider/script"
	"github.com/herbygillot/dockhand/internal/provider/tart"
)

// settings are the global selections. Each comes from its flag, then its
// environment variable, then the configuration file, then its default.
type settings struct {
	tree     string
	database string
	git      string
	// file is the configuration file, as read when the engine opened.
	file config.File
}

func (s *settings) flags(root *cobra.Command) {
	root.PersistentFlags().StringVarP(&s.tree, "tree", "t", "", "the ports checkout (default $MACPORTS_TREE, else the current directory)")
	root.PersistentFlags().StringVar(&s.database, "db", "", "the database (default $DOCKHAND_DB, else ~/.dockhand/dockhand.db)")
	root.PersistentFlags().StringVar(&s.git, "git", "", "the git executable (default $GIT_BIN, else git on PATH)")
}

func firstOf(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

// options resolves the engine's options, reading the configuration file.
func (s *settings) options() (engine.Options, config.File, string, error) {
	configPath, err := config.Path()
	if err != nil {
		return engine.Options{}, config.File{}, "", err
	}
	file, err := config.Load(configPath)
	if err != nil {
		return engine.Options{}, config.File{}, "", err
	}
	database := firstOf(s.database, os.Getenv("DOCKHAND_DB"))
	if database == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return engine.Options{}, config.File{}, "", err
		}
		database = filepath.Join(home, ".dockhand", "dockhand.db")
	}
	return engine.Options{
		Tree:      firstOf(s.tree, os.Getenv("MACPORTS_TREE"), "."),
		Git:       firstOf(s.git, os.Getenv("GIT_BIN")),
		Database:  database,
		Worktrees: file.Worktrees,
		// DOCKHAND_UPSTREAM fetches master from a mirror or, in tests, a
		// local repository.
		Upstream: os.Getenv("DOCKHAND_UPSTREAM"),
		Tclsh:    portTclsh(),
	}, file, configPath, nil
}

func (s *settings) open(ctx context.Context) (*engine.Engine, error) {
	options, file, _, err := s.options()
	if err != nil {
		return nil, err
	}
	e, err := engine.Open(ctx, options)
	if err != nil {
		return nil, err
	}
	s.file = file
	e.Providers = map[string]provider.Provider{}
	if command := file.Providers.Command; command != nil {
		e.Providers["command"] = &script.Provider{Run: command.Run, Label: command.Name, Repo: e.Repo}
	}
	remote := file.Providers.GitHub.Remote
	github := &actions.Provider{Repo: e.Repo, Fork: func(ctx context.Context) (provider.Fork, error) { return e.Fork(ctx, remote) },
		API: actions.GitHub{Client: authAPI(authStore)}}
	if testActions != nil {
		github.API, github.Sleep = testActions, func(ctx context.Context, _ time.Duration) error { return ctx.Err() }
	}
	e.Providers["github"] = github
	// Tart builds wherever Tart is installed; its images are checked when a
	// check names a release.
	if _, err := lookTart("tart"); err == nil || testTart != nil {
		provider := &tart.Provider{Repo: e.Repo, Index: e.PortIndex, TestTimeout: file.Providers.Tart.Timeout()}
		if testTart != nil {
			testTart(provider)
		}
		e.Providers["tart"] = provider
	}
	if testLeftovers != nil {
		e.Providers[testLeftovers.Name()] = testLeftovers
	}
	if testPreparer != nil {
		e.Preparer = testPreparer(e)
	}
	if testForge != nil {
		e.Forge = testForge(e)
	}
	if testPortReader != nil {
		e.PortReader = testPortReader
	}
	if testDependentReader != nil {
		e.DependentReader = testDependentReader
	}
	if testOutdatedReader != nil {
		e.OutdatedReader = testOutdatedReader
	}
	if testArchiveFetcher != nil {
		e.ArchiveFetcher = testArchiveFetcher(e)
	}
	return e, nil
}

// testArchiveFetcher, when set, stands in for fetching archives.
var testArchiveFetcher func(*engine.Engine) engine.ArchiveFetcher

// testActions, when set, stands in for GitHub Actions.
var testActions actions.API

// testLeftovers, when set, is a provider with environments left behind,
// registered under its name in place of whichever the configuration made.
var testLeftovers provider.LeftoverProvider

// testTart, when set, registers the Tart provider whether or not Tart is
// installed, and adjusts it for a test.
var testTart func(*tart.Provider)

// lookTart finds Tart; the tests find none, so they don't depend on the
// machine they run on.
var lookTart = exec.LookPath

// testPortReader, when set, stands in for MacPorts' evaluator in plans.
var testPortReader engine.PortReader

// testDependentReader, when set, stands in for the port index in impact.
var testDependentReader engine.DependentReader

// testOutdatedReader, when set, stands in for upstream discovery.
var testOutdatedReader engine.OutdatedReader

// tilde abbreviates the home directory in a path shown to a person.
func tilde(path string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return path
	}
	// Git reports resolved paths, so a home reached through a symbolic
	// link is matched in its resolved form too.
	homes := []string{home}
	if resolved, err := filepath.EvalSymlinks(home); err == nil && resolved != home {
		homes = append(homes, resolved)
	}
	for _, home := range homes {
		if path == home {
			return "~"
		}
		if rest, ok := strings.CutPrefix(path, home+string(filepath.Separator)); ok {
			return "~/" + rest
		}
	}
	return path
}

func homeDir() (string, error) { return os.UserHomeDir() }
