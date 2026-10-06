package github_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/herbygillot/dockhand/internal/credential"
	"github.com/herbygillot/dockhand/internal/github"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/oauth2"
)

func TestDeviceFlowUsesOAuthPollingAndValidatesTheAuthenticatedUser(t *testing.T) {
	presented := false
	tokenRequests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/device/code":
			require.NoError(t, r.ParseForm())
			assert.Equal(t, "fixture-client", r.Form.Get("client_id"))
			assert.Equal(t, "public_repo offline_access", r.Form.Get("scope"))
			fmt.Fprint(w, `{"device_code":"device-secret","user_code":"ABCD-EFGH","verification_uri":"https://github.com/login/device","expires_in":10,"interval":1}`)
		case "/access_token":
			tokenRequests++
			assert.True(t, presented)
			require.NoError(t, r.ParseForm())
			assert.Equal(t, "fixture-client", r.Form.Get("client_id"))
			assert.Equal(t, "device-secret", r.Form.Get("device_code"))
			assert.Equal(t, "urn:ietf:params:oauth:grant-type:device_code", r.Form.Get("grant_type"))
			// GitHub's token endpoint answers form-encoded, as its docs
			// show.
			w.Header().Set("Content-Type", "application/x-www-form-urlencoded")
			fmt.Fprint(w, "access_token=oauth-secret&expires_in=28800&refresh_token=refresh-secret&refresh_token_expires_in=15897600&scope=public_repo&token_type=bearer")
		case "/user":
			assert.Equal(t, "Bearer oauth-secret", r.Header.Get("Authorization"))
			fmt.Fprint(w, `{"login":"fixture-user"}`)
		default:
			t.Errorf("unexpected request %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 4*time.Second)
	defer cancel()
	now := time.Date(2026, 10, 2, 17, 0, 0, 0, time.UTC)
	flow := &github.DeviceFlow{HTTP: server.Client(), Endpoint: oauth2.Endpoint{DeviceAuthURL: server.URL + "/device/code", TokenURL: server.URL + "/access_token", AuthStyle: oauth2.AuthStyleInParams}, APIBaseURL: server.URL, Now: func() time.Time { return now }}
	value, err := flow.Authorize(ctx, "fixture-client", func(authorization credential.DeviceAuthorization) error {
		presented = true
		require.Equal(t, "ABCD-EFGH", authorization.UserCode)
		require.Equal(t, "https://github.com/login/device", authorization.VerificationURL)
		require.False(t, authorization.ExpiresAt.IsZero())
		return nil
	})
	require.NoError(t, err)
	require.Equal(t, "oauth-secret", value.Access)
	require.Equal(t, "refresh-secret", value.Refresh)
	require.Equal(t, now.Add(15897600*time.Second), value.RefreshExpiry, "GitHub's six months, form-encoded")
	require.WithinDuration(t, time.Now().Add(8*time.Hour), value.AccessExpiry, time.Minute)
	require.Equal(t, "fixture-user", value.Account)
	require.Equal(t, "fixture-client", value.ClientID)
	require.Equal(t, 1, tokenRequests)
}

func TestDeviceFlowStopsWhenThePromptCannotBePresented(t *testing.T) {
	tokenRequests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/device/code" {
			fmt.Fprint(w, `{"device_code":"device-secret","user_code":"ABCD-EFGH","verification_uri":"https://github.com/login/device","expires_in":10,"interval":1}`)
			return
		}
		tokenRequests++
	}))
	defer server.Close()
	flow := &github.DeviceFlow{HTTP: server.Client(), Endpoint: oauth2.Endpoint{DeviceAuthURL: server.URL + "/device/code", TokenURL: server.URL + "/access_token", AuthStyle: oauth2.AuthStyleInParams}}
	_, err := flow.Authorize(t.Context(), "fixture-client", func(credential.DeviceAuthorization) error { return assert.AnError })
	require.ErrorIs(t, err, assert.AnError)
	require.Zero(t, tokenRequests)
	_, err = flow.Authorize(t.Context(), "", func(credential.DeviceAuthorization) error { return nil })
	require.Error(t, err)
}

// A login GitHub gives without a refresh token, as an OAuth app whose
// tokens don't expire does, can't renew itself, and isn't kept.
func TestALoginWithoutARefreshTokenIsRefused(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/device/code":
			fmt.Fprint(w, `{"device_code":"device-secret","user_code":"ABCD-EFGH","verification_uri":"https://github.com/login/device","expires_in":10,"interval":1}`)
		case "/access_token":
			fmt.Fprint(w, `{"access_token":"oauth-secret","token_type":"bearer","scope":"public_repo"}`)
		}
	}))
	defer server.Close()
	flow := &github.DeviceFlow{HTTP: server.Client(), Endpoint: oauth2.Endpoint{DeviceAuthURL: server.URL + "/device/code", TokenURL: server.URL + "/access_token", AuthStyle: oauth2.AuthStyleInParams}, APIBaseURL: server.URL}
	_, err := flow.Authorize(t.Context(), "fixture-client", func(credential.DeviceAuthorization) error { return nil })
	require.ErrorContains(t, err, "GitHub's login came with no refresh token, no refresh_token_expires_in, no expires_in, so it couldn't renew itself; nothing was saved")
}

// A code left unentered says it expired, and how to get another, as
// GitHub's expired_token or the wait its expiry set ends it (the rc6 full
// stage, D-C2).
func TestAnExpiredDeviceCodeSaysSo(t *testing.T) {
	for _, answer := range []string{"expired_token", "authorization_pending"} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			switch r.URL.Path {
			case "/device/code":
				fmt.Fprint(w, `{"device_code":"device-secret","user_code":"ABCD-EFGH","verification_uri":"https://github.com/login/device","expires_in":2,"interval":1}`)
			case "/access_token":
				fmt.Fprintf(w, `{"error":%q}`, answer)
			}
		}))
		flow := &github.DeviceFlow{HTTP: server.Client(), Endpoint: oauth2.Endpoint{DeviceAuthURL: server.URL + "/device/code", TokenURL: server.URL + "/access_token", AuthStyle: oauth2.AuthStyleInParams}, APIBaseURL: server.URL}
		_, err := flow.Authorize(t.Context(), "fixture-client", func(credential.DeviceAuthorization) error { return nil })
		server.Close()
		require.ErrorContains(t, err, "the code ABCD-EFGH expired before it was entered at https://github.com/login/device; run dockhand setup github again", answer)
	}
}
