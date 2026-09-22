package app

import (
	"net/http"

	forgegithub "github.com/herbygillot/dockhand/internal/forge/github"
	forgegitlab "github.com/herbygillot/dockhand/internal/forge/gitlab"
	githubapi "github.com/herbygillot/dockhand/internal/github"
	"github.com/herbygillot/dockhand/internal/macports/selection"
	portsource "github.com/herbygillot/dockhand/internal/macports/source"
	"github.com/herbygillot/dockhand/internal/upstream"
)

func releaseDiscovery(ports *selection.Reader, github *githubapi.Client, httpClient *http.Client, gitExecutable string) *upstream.Service {
	gitlab := &forgegitlab.Client{HTTP: httpClient}
	return &upstream.Service{
		Ports: ports, HTTP: httpClient,
		Catalogs: map[portsource.Forge]upstream.Catalog{
			portsource.GitHub: &forgegithub.Client{Client: github, GitExecutable: gitExecutable},
			portsource.GitLab: gitlab,
		},
		Versions: ports,
	}
}
