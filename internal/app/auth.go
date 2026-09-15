package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/herbygillot/dockhand/internal/credential"
	"github.com/herbygillot/dockhand/internal/credential/keychain"
	"github.com/herbygillot/dockhand/internal/forge/github"
)

var DefaultGitHubOAuthClientID string

var githubCredentialKey = credential.Key{Service: "github.com/herbygillot/dockhand", Account: "github.com"}

type GitHubLoginOptions struct {
	ClientID string
	Present  func(credential.DeviceAuthorization) error
	Flow     credential.DeviceFlow
	Store    credential.Store
}

type GitHubLoginResult struct {
	Host    string `json:"host"`
	Account string `json:"account"`
	Storage string `json:"storage"`
}

func LoginGitHub(ctx context.Context, options GitHubLoginOptions) (GitHubLoginResult, error) {
	if options.ClientID == "" {
		options.ClientID = DefaultGitHubOAuthClientID
	}
	if options.ClientID == "" {
		return GitHubLoginResult{}, fmt.Errorf("auth: GitHub OAuth client ID is required")
	}
	if options.Flow == nil {
		options.Flow = &github.DeviceFlow{HTTP: http.DefaultClient}
	}
	if options.Store == nil {
		options.Store = keychain.Store{}
	}
	value, err := options.Flow.Authorize(ctx, options.ClientID, options.Present)
	if err != nil {
		return GitHubLoginResult{}, err
	}
	if value.Secret == "" || value.Account == "" {
		return GitHubLoginResult{}, fmt.Errorf("auth: GitHub returned an incomplete credential")
	}
	if err := options.Store.Put(ctx, githubCredentialKey, value.Secret); err != nil {
		return GitHubLoginResult{}, err
	}
	return GitHubLoginResult{Host: "github.com", Account: value.Account, Storage: "macOS Keychain"}, nil
}

type GitHubAuthStatus struct {
	Host          string                  `json:"host"`
	Account       string                  `json:"account,omitempty"`
	Source        github.CredentialSource `json:"source,omitempty"`
	Authenticated bool                    `json:"authenticated"`
}

func StatusGitHub(ctx context.Context, client *github.Client) (GitHubAuthStatus, error) {
	if client == nil {
		client = &github.Client{Credentials: github.SystemCredentials{Store: keychain.Store{}, Key: githubCredentialKey}}
	}
	account, err := client.AuthenticatedUser(ctx)
	return GitHubAuthStatus{Host: "github.com", Account: account, Source: client.CredentialSource(), Authenticated: err == nil}, err
}

type GitHubLogoutResult struct {
	Host    string `json:"host"`
	Storage string `json:"storage"`
	Removed bool   `json:"removed"`
}

func LogoutGitHub(ctx context.Context, store credential.Remover) (GitHubLogoutResult, error) {
	if store == nil {
		store = keychain.Store{}
	}
	result := GitHubLogoutResult{Host: "github.com", Storage: "macOS Keychain"}
	err := store.Delete(ctx, githubCredentialKey)
	if errors.Is(err, credential.ErrNotFound) {
		return result, nil
	}
	if err != nil {
		return result, err
	}
	result.Removed = true
	return result, nil
}
