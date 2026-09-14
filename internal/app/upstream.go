package app

import (
	"net/http"

	forgegithub "github.com/herbygillot/dockhand/v2/internal/forge/github"
	forgegitlab "github.com/herbygillot/dockhand/v2/internal/forge/gitlab"
	"github.com/herbygillot/dockhand/v2/internal/macports"
	portsource "github.com/herbygillot/dockhand/v2/internal/macports/source"
	"github.com/herbygillot/dockhand/v2/internal/upstream"
)

func releaseDiscovery(ports *macports.Evaluator, github *forgegithub.Client, httpClient *http.Client) *upstream.Service {
	gitlab := &forgegitlab.Client{HTTP: httpClient}
	return &upstream.Service{
		Ports: ports,
		Catalogs: map[portsource.Forge]upstream.Catalog{
			portsource.GitHub: github,
			portsource.GitLab: gitlab,
		},
		Versions: ports,
	}
}
