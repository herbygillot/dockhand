package app

import (
	forgegithub "github.com/herbygillot/dockhand/v2/internal/forge/github"
	"github.com/herbygillot/dockhand/v2/internal/macports"
	portsource "github.com/herbygillot/dockhand/v2/internal/macports/source"
	"github.com/herbygillot/dockhand/v2/internal/upstream"
)

func releaseDiscovery(ports *macports.Evaluator, github *forgegithub.Client) *upstream.Service {
	return &upstream.Service{
		Ports: ports,
		Catalogs: map[portsource.Forge]upstream.Catalog{
			portsource.GitHub: github,
		},
		Versions: ports,
	}
}
