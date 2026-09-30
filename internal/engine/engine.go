// Package engine carries out dockhand v3's operations (docs/design-v3.md):
// it binds a person's request to the ports checkout, Git, and the store,
// and does the work. The command package parses and renders; the engine
// decides and acts.
package engine

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/herbygillot/dockhand/internal/buildenv"
	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/github"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/selection"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/progress"
	"github.com/herbygillot/dockhand/internal/store"
	"github.com/herbygillot/dockhand/internal/store/sqlite"
)

// UpstreamURL is where master is fetched from, whatever the checkout's
// remotes are called, so a branch always starts from MacPorts' own master.
const UpstreamURL = "https://github.com/macports/macports-ports.git"

// UpstreamRepository is MacPorts' ports repository on GitHub.
const UpstreamRepository = "macports/macports-ports"

// Options selects what an engine works on.
type Options struct {
	// Tree is a directory inside the ports checkout; "." when empty.
	Tree string
	// Git is the git executable; "git" when empty.
	Git string
	// Database is the store's path.
	Database string
	// Worktrees is the directory managed worktrees live in; when empty,
	// ~/Source/macports-branches (DefaultWorktrees).
	Worktrees string
	// Upstream is the URL master is fetched from; UpstreamURL when empty.
	Upstream string
	// Tclsh is MacPorts' port-tclsh, which evaluates Portfiles; found on
	// PATH when empty.
	Tclsh string
	// Now defaults to time.Now.
	Now func() time.Time
	// Poll is how often a driver looks for a cancel request; a second when
	// zero.
	Poll time.Duration
}

// Engine is bound to one ports checkout and its registered repository.
type Engine struct {
	Repo       *git.Repository
	Store      store.Store
	Repository model.RepositoryID
	// Preparer edits ports; MacPorts' own evaluator when nil.
	Preparer Preparer
	// Forge publishes; GitHub when nil.
	Forge Forge
	// GitHubCLI marks a draft ready where an organization refuses the
	// forge's app; none when nil.
	GitHubCLI GitHubCLI
	// PortReader reads ports for plans; MacPorts' own evaluator when nil.
	PortReader PortReader
	// DependentReader finds dependents for impact; the port index when nil.
	DependentReader DependentReader
	// OutdatedReader finds newer releases for outdated; MacPorts' evaluator
	// and upstream discovery when nil.
	OutdatedReader OutdatedReader
	// ArchiveFetcher fetches the archives a Portfile declares, for diff
	// --archive; MacPorts' evaluator when nil.
	ArchiveFetcher ArchiveFetcher
	// ProjectReader observes upstream projects for create; GitHub when nil.
	ProjectReader ProjectReader
	// HTTPS says whether a URL answers over HTTPS, for a port's plain-HTTP
	// URLs; a request of the URL when nil.
	HTTPS HTTPSProbe
	// Providers are where checks build, by name.
	Providers map[string]buildenv.Provider
	ports     *selection.Reader
	options   Options
	// stopAt stops a history change at a step, as if the process ended
	// there: tests set it (historyStep).
	stopAt func(step string) error
	// betweenReads runs between a capture's two reads of the working
	// files: tests change them there.
	betweenReads func()
	// lazy guards what the engine assembles on first use (assemble, and
	// the forge), which serve's runs, and a check's environments building
	// together, share.
	lazy sync.Mutex
}

// assemble returns what field holds, building it on first use. The build
// runs outside the engine's lock, so it may assemble what it needs in
// turn; two callers building at once both get the one set first.
func assemble[T comparable](e *Engine, field *T, build func() (T, error)) (T, error) {
	var none T
	e.lazy.Lock()
	held := *field
	e.lazy.Unlock()
	if held != none {
		return held, nil
	}
	built, err := build()
	if err != nil {
		return none, err
	}
	e.lazy.Lock()
	defer e.lazy.Unlock()
	if *field == none {
		*field = built
	}
	return *field, nil
}

// Open checks that Tree is inside a ports checkout, opens the store, and
// registers the repository. A directory that is not a ports tree is
// refused before anything is recorded for it.
func Open(ctx context.Context, options Options) (*Engine, error) {
	repo, err := OpenPortsTree(ctx, options.Tree, options.Git)
	if err != nil {
		return nil, err
	}
	s, err := sqlite.Open(ctx, options.Database, sqlite.Options{})
	if err != nil {
		return nil, err
	}
	// A migration can't be undone by an older build, which can't open the
	// database after it, so it's said, with the copy kept of the database
	// before it.
	if migrated := s.Migration(); migrated != nil {
		progress.Report(ctx, "Migrated dockhand's database from schema %d to %d; dockhand builds older than this one can't open it now. Its schema %d form is kept at %s.", migrated.From, migrated.To, migrated.From, migrated.Copy)
	}
	id, err := s.Register(ctx, repo.CommonDir)
	if err != nil {
		s.Close()
		return nil, err
	}
	return &Engine{Repo: repo, Store: s, Repository: id, options: options}, nil
}

// Close releases the store.
func (e *Engine) Close() error { return e.Store.Close() }

func (e *Engine) now() time.Time {
	if e.options.Now != nil {
		return e.options.Now()
	}
	return time.Now()
}

// Upstream is the URL master is fetched from.
func (e *Engine) Upstream() string {
	if e.options.Upstream != "" {
		return e.options.Upstream
	}
	return UpstreamURL
}

// Clone is the main checkout's directory, whichever worktree the engine
// was opened in.
func (e *Engine) Clone() string { return filepath.Dir(e.Repo.CommonDir) }

// DefaultWorktrees is where managed worktrees go unless configured
// otherwise: ~/Source/macports-branches, wherever the clone is, so a
// person's branches are in one place they know. Without a home directory,
// they go beside the clone.
func (e *Engine) DefaultWorktrees() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return filepath.Join(filepath.Dir(e.Clone()), "macports-branches")
	}
	return filepath.Join(home, "Source", "macports-branches")
}

// Worktrees is where managed worktrees go.
func (e *Engine) Worktrees() string {
	if e.options.Worktrees != "" {
		return e.options.Worktrees
	}
	return e.DefaultWorktrees()
}

// ErrNotPortsTree reports a directory that is not a MacPorts ports checkout.
var ErrNotPortsTree = errors.New("not a MacPorts ports tree")

// GitVersion reads the version of the Git dockhand runs, options.Git or
// git on PATH, and refuses one older than dockhand works with
// (git.MinimumVersion), saying how to get a newer one.
func GitVersion(ctx context.Context, options Options) (git.Version, error) {
	executable := options.Git
	if executable == "" {
		executable = "git"
	}
	version, err := git.ExecutableVersion(ctx, executable)
	if err != nil {
		return version, fmt.Errorf("dockhand runs Git, and %s can't be run: %w; install Git, such as with: sudo port install git", executable, err)
	}
	if !version.AtLeast(git.MinimumVersion) {
		return version, fmt.Errorf("dockhand needs Git %s or newer, and %s is %s; install a newer one, such as with: sudo port install git, or name one with GIT_BIN", git.MinimumVersion, version.Path, version)
	}
	return version, nil
}

// Here is the checkout to work on in dir when a command is pointed at
// tree without naming it itself, as MACPORTS_TREE points one: dir's own
// worktree when it is a worktree of the same repository, whose branch is
// the one checked out here, and tree otherwise. Inside a branch's
// worktree, that worktree is the checkout it belongs to.
func Here(ctx context.Context, tree, dir, executable string) string {
	named, err := git.Open(ctx, tree, executable)
	if err != nil {
		return tree
	}
	here, err := git.Open(ctx, dir, executable)
	if err != nil || !sameDirectory(here.CommonDir, named.CommonDir) {
		return tree
	}
	return here.Root
}

// sameDirectory reports whether two paths name one directory, however
// either is spelled.
func sameDirectory(a, b string) bool {
	a, errA := filepath.EvalSymlinks(a)
	b, errB := filepath.EvalSymlinks(b)
	return errA == nil && errB == nil && a == b
}

// OpenPortsTree opens the checkout containing dir and refuses anything that
// is not a ports tree: at least one <category>/<port>/Portfile in the
// working tree, or, for a sparse checkout, on one of its local branches.
func OpenPortsTree(ctx context.Context, dir, executable string) (*git.Repository, error) {
	if dir == "" {
		dir = "."
	}
	repo, err := git.Open(ctx, dir, executable)
	if err != nil {
		return nil, fmt.Errorf("%s is not in a Git checkout (select one with --tree or MACPORTS_TREE): %w", dir, err)
	}
	if macports.ValidatePortsTree(repo.Root, dir) == nil || branchHoldsPorts(ctx, repo) {
		return repo, nil
	}
	return nil, fmt.Errorf("%w: %s has no <category>/<port>/Portfile (select your macports-ports clone with --tree or MACPORTS_TREE)", ErrNotPortsTree, repo.Root)
}

func branchHoldsPorts(ctx context.Context, repo *git.Repository) bool {
	var names []string
	if current, err := repo.CurrentBranch(ctx); err == nil {
		names = append(names, current)
	}
	if refs, err := repo.ReadRefs(ctx, "refs/heads/"); err == nil {
		for name := range refs {
			names = append(names, strings.TrimPrefix(name, "refs/heads/"))
		}
	}
	for _, name := range names {
		if _, tree, err := repo.Branch(ctx, name); err == nil && treeHoldsPorts(ctx, repo, tree) {
			return true
		}
	}
	return false
}

func treeHoldsPorts(ctx context.Context, repo *git.Repository, tree string) bool {
	categories, err := repo.ReadTree(ctx, tree)
	if err != nil {
		return false
	}
	for _, category := range categories {
		if category.Type != "tree" || !macports.IsCategory(category.Name) {
			continue
		}
		ports, err := repo.ReadTree(ctx, category.Object)
		if err != nil {
			continue
		}
		for _, port := range ports {
			if port.Type != "tree" || strings.HasPrefix(port.Name, ".") {
				continue
			}
			files, err := repo.ReadTree(ctx, port.Object)
			if err != nil {
				continue
			}
			for _, file := range files {
				if file.Name == "Portfile" && file.Type == "blob" {
					return true
				}
			}
		}
	}
	return false
}

// UpstreamRemote finds the checkout's remote for MacPorts' repository,
// whatever it is called; nil when it has none.
func (e *Engine) UpstreamRemote(ctx context.Context) (*git.Remote, error) {
	remotes, err := e.Repo.Remotes(ctx)
	if err != nil {
		return nil, err
	}
	for _, remote := range remotes {
		if name, err := github.RemoteRepository(remote.FetchURL); err == nil && strings.EqualFold(name, UpstreamRepository) {
			return &remote, nil
		}
	}
	return nil, nil
}

// fetchMaster freezes MacPorts' current master in the repository.
func (e *Engine) fetchMaster(ctx context.Context) (model.ObjectID, error) {
	commit, _, err := e.Repo.FetchBranch(ctx, e.Upstream(), "master")
	if err != nil {
		return "", fmt.Errorf("fetching master from %s: %w", e.Upstream(), err)
	}
	return model.ObjectID(commit), nil
}

func exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}
