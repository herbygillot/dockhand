package command

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/herbygillot/dockhand/internal/buildenv"
	"github.com/herbygillot/dockhand/internal/buildenv/ghactions"
	"github.com/herbygillot/dockhand/internal/buildenv/script"
	"github.com/herbygillot/dockhand/internal/buildenv/tart"
	"github.com/herbygillot/dockhand/internal/config"
	"github.com/herbygillot/dockhand/internal/engine"
)

// settings are the global selections. Each comes from its flag, then its
// environment variable, then its default.
type settings struct {
	tree     string
	database string
	git      string
	// file is the configuration file, as read when the engine opened.
	file config.File
	// opened is the engine a command opened, and openedWith what it opened
	// with, for the cleanup that follows the command's work.
	opened     *engine.Engine
	openedWith engine.Options
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
func (s *settings) options(ctx context.Context) (engine.Options, config.File, string, error) {
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
	// With no --tree, MACPORTS_TREE names the checkout, and a directory in
	// one of its worktrees is that worktree, whose branch is the one
	// checked out here.
	tree := s.tree
	if tree == "" {
		tree = "."
		if named := os.Getenv("MACPORTS_TREE"); named != "" {
			tree = engine.Here(ctx, named, ".", firstOf(s.git, os.Getenv("GIT_BIN")))
		}
	}
	return engine.Options{
		Tree:      tree,
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
	options, file, _, err := s.options(ctx)
	if err != nil {
		return nil, err
	}
	e, err := engine.Open(ctx, options)
	if err != nil {
		return nil, err
	}
	s.file, s.opened, s.openedWith = file, e, options
	e.Providers = map[string]buildenv.Provider{}
	if command := file.Providers.Command; command != nil {
		e.Providers[buildenv.Command] = &script.Provider{Run: command.Run, Label: command.Name, Repo: e.Repo}
	}
	remote := file.Providers.GitHub.Remote
	github := &ghactions.Provider{Repo: e.Repo, Fork: func(ctx context.Context) (buildenv.Fork, error) { return e.Fork(ctx, remote) },
		API: ghactions.GitHub{Client: authAPI(authStore)}}
	if testActions != nil {
		github.API, github.Sleep = testActions, func(ctx context.Context, _ time.Duration) error { return ctx.Err() }
	}
	e.Providers[buildenv.GitHub] = github
	// Tart builds wherever Tart is installed; its images are checked when a
	// check names a release.
	if _, err := lookTart("tart"); err == nil || testTart != nil {
		provider := &tart.Provider{Repo: e.Repo, Index: e.PortIndex, TestTimeout: file.Providers.Tart.Timeout()}
		if testTart != nil {
			testTart(provider)
		}
		e.Providers[buildenv.Tart] = provider
	}
	if testLeftovers != nil {
		e.Providers[testLeftovers.Name()] = testLeftovers
	}
	if testPreparer != nil {
		e.Preparer = testPreparer(e)
	}
	e.GitHubCLI = gitHubCLI
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
var testActions ghactions.API

// testLeftovers, when set, is a provider with environments left behind,
// registered under its name in place of whichever the configuration made.
var testLeftovers buildenv.LeftoverProvider

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
