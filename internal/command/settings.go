package command

import (
	"cmp"
	"context"
	"errors"
	"fmt"
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
	githubapi "github.com/herbygillot/dockhand/internal/github"
	"github.com/herbygillot/dockhand/internal/model"
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
	// streams are the command's, for a question opening the engine asks.
	streams Streams
	// namesBranch is a command naming a branch, by -b, -p, or --pr or by
	// its name, which run outside any checkout finds the registered one.
	namesBranch bool
}

func (s *settings) flags(root *cobra.Command) {
	root.PersistentFlags().StringVarP(&s.tree, "tree", "t", "", "the ports checkout (default $MACPORTS_TREE, else the current directory)")
	root.PersistentFlags().StringVar(&s.database, "db", "", "the database (default $DOCKHAND_DB, else ~/.dockhand/dockhand.db)")
	root.PersistentFlags().StringVar(&s.git, "git", "", "the git executable (default $GIT_BIN, else git on PATH)")
}

// pullRequestsVariable names the sandbox pull requests go to instead of
// MacPorts' repository (engine.Options.PullRequests).
const pullRequestsVariable = "DOCKHAND_PULL_REQUESTS"

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
	database := cmp.Or(s.database, os.Getenv("DOCKHAND_DB"))
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
			tree = engine.Here(ctx, named, ".", cmp.Or(s.git, os.Getenv("GIT_BIN")))
		}
	}
	return engine.Options{
		Tree:      tree,
		Git:       cmp.Or(s.git, os.Getenv("GIT_BIN")),
		Database:  database,
		Worktrees: file.Worktrees,
		// holds.majors = "allow" lets bump and serve move a major version.
		AllowMajors: !file.Holds.HoldMajors(),
		Tests:       model.TestPolicy(file.Check.Tests),
		// DOCKHAND_UPSTREAM fetches master from a mirror or, in tests, a
		// local repository.
		Upstream: os.Getenv("DOCKHAND_UPSTREAM"),
		// DOCKHAND_PULL_REQUESTS sends pull requests to a sandbox, a fork
		// of MacPorts' repository, for the acceptance test's repeatable
		// rows; submit's preview names it.
		PullRequests: os.Getenv(pullRequestsVariable),
		// Readings are a saving: where the cache can't be placed, none
		// are kept.
		Readings: readingCache(),
	}, file, configPath, nil
}

func (s *settings) open(ctx context.Context) (*engine.Engine, error) {
	options, file, configPath, err := s.options(ctx)
	if err != nil {
		return nil, err
	}
	e, err := engine.Open(ctx, options)
	if errors.Is(err, engine.ErrNotPortsTree) && s.namesBranch && s.tree == "" && os.Getenv("MACPORTS_TREE") == "" {
		if options.Tree, err = s.registeredCheckout(ctx, options, err); err == nil {
			e, err = engine.Open(ctx, options)
		}
	}
	if refused := new(engine.LiveUnderMigrationError); errors.As(err, &refused) && s.streams.terminal() {
		// A person decides: their checks can be resumed by this build
		// once they stop, as dockhand wait resumes a stopped check.
		migrate, askErr := s.streams.askYes(fmt.Sprintf("%v.\n? Migrate anyway? What they're doing stops, and this dockhand can resume a stopped check. [y/N] ", refused), false)
		if askErr != nil {
			return nil, askErr
		}
		if migrate {
			options.MigrateUnderLive = true
			e, err = engine.Open(ctx, options)
		}
	}
	if err != nil {
		return nil, err
	}
	s.file, s.opened, s.openedWith = file, e, options
	e.ConfigFile = configPath
	e.Builds = engine.Builds{Default: file.Build.Default, Unattended: file.Build.Unattended, Profiles: map[string]engine.Profile{}}
	for name, profile := range file.Build.Profiles {
		releases, rule := profile.List()
		e.Builds.Profiles[name] = engine.Profile{Provider: profile.Provider, Releases: releases, Rule: rule}
	}
	e.Providers = map[string]buildenv.Provider{}
	if command := file.Providers.Command; command != nil {
		e.Providers[buildenv.Command] = &script.Provider{Run: command.Run, Label: command.Name, Repo: e.Repo}
	}
	// One GitHub runtime, which the engine's forge, discovery, and project
	// inspection share with Actions (CX-11).
	e.GitHub = &githubapi.Runtime{Client: authAPI(authStore)}
	remote := file.Providers.GitHub.Remote
	github := &ghactions.Provider{Repo: e.Repo, Fork: func(ctx context.Context) (buildenv.Fork, error) { return e.Fork(ctx, remote) },
		API: ghactions.GitHub{Client: e.GitHub.Client}, BuildTimeout: file.Providers.GitHub.BuildBound()}
	if testActions != nil {
		github.API, github.Sleep = testActions, func(ctx context.Context, _ time.Duration) error { return ctx.Err() }
	}
	e.Providers[buildenv.GitHub] = github
	// Tart builds wherever Tart is installed; its images are checked when a
	// check names a release.
	if _, err := lookTart("tart"); err == nil {
		e.Providers[buildenv.Tart] = &tart.Provider{Repo: e.Repo, Index: e.PortIndex, TestTimeout: file.Providers.Tart.Timeout(), BuildTimeout: file.Providers.Tart.BuildBound()}
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
	if testHTTPS != nil {
		e.HTTPS = testHTTPS
	}
	if testArchiveFetcher != nil {
		e.ArchiveFetcher = testArchiveFetcher(e)
	}
	return e, nil
}

// registeredCheckout is the one ports checkout the database registered,
// for a command naming a branch run outside any checkout; refused, as
// outside was, where none is, and naming them where several are, since
// which one isn't plain.
func (s *settings) registeredCheckout(ctx context.Context, options engine.Options, outside error) (string, error) {
	checkouts, err := engine.Checkouts(ctx, options)
	if err != nil {
		return "", err
	}
	switch len(checkouts) {
	case 0:
		return "", outside
	case 1:
		return checkouts[0], nil
	}
	var named []string
	for _, checkout := range checkouts {
		named = append(named, config.Tilde(checkout))
	}
	return "", fmt.Errorf("this isn't a ports checkout, and dockhand knows several: %s; choose one with --tree or MACPORTS_TREE", strings.Join(named, ", "))
}

// testHTTPS, when set, stands in for asking URLs over HTTPS.
var testHTTPS engine.HTTPSProbe

// testArchiveFetcher, when set, stands in for fetching archives.
var testArchiveFetcher func(*engine.Engine) engine.ArchiveFetcher

// testActions, when set, stands in for GitHub Actions.
var testActions ghactions.API

// testLeftovers, when set, is a provider with environments left behind,
// registered under its name in place of whichever the configuration made.
var testLeftovers buildenv.LeftoverProvider

// lookTart finds Tart; the tests find none, so they don't depend on the
// machine they run on.
var lookTart = exec.LookPath

// testPortReader, when set, stands in for MacPorts' evaluator in plans.
var testPortReader engine.PortReader

// testDependentReader, when set, stands in for the port index in impact.
var testDependentReader engine.DependentReader

// testOutdatedReader, when set, stands in for upstream discovery.
var testOutdatedReader engine.OutdatedReader

// readingCache is where readings are kept, or none where it can't be
// placed.
func readingCache() string {
	directory, err := engine.ReadingCache()
	if err != nil {
		return ""
	}
	return directory
}
