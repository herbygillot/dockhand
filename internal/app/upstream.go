package app

import (
	"net/http"

	forgegithub "github.com/herbygillot/dockhand/internal/forge/github"
	forgegitlab "github.com/herbygillot/dockhand/internal/forge/gitlab"
	"github.com/herbygillot/dockhand/internal/macports"
	portsource "github.com/herbygillot/dockhand/internal/macports/source"
	"github.com/herbygillot/dockhand/internal/upstream"
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
