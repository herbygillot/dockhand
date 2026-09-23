package app

import (
	"context"
	"github.com/herbygillot/dockhand/internal/macports/portindex"
	"github.com/herbygillot/dockhand/internal/macports/workspace"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/dependents"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/verify"
	"github.com/herbygillot/dockhand/internal/verify/tart"
)

type dependentDiscovery struct {
	repo       *git.Repository
	ports      macports.Reader
	indexCache string
	mirror     *portindex.Mirror
	workspaces *workspace.Registry
}

func (d dependentDiscovery) Discover(ctx context.Context, source record.Source, build record.BuildConfig, roots []record.Target) (verify.Coverage, error) {
	// The index recipe is the one frozen in the build's Tart configuration,
	// so discovery indexes exactly what verification will build against.
	index, err := tart.SourceIndex(build, d.indexCache, d.mirror)
	if err != nil {
		return verify.Coverage{}, err
	}
	service := dependents.Service{Repo: d.repo, Ports: d.ports, Index: &portindex.Stager{Repo: d.repo, Config: index}, Workspaces: d.workspaces}
	return service.Discover(ctx, source, build.Platform, roots)
}
