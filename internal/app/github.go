package app

import (
	"github.com/herbygillot/dockhand/internal/credential/keychain"
	"github.com/herbygillot/dockhand/internal/forge/github"
	"net/http"
)

func newGitHubClient(config github.Config) *github.Client {
	client := &github.Client{HTTP: http.DefaultClient, Config: config}
	if config.BaseURL == "" {
		client.Credentials = github.SystemCredentials{Store: keychain.Store{}, Key: githubCredentialKey}
	}
	return client
}
