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
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
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
	gitprogram "github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/github"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/subprocess"
	tartvm "github.com/herbygillot/dockhand/internal/tart"
)

// Options are the command line's selections, each its flag's value, empty
// where it wasn't given: its environment variable then stands, then its
// default.
type Options struct {
	Tree, Database, Git string
	// MigrateUnderLive migrates the database though a process on the
	// older schema is working, as the person answered.
	MigrateUnderLive bool
	// MigrateRelease migrates a database a release last opened with a
	// build that isn't one, as the person answered.
	MigrateRelease bool
	// NamesBranch is a command naming a branch, which run outside any
	// checkout finds the one the database registered (registeredCheckout).
	NamesBranch bool
	// Env is the command line's environment, as os.Environ gives one, and
	// Dir its working directory, which compose reads in place of the
	// process's.
	Env []string
	Dir string
}

// Getenv is the variable's value in the command line's environment, or
// empty.
func (o Options) Getenv(name string) string {
	value := ""
	for _, entry := range o.Env {
		if key, rest, ok := strings.Cut(entry, "="); ok && key == name {
			value = rest
		}
	}
	return value
}

// Signin is how the command line reaches the person's GitHub login, with
// the login kept in store: GH_TOKEN's token, else GITHUB_TOKEN's, ahead of
// it; its renewals' lock beside the configuration,
// ~/.dockhand/github-login.lock; and the GitHub CLI on the command line's
// PATH.
func (o Options) Signin(store credential.Store) github.Signin {
	signin := github.Signin{Store: store, CLI: o.LookPath("gh")}
	for _, source := range []github.CredentialSource{github.SourceGHEnvironment, github.SourceGitHubEnvironment} {
		if secret := strings.TrimSpace(o.Getenv(string(source))); secret != "" {
			signin.Token = github.Token{Secret: secret, Source: source}
			break
		}
	}
	if home := o.Places().Home; home != "" {
		signin.Lock = filepath.Join(home, ".dockhand", "github-login.lock")
	}
	return signin
}

// SpendLog is the file each command appends what it spent of GitHub's
// allowance to, $DOCKHAND_GITHUB_LOG, or none.
func (o Options) SpendLog() string {
	if named := o.Getenv("DOCKHAND_GITHUB_LOG"); named != "" {
		if path, err := o.Places().Absolute(named); err == nil {
			return path
		}
	}
	return ""
}

// LookPath is the executable name finds on the command line's PATH
// (subprocess.LookPath), or empty.
func (o Options) LookPath(name string) string {
	path, err := subprocess.LookPath(name, o.Env)
	if err != nil {
		return ""
	}
	return path
}

// TartPlaces are where dockhand's Tart work lives on this Mac: Home, its
// own Tart home; Personal, the person's, which it only reads; and Keys,
// the folder of the SSH keys its guests are reached with.
type TartPlaces struct {
	Home, Personal, Keys string
}

// TartPlaces are dockhand's Tart home, $DOCKHAND_TART_HOME or
// ~/.dockhand/tart; the person's, $TART_HOME or ~/.tart; and the SSH
// keys' folder, $DOCKHAND_SSH_DIR or ~/.dockhand/ssh: each variable's
// value made absolute against the command line's directory, each default
// under its home, or empty without one.
func (o Options) TartPlaces() TartPlaces {
	places := o.Places()
	place := func(variable string, under ...string) string {
		if named := o.Getenv(variable); named != "" {
			if path, err := places.Absolute(named); err == nil {
				return path
			}
		}
		if places.Home == "" {
			return ""
		}
		return filepath.Join(append([]string{places.Home}, under...)...)
	}
	return TartPlaces{
		Home:     place("DOCKHAND_TART_HOME", ".dockhand", "tart"),
		Personal: place("TART_HOME", ".tart"),
		Keys:     place("DOCKHAND_SSH_DIR", ".dockhand", "ssh"),
	}
}

// TartProvider is the Tart provider, in the command line's places.
func (o Options) TartProvider() *tart.Provider {
	places := o.TartPlaces()
	return &tart.Provider{Tart: tartvm.Client{Home: places.Home, Env: o.Env}, Personal: places.Personal, Keys: places.Keys}
}

// Places are the command line's home, $HOME, and working directory.
func (o Options) Places() config.Places {
	return config.Places{Home: o.Getenv("HOME"), Dir: o.Dir}
}

// Dependencies stand in for what compose would make: the readers,
// providers, and clients a test gives, each the real one where it's nil.
type Dependencies struct {
	Preparer func(*engine.Engine) engine.Preparer
	Forge    func(*engine.Engine) engine.Forge
	// Ports are what the engine reads of ports, MacPorts' own where no
	// Reader is given; ArchiveFetcher, given the engine, fetches archives
	// in their place.
	Ports          engine.Ports
	ArchiveFetcher func(*engine.Engine) engine.ArchiveFetcher
	OutdatedReader engine.OutdatedReader
	ProjectReader  engine.ProjectReader
	HTTPS          engine.HTTPSProbe
	GitHubCLI      engine.GitHubCLI
	// Actions stands in for GitHub Actions, with no waiting between polls.
	Actions ghactions.API
	// Leftovers is a provider with environments left behind, registered
	// under its name in place of whichever the configuration made.
	Leftovers buildenv.LeftoverProvider
	// LookTart finds Tart; exec.LookPath where nil.
	LookTart func(string) (string, error)
	// GitHubClient is GitHub as the login reaches it, with Store's;
	// github.SystemClient where nil, over the system keychain where Store
	// is nil.
	GitHubClient func(github.Signin) *github.Client
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
	places := options.Places()
	configPath, err := places.ConfigPath(options.Getenv(config.PathVariable))
	if err != nil {
		return engine.Options{}, config.File{}, "", err
	}
	file, err := places.Load(configPath)
	if err != nil {
		return engine.Options{}, config.File{}, "", err
	}
	// The hold list names rules the engine raises, which the configuration
	// can't know of.
	for _, rule := range file.Holds.Allow {
		if !slices.Contains(engine.HoldRules(), rule) {
			return engine.Options{}, config.File{}, "", fmt.Errorf("%s: holds.allow: %q is not a hold dockhand raises or lets go; the rules are %s", configPath, rule, strings.Join(engine.HoldRules(), ", "))
		}
	}
	git := cmp.Or(options.Git, options.Getenv("GIT_BIN"))
	database := cmp.Or(options.Database, options.Getenv("DOCKHAND_DB"))
	if database == "" {
		if places.Home == "" {
			return engine.Options{}, config.File{}, "", errors.New("$HOME is not defined; name the database with --db or DOCKHAND_DB")
		}
		database = filepath.Join(places.Home, ".dockhand", "dockhand.db")
	}
	// A path given is read against the command line's directory, never
	// the process's, which a test running beside others shares.
	if database, err = places.Absolute(database); err != nil {
		return engine.Options{}, config.File{}, "", err
	}
	// With no --tree, MACPORTS_TREE names the checkout, and a directory in
	// one of its worktrees is that worktree, whose branch is the one
	// checked out here.
	tree := options.Tree
	if tree == "" {
		tree = cmp.Or(options.Dir, ".")
		if named := options.Getenv("MACPORTS_TREE"); named != "" {
			if named, err = places.Absolute(named); err != nil {
				return engine.Options{}, config.File{}, "", err
			}
			tree = engine.Here(ctx, named, cmp.Or(options.Dir, "."), gitprogram.Program{Executable: git, Env: options.Env})
		}
	}
	if tree, err = places.Absolute(tree); err != nil {
		return engine.Options{}, config.File{}, "", err
	}
	readings, _ := options.cacheDirectory("DOCKHAND_READING_CACHE", "readings")
	indexes, _ := options.cacheDirectory("DOCKHAND_INDEX_CACHE", "indexes")
	return engine.Options{
		Tree: tree,
		Git:  git,
		Env:  options.Env,
		// port-tclsh on the command line's PATH, else under MacPorts'
		// default prefix.
		Tclsh:     macports.FindTclsh(func(name string) (string, error) { return subprocess.LookPath(name, options.Env) }),
		Database:  database,
		Worktrees: file.Worktrees,
		// holds.majors = "allow" lets bump and serve move a major version.
		AllowMajors: !file.Holds.HoldMajors(),
		AllowHolds:  file.Holds.Allow,
		Tests:       model.TestPolicy(file.Check.Tests),
		// DOCKHAND_UPSTREAM fetches master from a mirror or, in tests, a
		// local repository.
		Upstream: options.Getenv("DOCKHAND_UPSTREAM"),
		// DOCKHAND_PULL_REQUESTS sends pull requests to a sandbox, a fork
		// of MacPorts' repository, for the acceptance test's repeatable
		// rows; submit's preview names it.
		PullRequests: options.Getenv(PullRequestsVariable),
		// Readings are a saving: where the cache can't be placed, none
		// are kept; port indexes are staged, so their cache is refused
		// when they're staged.
		Readings:         readings,
		Indexes:          indexes,
		IndexMirror:      options.Getenv("DOCKHAND_INDEX_MIRROR"),
		Home:             places.Home,
		MigrateUnderLive: options.MigrateUnderLive,
		MigrateRelease:   options.MigrateRelease,
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
	if errors.Is(err, engine.ErrNotPortsTree) && options.NamesBranch && options.Tree == "" && options.Getenv("MACPORTS_TREE") == "" {
		if engineOptions.Tree, err = registeredCheckout(ctx, engineOptions, options.Places(), err); err == nil {
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
		e.Providers[buildenv.Command] = &script.Provider{Run: command.Run, Label: command.Name, Repo: e.Repo, Env: options.Env}
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
	e.GitHub = &github.Runtime{Client: client(options.Signin(store))}
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
		provider := options.TartProvider()
		provider.Repo, provider.Index = e.Repo, e.PortIndex
		provider.TestTimeout, provider.BuildTimeout = file.Providers.Tart.Timeout(), file.Providers.Tart.BuildBound()
		e.Providers[buildenv.Tart] = provider
	}
	if dependencies.Leftovers != nil {
		e.Providers[dependencies.Leftovers.Name()] = dependencies.Leftovers
	}
	e.Maintainers = file.Maintainers()
	// Each provider's capacity, which serve and a check run here keep to.
	e.Capacity = map[string]int{}
	for name := range e.Providers {
		e.Capacity[name] = file.Capacity(name)
	}
	e.GitHubCLI = dependencies.GitHubCLI
	if e.GitHubCLI == nil {
		e.GitHubCLI = github.CLI{Path: options.LookPath("gh")}
	}
	if dependencies.Preparer != nil {
		e.Preparer = dependencies.Preparer(e)
	}
	if dependencies.Forge != nil {
		e.Forge = dependencies.Forge(e)
	}
	e.Ports = dependencies.Ports
	if dependencies.ArchiveFetcher != nil {
		e.Ports.Fetcher = dependencies.ArchiveFetcher(e)
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
func registeredCheckout(ctx context.Context, options engine.Options, places config.Places, outside error) (string, error) {
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
		named = append(named, places.Tilde(checkout))
	}
	return "", fmt.Errorf("this isn't a ports checkout, and dockhand knows several: %s; choose one with --tree or MACPORTS_TREE", strings.Join(named, ", "))
}

// cacheDirectory is dockhand's directory, named, in the command line's
// user cache directory: ~/Library/Caches on macOS, else $XDG_CACHE_HOME or
// ~/.cache, as os.UserCacheDir has it; or the variable's value, made
// absolute, where it's set. It is disposable.
func (o Options) cacheDirectory(variable, name string) (string, error) {
	places := o.Places()
	if chosen := o.Getenv(variable); chosen != "" {
		return places.Absolute(chosen)
	}
	cache := o.Getenv("XDG_CACHE_HOME")
	if runtime.GOOS == "darwin" || !filepath.IsAbs(cache) {
		if places.Home == "" {
			return "", errors.New("$HOME is not defined")
		}
		cache = filepath.Join(places.Home, ".cache")
		if runtime.GOOS == "darwin" {
			cache = filepath.Join(places.Home, "Library", "Caches")
		}
	}
	return filepath.Join(cache, "dockhand", name), nil
}
