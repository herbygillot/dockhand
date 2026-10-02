package github

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

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
	// Now is the clock a refresh token's expiry is reckoned by; time.Now
	// where nil.
	Now func() time.Time
}

// DefaultOAuthClientID is the public client ID of dockhand's GitHub OAuth
// application, which has the device flow enabled. No client secret exists.
// The Makefile's GITHUB_OAUTH_CLIENT_ID replaces it at link time for
// development and alternate registrations.
var DefaultOAuthClientID = "Ov23lixRrrqO0uEzU2GA"

func (f *DeviceFlow) Authorize(ctx context.Context, clientID string, present func(credential.DeviceAuthorization) error) (credential.Login, error) {
	if f == nil || strings.TrimSpace(clientID) == "" || strings.ContainsAny(clientID, " \t\r\n") {
		return credential.Login{}, fmt.Errorf("github: OAuth client ID is required")
	}
	if present == nil {
		return credential.Login{}, fmt.Errorf("github: device authorization presenter is required")
	}
	config, ctx := f.config(ctx, clientID)
	authorization, err := config.DeviceAuth(ctx)
	if err != nil {
		return credential.Login{}, fmt.Errorf("github: starting device authorization: %w", err)
	}
	if authorization.DeviceCode == "" || authorization.UserCode == "" || authorization.VerificationURI == "" {
		return credential.Login{}, fmt.Errorf("github: incomplete device authorization response")
	}
	if err := present(credential.DeviceAuthorization{UserCode: authorization.UserCode, VerificationURL: authorization.VerificationURI, ExpiresAt: authorization.Expiry}); err != nil {
		return credential.Login{}, err
	}
	token, err := config.DeviceAccessToken(ctx, authorization)
	if err != nil {
		return credential.Login{}, fmt.Errorf("github: completing device authorization: %w", err)
	}
	if token == nil {
		return credential.Login{}, fmt.Errorf("github: device authorization returned no token")
	}
	secret, err := validToken(token.AccessToken)
	if err != nil {
		return credential.Login{}, err
	}
	if token.TokenType != "" && !strings.EqualFold(token.TokenType, "bearer") {
		return credential.Login{}, fmt.Errorf("github: device authorization returned unsupported token type %q", token.TokenType)
	}
	renewal, err := renewalOf(token, f.clock())
	if err != nil {
		return credential.Login{}, err
	}
	api := &Client{HTTP: f.client(), Config: Config{BaseURL: f.APIBaseURL, Token: secret}}
	account, err := api.AuthenticatedUser(ctx)
	if err != nil {
		return credential.Login{}, err
	}
	login := credential.Login{Access: secret, AccessExpiry: token.Expiry, Refresh: token.RefreshToken, RefreshExpiry: renewal, Account: account, ClientID: clientID}
	if !login.Complete() {
		return credential.Login{}, fmt.Errorf("github: GitHub's login has no expiry, or no refresh token, so it couldn't renew itself; nothing was saved")
	}
	return login, nil
}

// scopes are what dockhand's login asks: opening pull requests from the
// person's fork, and a refresh token, which offline_access asks of an
// OAuth app that doesn't expire its tokens for all.
var scopes = []string{"public_repo", "offline_access"}

// config is the OAuth configuration of the device flow and its refreshes,
// with ctx carrying the flow's HTTP client. GitHub's endpoints take the
// client ID in the request's parameters: auto-detection would first try
// Basic auth with an empty secret.
func (f *DeviceFlow) config(ctx context.Context, clientID string) (oauth2.Config, context.Context) {
	endpoint := f.Endpoint
	if endpoint.DeviceAuthURL == "" && endpoint.TokenURL == "" {
		endpoint = endpoints.GitHub
		endpoint.AuthStyle = oauth2.AuthStyleInParams
	}
	return oauth2.Config{ClientID: clientID, Scopes: scopes, Endpoint: endpoint}, context.WithValue(ctx, oauth2.HTTPClient, f.client())
}

func (f *DeviceFlow) client() *http.Client {
	if f.HTTP != nil {
		return f.HTTP
	}
	return fetch.Client
}

func (f *DeviceFlow) clock() time.Time {
	if f.Now != nil {
		return f.Now()
	}
	return time.Now()
}

// renewalOf is when a token's refresh token expires: GitHub says how many
// seconds it lasts, as refresh_token_expires_in, a number or a string.
func renewalOf(token *oauth2.Token, now time.Time) (time.Time, error) {
	var seconds int64
	// GitHub answers form-encoded, where oauth2 reads an integer as an
	// int64, or as JSON, where it's a float64 or, here and there, a
	// string: the int64 case was missing, so every real login was refused
	// (the auth flow review thread, 2026-10-02).
	switch value := token.Extra("refresh_token_expires_in").(type) {
	case int64:
		seconds = value
	case int:
		seconds = int64(value)
	case float64:
		seconds = int64(value)
	case string:
		parsed, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			return time.Time{}, fmt.Errorf("github: GitHub's login says its refresh token lasts %q seconds", value)
		}
		seconds = parsed
	case json.Number:
		parsed, err := value.Int64()
		if err != nil {
			return time.Time{}, fmt.Errorf("github: GitHub's login says its refresh token lasts %q seconds", value)
		}
		seconds = parsed
	}
	var missing []string
	if token.RefreshToken == "" {
		missing = append(missing, "no refresh token")
	}
	if seconds <= 0 {
		missing = append(missing, "no refresh_token_expires_in")
	}
	if token.Expiry.IsZero() {
		missing = append(missing, "no expires_in")
	}
	if len(missing) > 0 {
		return time.Time{}, fmt.Errorf("github: GitHub's login came with %s, so it couldn't renew itself; nothing was saved. GitHub gives a refresh token where the OAuth app expires its user tokens, or the login asks offline_access", strings.Join(missing, ", "))
	}
	return now.Add(time.Duration(seconds) * time.Second), nil
}
