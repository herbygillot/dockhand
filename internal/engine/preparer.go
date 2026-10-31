package engine

import (
	"context"
	"errors"
	"path/filepath"

	"github.com/herbygillot/dockhand/internal/editprep"
	"github.com/herbygillot/dockhand/internal/fetch"
	forgegithub "github.com/herbygillot/dockhand/internal/forge/github"
	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/github"
	"github.com/herbygillot/dockhand/internal/macports/eval"
	"github.com/herbygillot/dockhand/internal/macports/portindex"
	"github.com/herbygillot/dockhand/internal/macports/selection"
	"github.com/herbygillot/dockhand/internal/model"
)

// Preparer edits a port in a source tree: it finds the release an update
// moves to, and prepares the edited tree. preparation.Service is the real
// one; tests substitute their own.
type Preparer interface {
	ResolveRelease(ctx context.Context, request editprep.Request) (model.Release, error)
	Prepare(ctx context.Context, request editprep.Request) (editprep.Result, error)
}

// preparer is the engine's Preparer, as compose composed it from MacPorts'
// evaluator and upstream discovery (Then's item 15); none where there's no
// evaluator to compose it from.
func (e *Engine) preparer() (Preparer, error) {
	if e.Preparer == nil {
		return nil, errors.New("no port editor is set up: MacPorts' evaluator, port-tclsh, couldn't be read")
	}
	return e.Preparer, nil
}

// github is GitHub as the person's login reaches it, through the
// process's one runtime (GitHub), one with no login where none was
// given: one forge client, made once, which the forge, discovery, and
// create's project inspection share, where each made its own.
func (e *Engine) github() *forgegithub.Client {
	e.githubOnce.Do(func() {
		if e.GitHub == nil {
			e.GitHub = github.NewRuntime(github.Signin{})
		}
		e.githubClient = &forgegithub.Client{Client: e.GitHub.Client, Git: e.options.git()}
	})
	return e.githubClient
}

// GitHubClient is the engine's one GitHub client (github), which compose
// gives upstream discovery as its GitHub catalog.
func (e *Engine) GitHubClient() *forgegithub.Client { return e.github() }

// GitProgram is the Git the engine runs, which compose gives discovery.
func (e *Engine) GitProgram() git.Program { return e.options.git() }

// SelectionReader is MacPorts' own evaluator, which compose builds
// discovery, the preparer, and the archiver over (selectionReader).
func (e *Engine) SelectionReader() (*selection.Reader, error) { return e.selectionReader() }

// selectionReader is MacPorts' own evaluator, resolving port names
// against an index staged for each source tree.
func (e *Engine) selectionReader() (*selection.Reader, error) {
	return assemble(e, &e.ports, func() (*selection.Reader, error) {
		index, err := e.indexConfig()
		if err != nil {
			return nil, err
		}
		// A port's baseline read unchanged since a look's last is answered
		// from what that evaluation kept, beside the index generations,
		// whose collection leaves it alone (eval.Memo).
		native := &eval.Evaluator{Executable: e.Tclsh(), Env: e.options.Env, Memo: &eval.Memo{Directory: filepath.Join(index.CacheDirectory, "evaluations")}}
		return &selection.Reader{Evaluator: native, Index: &portindex.Stager{Repo: e.Repo, Config: index, NativePlatform: native.NativePlatform, WithoutBase: true}}, nil
	})
}

// indexConfig is where port indexes are cached, and the mirror whose index
// seeds a cache with no generation near enough.
func (e *Engine) indexConfig() (portindex.Config, error) {
	cache, err := e.indexCache()
	if err != nil {
		return portindex.Config{}, err
	}
	return portindex.Config{CacheDirectory: cache, Env: e.options.Env, Mirror: &portindex.Mirror{HTTP: fetch.Client, Base: e.options.IndexMirror}}, nil
}

// PortIndex is how the engine stages a tree's port index for a platform,
// which a provider that ships the tree to a builder ships with it. Unlike
// name lookup's, it keeps the tree's base, as the stager says verification
// does: a check's index derives from its base's for the release, by the
// ports the branch changes, and a snapshot, which has no commit, reaches
// the mirror's index through its base's. Lookup's stager, shared until
// batch 14, built check-23's whole index for macOS 15 (the ov run, finding
// 4).
func (e *Engine) PortIndex() (portindex.Source, error) {
	stager, err := assemble(e, &e.checkIndex, func() (*portindex.Stager, error) {
		index, err := e.indexConfig()
		if err != nil {
			return nil, err
		}
		native := &eval.Evaluator{Executable: e.Tclsh(), Env: e.options.Env}
		return &portindex.Stager{Repo: e.Repo, Config: index, NativePlatform: native.NativePlatform}, nil
	})
	if err != nil {
		return nil, err
	}
	return stager, nil
}

// indexCache is where this engine keeps port indexes (Options.Indexes).
func (e *Engine) indexCache() (string, error) {
	if e.options.Indexes == "" {
		return "", errors.New("no directory to cache port indexes in was given")
	}
	return e.options.Indexes, nil
}
