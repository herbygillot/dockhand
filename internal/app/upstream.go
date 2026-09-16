package app

import (
	"net/http"

	forgegithub "github.com/herbygillot/dockhand/internal/forge/github"
	forgegitlab "github.com/herbygillot/dockhand/internal/forge/gitlab"
	githubapi "github.com/herbygillot/dockhand/internal/github"
	"github.com/herbygillot/dockhand/internal/macports/eval"
	portsource "github.com/herbygillot/dockhand/internal/macports/source"
	"github.com/herbygillot/dockhand/internal/upstream"
)

func releaseDiscovery(ports *eval.Evaluator, github *githubapi.Client, httpClient *http.Client) *upstream.Service {
	gitlab := &forgegitlab.Client{HTTP: httpClient}
	return &upstream.Service{
		Ports: ports,
		Catalogs: map[portsource.Forge]upstream.Catalog{
			portsource.GitHub: &forgegithub.Client{Client: github},
			portsource.GitLab: gitlab,
		},
		Versions: ports,
	}
}
