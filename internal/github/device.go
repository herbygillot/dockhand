package github

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/herbygillot/dockhand/internal/credential"
	"github.com/herbygillot/dockhand/internal/fetch"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/endpoints"
)

// DeviceFlow signs in through GitHub's device flow. HTTP is fetch.Client
// when nil, whose wait for a response is bounded.
type DeviceFlow struct {
	HTTP       *http.Client
	Endpoint   oauth2.Endpoint
	APIBaseURL string
}

// DefaultOAuthClientID is the public client ID of dockhand's GitHub OAuth
// application, which has the device flow enabled. No client secret exists.
// The Makefile's GITHUB_OAUTH_CLIENT_ID replaces it at link time for
// development and alternate registrations.
var DefaultOAuthClientID = "Ov23lixRrrqO0uEzU2GA"

func (f *DeviceFlow) Authorize(ctx context.Context, clientID string, present func(credential.DeviceAuthorization) error) (credential.Value, error) {
	if f == nil || strings.TrimSpace(clientID) == "" || strings.ContainsAny(clientID, " \t\r\n") {
		return credential.Value{}, fmt.Errorf("github: OAuth client ID is required")
	}
	if present == nil {
		return credential.Value{}, fmt.Errorf("github: device authorization presenter is required")
	}
	endpoint := f.Endpoint
	if endpoint.DeviceAuthURL == "" && endpoint.TokenURL == "" {
		endpoint = endpoints.GitHub
	}
	config := oauth2.Config{ClientID: clientID, Scopes: []string{"public_repo"}, Endpoint: endpoint}
	client := f.HTTP
	if client == nil {
		client = fetch.Client
	}
	ctx = context.WithValue(ctx, oauth2.HTTPClient, client)
	authorization, err := config.DeviceAuth(ctx)
	if err != nil {
		return credential.Value{}, fmt.Errorf("github: starting device authorization: %w", err)
	}
	if authorization.DeviceCode == "" || authorization.UserCode == "" || authorization.VerificationURI == "" {
		return credential.Value{}, fmt.Errorf("github: incomplete device authorization response")
	}
	if err := present(credential.DeviceAuthorization{UserCode: authorization.UserCode, VerificationURL: authorization.VerificationURI, ExpiresAt: authorization.Expiry}); err != nil {
		return credential.Value{}, err
	}
	token, err := config.DeviceAccessToken(ctx, authorization)
	if err != nil {
		return credential.Value{}, fmt.Errorf("github: completing device authorization: %w", err)
	}
	if token == nil {
		return credential.Value{}, fmt.Errorf("github: device authorization returned no token")
	}
	secret, err := validToken(token.AccessToken)
	if err != nil {
		return credential.Value{}, err
	}
	if token.TokenType != "" && !strings.EqualFold(token.TokenType, "bearer") {
		return credential.Value{}, fmt.Errorf("github: device authorization returned unsupported token type %q", token.TokenType)
	}
	api := &Client{HTTP: client, Config: Config{BaseURL: f.APIBaseURL, Token: secret}}
	login, err := api.AuthenticatedUser(ctx)
	if err != nil {
		return credential.Value{}, err
	}
	return credential.Value{Secret: secret, Account: login}, nil
}
