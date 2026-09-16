package app

import (
	"context"
	"net/http"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/macports"
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
	index, err := surveyIndex(config, len(selection.Ports) == 0)
	if err != nil {
		return outdated.Result{}, err
	}
	service := outdated.Service{Repo: repo, Ports: ports, Upstream: releaseDiscovery(ports, newGitHubClient(config.GitHub), http.DefaultClient), Index: index, HTTP: http.DefaultClient}
	return service.Observe(ctx, selection)
}
