package app

import (
	"context"
	"net/http"

	"github.com/herbygillot/dockhand/internal/assess"
)

// Assess wires preparation diagnostics without opening workflow state.
func Assess(ctx context.Context, config Config, request assess.Request) (assess.Result, error) {
	if err := request.Validate(); err != nil {
		return assess.Result{}, err
	}
	repo, err := openPortsTree(ctx, config.Repository, config.GitExecutable)
	if err != nil {
		return assess.Result{}, err
	}
	ports := portReader(config, repo, nil)
	// Explicit names need the index too: it tells which of them share a
	// Portfile, and those are assessed one after another rather than at once.
	index, err := surveyIndex(config, true)
	if err != nil {
		return assess.Result{}, err
	}
	service := assess.Service{Repo: repo, Ports: ports, Index: index, DependencyTools: config.DependencyTools}
	if request.Version != "" {
		service.Upstream = releaseDiscovery(ports, newGitHubClient(config.GitHub), http.DefaultClient)
	}
	return service.Assess(ctx, request)
}
