package app

import (
	"net/http"

	"github.com/herbygillot/dockhand/internal/credential/keychain"
	"github.com/herbygillot/dockhand/internal/github"
)

func newGitHubClient(config github.Config) *github.Client {
	client := &github.Client{HTTP: http.DefaultClient, Config: config}
	if config.BaseURL == "" {
		client.Credentials = github.SystemCredentials{Store: keychain.Store{}, Key: githubCredentialKey}
	}
	return client
}
