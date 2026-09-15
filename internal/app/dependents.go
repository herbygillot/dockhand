package app

import (
	"context"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/dependents"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/verify/tart"
)

type dependentDiscovery struct {
	repo  *git.Repository
	ports macports.Reader
}

func (d dependentDiscovery) Discover(ctx context.Context, source record.Source, build record.BuildConfig, roots []record.Target) (dependents.Coverage, error) {
	index, err := tart.SourceIndex(build)
	if err != nil {
		return dependents.Coverage{}, err
	}
	service := dependents.Service{Repo: d.repo, Ports: d.ports, Index: index}
	return service.Discover(ctx, source, build.Platform, roots)
}
