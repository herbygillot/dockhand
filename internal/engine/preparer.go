package engine

import (
	"context"
	"net/http"
	"os"
	"path/filepath"

	"github.com/herbygillot/dockhand/internal/credential/keychain"
	forgegithub "github.com/herbygillot/dockhand/internal/forge/github"
	forgegitlab "github.com/herbygillot/dockhand/internal/forge/gitlab"
	"github.com/herbygillot/dockhand/internal/github"
	"github.com/herbygillot/dockhand/internal/macports/eval"
	"github.com/herbygillot/dockhand/internal/macports/portedit/archives"
	"github.com/herbygillot/dockhand/internal/macports/portindex"
	"github.com/herbygillot/dockhand/internal/macports/portsource"
	"github.com/herbygillot/dockhand/internal/macports/selection"
	"github.com/herbygillot/dockhand/internal/macports/workspace"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/preparation"
	"github.com/herbygillot/dockhand/internal/upstream"
)

// Preparer edits a port in a source tree: it finds the release an update
// moves to, and prepares the edited tree. preparation.Service is the real
// one; tests substitute their own.
type Preparer interface {
	ResolveRelease(ctx context.Context, request preparation.Request) (model.Release, error)
	Prepare(ctx context.Context, request preparation.Request) (preparation.Result, error)
}

// preparer is the engine's Preparer: the one it was given, or MacPorts'
// own evaluator with upstream discovery on GitHub and GitLab, assembled
// on first use.
func (e *Engine) preparer() (Preparer, error) {
	return assemble(e, &e.Preparer, func() (Preparer, error) {
		ports, err := e.selectionReader()
		if err != nil {
			return nil, err
		}
		return &preparation.Service{Repo: e.Repo, Ports: ports, Upstream: e.discovery(ports), HTTP: http.DefaultClient, Mirror: archives.MacPortsMirror, Workspaces: &workspace.Registry{}}, nil
	})
}

// github is GitHub as the person's login reaches it: the system keychain's,
// or GH_TOKEN's.
func (e *Engine) github() *forgegithub.Client {
	return &forgegithub.Client{Client: github.SystemClient(keychain.Store{}), GitExecutable: e.options.Git}
}

// discovery finds ports' newest releases upstream, on GitHub and GitLab.
func (e *Engine) discovery(ports *selection.Reader) *upstream.Service {
	return &upstream.Service{
		Ports: ports, HTTP: http.DefaultClient, Versions: ports,
		Catalogs: map[portsource.Forge]upstream.Catalog{
			portsource.GitHub: e.github(),
			portsource.GitLab: &forgegitlab.Client{HTTP: http.DefaultClient},
		},
	}
}

// selectionReader is MacPorts' own evaluator, resolving port names
// against an index staged for each source tree.
func (e *Engine) selectionReader() (*selection.Reader, error) {
	return assemble(e, &e.ports, func() (*selection.Reader, error) {
		cache, err := IndexCache()
		if err != nil {
			return nil, err
		}
		native := &eval.Evaluator{Executable: e.options.Tclsh}
		index := portindex.Config{CacheDirectory: cache, Mirror: &portindex.Mirror{HTTP: http.DefaultClient, Base: os.Getenv("DOCKHAND_INDEX_MIRROR")}}
		return &selection.Reader{Evaluator: native, Index: &portindex.Stager{Repo: e.Repo, Config: index, NativePlatform: native.NativePlatform, WithoutBase: true}}, nil
	})
}

// PortIndex is how the engine stages a tree's port index for a platform,
// which a provider that ships the tree to a builder ships with it.
func (e *Engine) PortIndex() (portindex.Source, error) {
	ports, err := e.selectionReader()
	if err != nil {
		return nil, err
	}
	return ports.Index, nil
}

// ReadingCache is where readings of upstream's archives are kept:
// $DOCKHAND_READING_CACHE, else dockhand/readings in the user's cache
// directory. It is disposable.
func ReadingCache() (string, error) {
	if chosen := os.Getenv("DOCKHAND_READING_CACHE"); chosen != "" {
		return filepath.Abs(chosen)
	}
	cache, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(cache, "dockhand", "readings"), nil
}

// IndexCache is where port indexes are kept: $DOCKHAND_INDEX_CACHE, else
// dockhand/indexes in the user's cache directory. It is disposable.
func IndexCache() (string, error) {
	if chosen := os.Getenv("DOCKHAND_INDEX_CACHE"); chosen != "" {
		return filepath.Abs(chosen)
	}
	cache, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(cache, "dockhand", "indexes"), nil
}
