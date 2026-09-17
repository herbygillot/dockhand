package app

import (
	"context"
	"net/http"

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
	ports := portReader(config, repo)
	index, err := surveyIndex(config, len(selection.Ports) == 0)
	if err != nil {
		return outdated.Result{}, err
	}
	service := outdated.Service{Repo: repo, Ports: ports, Upstream: releaseDiscovery(ports, newGitHubClient(config.GitHub), http.DefaultClient), Index: index, HTTP: http.DefaultClient}
	return service.Observe(ctx, selection)
}
