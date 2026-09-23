package app

import (
	"context"
	"net/http"

	"github.com/herbygillot/dockhand/internal/macports/portindex"
	"github.com/herbygillot/dockhand/internal/outdated"
)

// Outdated wires read-only discovery without constructing workflow services.
func Outdated(ctx context.Context, config Config, selection outdated.Selection) (outdated.Result, error) {
	if err := selection.Validate(); err != nil {
		return outdated.Result{}, err
	}
	repo, err := openPortsTree(ctx, config.Repository, config.GitExecutable)
	if err != nil {
		return outdated.Result{}, err
	}
	ports, err := portReader(config, repo, nil)
	if err != nil {
		return outdated.Result{}, err
	}
	// A filter selects from the index; explicit names need none.
	var index portindex.Source
	if len(selection.Ports) == 0 {
		recipe, err := surveyIndex(config, true)
		if err != nil {
			return outdated.Result{}, err
		}
		index = &portindex.Stager{Repo: repo, Config: recipe}
	}
	service := outdated.Service{Repo: repo, Ports: ports, Upstream: releaseDiscovery(ports, newGitHubClient(config.GitHub), http.DefaultClient, config.GitExecutable), Index: index}
	return service.Observe(ctx, selection)
}
