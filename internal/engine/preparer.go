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
	"github.com/herbygillot/dockhand/internal/macports/portindex"
	"github.com/herbygillot/dockhand/internal/macports/selection"
	portsource "github.com/herbygillot/dockhand/internal/macports/source"
	"github.com/herbygillot/dockhand/internal/macports/workspace"
	"github.com/herbygillot/dockhand/internal/preparation"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/upstream"
)

// Preparer edits a port in a source tree: it finds the release an update
// moves to, and prepares the edited tree. preparation.Service is the real
// one; tests substitute their own.
type Preparer interface {
	ResolveRelease(ctx context.Context, request preparation.Request) (record.Release, error)
	Prepare(ctx context.Context, request preparation.Request) (preparation.Result, error)
}

// preparer is the engine's Preparer: the one it was given, or MacPorts'
// own evaluator with upstream discovery on GitHub and GitLab, assembled
// on first use.
func (e *Engine) preparer() (Preparer, error) {
	if e.Preparer != nil {
		return e.Preparer, nil
	}
	ports, err := e.selectionReader()
	if err != nil {
		return nil, err
	}
	client := &github.Client{HTTP: http.DefaultClient, Credentials: github.SystemCredentials{Store: keychain.Store{}, Key: github.CredentialKey}}
	discovery := &upstream.Service{
		Ports: ports, HTTP: http.DefaultClient, Versions: ports,
		Catalogs: map[portsource.Forge]upstream.Catalog{
			portsource.GitHub: &forgegithub.Client{Client: client, GitExecutable: e.options.Git},
			portsource.GitLab: &forgegitlab.Client{HTTP: http.DefaultClient},
		},
	}
	e.Preparer = &preparation.Service{Repo: e.Repo, Ports: ports, Upstream: discovery, HTTP: http.DefaultClient, Workspaces: &workspace.Registry{}}
	return e.Preparer, nil
}

// selectionReader is MacPorts' own evaluator, resolving port names
// against an index staged for each source tree.
func (e *Engine) selectionReader() (*selection.Reader, error) {
	if e.ports != nil {
		return e.ports, nil
	}
	cache, err := IndexCache()
	if err != nil {
		return nil, err
	}
	native := &eval.Evaluator{Executable: e.options.Tclsh}
	index := portindex.Config{CacheDirectory: cache, Mirror: &portindex.Mirror{HTTP: http.DefaultClient, Base: os.Getenv("DOCKHAND_INDEX_MIRROR")}}
	e.ports = &selection.Reader{Evaluator: native, Index: &portindex.Stager{Repo: e.Repo, Config: index, NativePlatform: native.NativePlatform, WithoutBase: true}}
	return e.ports, nil
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
