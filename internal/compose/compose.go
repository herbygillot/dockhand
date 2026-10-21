// Package compose assembles dockhand for a command: the engine, with its
// providers, its GitHub runtime, and the readers it's given, from the
// person's settings and configuration, in one place. The command line
// asks it for an engine; a test gives it stand-ins (Dependencies), so no
// seam is a package variable (X1, the architecture review of 5897a82).
package compose

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

	"github.com/herbygillot/dockhand/internal/buildenv"
	"github.com/herbygillot/dockhand/internal/buildenv/ghactions"
	"github.com/herbygillot/dockhand/internal/buildenv/script"
	"github.com/herbygillot/dockhand/internal/buildenv/tart"
	"github.com/herbygillot/dockhand/internal/config"
	"github.com/herbygillot/dockhand/internal/credential"
	"github.com/herbygillot/dockhand/internal/credential/keychain"
	"github.com/herbygillot/dockhand/internal/engine"
	"github.com/herbygillot/dockhand/internal/github"
	"github.com/herbygillot/dockhand/internal/model"
)

// Options are the command line's selections, each its flag's value, empty
// where it wasn't given: its environment variable then stands, then its
// default.
type Options struct {
	Tree, Database, Git string
	// MigrateUnderLive migrates the database though a process on the
	// older schema is working, as the person answered.
	MigrateUnderLive bool
	// NamesBranch is a command naming a branch, which run outside any
	// checkout finds the one the database registered (registeredCheckout).
	NamesBranch bool
}

// Dependencies stand in for what compose would make: the readers,
// providers, and clients a test gives, each the real one where it's nil.
type Dependencies struct {
	Preparer        func(*engine.Engine) engine.Preparer
	Forge           func(*engine.Engine) engine.Forge
	ArchiveFetcher  func(*engine.Engine) engine.ArchiveFetcher
	PortReader      engine.PortReader
	DependentReader engine.DependentReader
	OutdatedReader  engine.OutdatedReader
	ProjectReader   engine.ProjectReader
	HTTPS           engine.HTTPSProbe
	GitHubCLI       engine.GitHubCLI
	// Actions stands in for GitHub Actions, with no waiting between polls.
	Actions ghactions.API
	// Leftovers is a provider with environments left behind, registered
	// under its name in place of whichever the configuration made.
	Leftovers buildenv.LeftoverProvider
	// LookTart finds Tart; exec.LookPath where nil.
	LookTart func(string) (string, error)
	// GitHubClient is GitHub as the login in Store reaches it;
	// github.SystemClient where nil, over the system keychain where Store
	// is nil.
	GitHubClient func(credential.Store) *github.Client
	Store        credential.Store
}

// Assembly is what a command works with: the engine, opened, the
// configuration it was opened with, and where that's read from.
type Assembly struct {
	Engine *engine.Engine
	File   config.File
	// Options are the engine's options, as it was opened with them.
	Options    engine.Options
	ConfigPath string
}

// PullRequestsVariable names the sandbox pull requests go to instead of
// MacPorts' repository (engine.Options.PullRequests).
const PullRequestsVariable = "DOCKHAND_PULL_REQUESTS"

// Settings resolves the engine's options from the command line's, the
// environment, and the configuration file, which it reads.
func Settings(ctx context.Context, options Options) (engine.Options, config.File, string, error) {
	configPath, err := config.Path()
	if err != nil {
		return engine.Options{}, config.File{}, "", err
	}
	file, err := config.Load(configPath)
	if err != nil {
		return engine.Options{}, config.File{}, "", err
	}
	database := cmp.Or(options.Database, os.Getenv("DOCKHAND_DB"))
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
	tree := options.Tree
	if tree == "" {
		tree = "."
		if named := os.Getenv("MACPORTS_TREE"); named != "" {
			tree = engine.Here(ctx, named, ".", cmp.Or(options.Git, os.Getenv("GIT_BIN")))
		}
	}
	return engine.Options{
		Tree:      tree,
		Git:       cmp.Or(options.Git, os.Getenv("GIT_BIN")),
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
		PullRequests: os.Getenv(PullRequestsVariable),
		// Readings are a saving: where the cache can't be placed, none
		// are kept.
		Readings:         readingCache(),
		MigrateUnderLive: options.MigrateUnderLive,
	}, file, configPath, nil
}

// Open opens the engine for a command, with its providers, its GitHub
// runtime, and the stand-ins given. A database a process on the older
// schema is working with is refused (engine.LiveUnderMigrationError), for
// the command to ask the person, and open again with MigrateUnderLive.
func Open(ctx context.Context, options Options, dependencies Dependencies) (Assembly, error) {
	engineOptions, file, configPath, err := Settings(ctx, options)
	if err != nil {
		return Assembly{}, err
	}
	e, err := engine.Open(ctx, engineOptions)
	if errors.Is(err, engine.ErrNotPortsTree) && options.NamesBranch && options.Tree == "" && os.Getenv("MACPORTS_TREE") == "" {
		if engineOptions.Tree, err = registeredCheckout(ctx, engineOptions, err); err == nil {
			e, err = engine.Open(ctx, engineOptions)
		}
	}
	if err != nil {
		return Assembly{}, err
	}
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
	client, store := dependencies.GitHubClient, dependencies.Store
	if client == nil {
		client = github.SystemClient
	}
	if store == nil {
		store = keychain.Store{}
	}
	e.GitHub = &github.Runtime{Client: client(store)}
	remote := file.Providers.GitHub.Remote
	actions := &ghactions.Provider{Repo: e.Repo, Fork: func(ctx context.Context) (buildenv.Fork, error) { return e.Fork(ctx, remote) },
		API: ghactions.GitHub{Client: e.GitHub.Client}, BuildTimeout: file.Providers.GitHub.BuildBound()}
	if dependencies.Actions != nil {
		actions.API, actions.Sleep = dependencies.Actions, func(ctx context.Context, _ time.Duration) error { return ctx.Err() }
	}
	e.Providers[buildenv.GitHub] = actions
	// Tart builds wherever Tart is installed; its images are checked when a
	// check names a release.
	look := dependencies.LookTart
	if look == nil {
		look = exec.LookPath
	}
	if _, err := look("tart"); err == nil {
		e.Providers[buildenv.Tart] = &tart.Provider{Repo: e.Repo, Index: e.PortIndex, TestTimeout: file.Providers.Tart.Timeout(), BuildTimeout: file.Providers.Tart.BuildBound()}
	}
	if dependencies.Leftovers != nil {
		e.Providers[dependencies.Leftovers.Name()] = dependencies.Leftovers
	}
	e.GitHubCLI = dependencies.GitHubCLI
	if e.GitHubCLI == nil {
		e.GitHubCLI = github.CLI{}
	}
	if dependencies.Preparer != nil {
		e.Preparer = dependencies.Preparer(e)
	}
	if dependencies.Forge != nil {
		e.Forge = dependencies.Forge(e)
	}
	if dependencies.ArchiveFetcher != nil {
		e.ArchiveFetcher = dependencies.ArchiveFetcher(e)
	}
	if dependencies.PortReader != nil {
		e.PortReader = dependencies.PortReader
	}
	if dependencies.DependentReader != nil {
		e.DependentReader = dependencies.DependentReader
	}
	if dependencies.OutdatedReader != nil {
		e.OutdatedReader = dependencies.OutdatedReader
	}
	if dependencies.ProjectReader != nil {
		e.ProjectReader = dependencies.ProjectReader
	}
	if dependencies.HTTPS != nil {
		e.HTTPS = dependencies.HTTPS
	}
	return Assembly{Engine: e, File: file, Options: engineOptions, ConfigPath: configPath}, nil
}

// registeredCheckout is the one ports checkout the database registered,
// for a command naming a branch run outside any checkout; refused, as
// outside was, where none is, and naming them where several are, since
// which one isn't plain.
func registeredCheckout(ctx context.Context, options engine.Options, outside error) (string, error) {
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

// readingCache is where readings are kept, or none where it can't be
// placed.
func readingCache() string {
	directory, err := engine.ReadingCache()
	if err != nil {
		return ""
	}
	return directory
}
