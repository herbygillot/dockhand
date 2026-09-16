package app

import (
	"context"
	"net/http"
	"os"
	"path/filepath"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/portindex"
	"github.com/herbygillot/dockhand/internal/outdated"
)

// Outdated wires read-only discovery without constructing workflow services.
func Outdated(ctx context.Context, config Config, selection outdated.Selection) (outdated.Result, error) {
	if err := selection.Validate(); err != nil {
		return outdated.Result{}, err
	}
	repo, err := git.Open(ctx, config.Repository, config.GitExecutable)
	if err != nil {
		return outdated.Result{}, err
	}
	ports := &macports.Evaluator{Executable: config.TclExecutable, Prefix: config.MacPortsPrefix}
	index := portindex.Config{}
	if len(selection.Ports) == 0 {
		cache, err := os.UserCacheDir()
		if err != nil {
			return outdated.Result{}, err
		}
		index.CacheDirectory = filepath.Join(cache, "dockhand", "indexes")
		if config.MacPortsPrefix != "" {
			index.Executable = filepath.Join(config.MacPortsPrefix, "bin", "portindex")
		}
	}
	service := outdated.Service{Repo: repo, Ports: ports, Upstream: releaseDiscovery(ports, newGitHubClient(config.GitHub), http.DefaultClient), Index: index, HTTP: http.DefaultClient}
	return service.Observe(ctx, selection)
}
