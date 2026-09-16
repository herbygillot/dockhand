package app

import (
	"context"
	"net/http"

	"github.com/herbygillot/dockhand/internal/assess"
	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/macports/eval"
)

// Assess wires preparation diagnostics without opening workflow state.
func Assess(ctx context.Context, config Config, request assess.Request) (assess.Result, error) {
	if err := request.Validate(); err != nil {
		return assess.Result{}, err
	}
	repo, err := git.Open(ctx, config.Repository, config.GitExecutable)
	if err != nil {
		return assess.Result{}, err
	}
	ports := &eval.Evaluator{Executable: config.TclExecutable, Prefix: config.MacPortsPrefix}
	index, err := surveyIndex(config, len(request.Selection.Ports) == 0)
	if err != nil {
		return assess.Result{}, err
	}
	service := assess.Service{Repo: repo, Ports: ports, Index: index, HTTP: http.DefaultClient, DependencyTools: config.DependencyTools}
	if request.Version != "" {
		service.Upstream = releaseDiscovery(ports, newGitHubClient(config.GitHub), http.DefaultClient)
	}
	return service.Assess(ctx, request)
}
